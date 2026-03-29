package keystore

import (
	"context"
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"database/sql"
	"fmt"

	"golang.org/x/crypto/argon2"
)

type LocalKeyStore struct {
	db               *sql.DB
	encryptionSecret string
}

func NewLocal(db *sql.DB, encryptionSecret string) *LocalKeyStore {
	return &LocalKeyStore{db: db, encryptionSecret: encryptionSecret}
}

func (s *LocalKeyStore) Put(ctx context.Context, serverID string, key []byte) error {
	salt := make([]byte, 16)
	if _, err := rand.Read(salt); err != nil {
		return fmt.Errorf("generate salt: %w", err)
	}

	encKey := deriveKey(s.encryptionSecret, salt)

	block, err := aes.NewCipher(encKey)
	if err != nil {
		return fmt.Errorf("create cipher: %w", err)
	}

	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return fmt.Errorf("create gcm: %w", err)
	}

	nonce := make([]byte, gcm.NonceSize())
	if _, err := rand.Read(nonce); err != nil {
		return fmt.Errorf("generate nonce: %w", err)
	}

	encrypted := gcm.Seal(nil, nonce, key, nil)

	_, err = s.db.ExecContext(ctx,
		`INSERT INTO ssh_keys (server_id, encrypted_key, salt, nonce) VALUES (?, ?, ?, ?)
		 ON CONFLICT(server_id) DO UPDATE SET encrypted_key=?, salt=?, nonce=?`,
		serverID, encrypted, salt, nonce, encrypted, salt, nonce,
	)
	if err != nil {
		return fmt.Errorf("store key: %w", err)
	}

	return nil
}

func (s *LocalKeyStore) GetFromPath(_ context.Context, _ string) ([]byte, error) {
	return nil, fmt.Errorf("GetFromPath not supported on local keystore")
}

func (s *LocalKeyStore) Get(ctx context.Context, serverID string) ([]byte, error) {
	var encrypted, salt, nonce []byte
	err := s.db.QueryRowContext(ctx,
		"SELECT encrypted_key, salt, nonce FROM ssh_keys WHERE server_id = ?", serverID,
	).Scan(&encrypted, &salt, &nonce)
	if err == sql.ErrNoRows {
		return nil, fmt.Errorf("key not found for server: %s", serverID)
	}
	if err != nil {
		return nil, fmt.Errorf("query key: %w", err)
	}

	encKey := deriveKey(s.encryptionSecret, salt)

	block, err := aes.NewCipher(encKey)
	if err != nil {
		return nil, fmt.Errorf("create cipher: %w", err)
	}

	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, fmt.Errorf("create gcm: %w", err)
	}

	plaintext, err := gcm.Open(nil, nonce, encrypted, nil)
	if err != nil {
		return nil, fmt.Errorf("decrypt key: %w", err)
	}

	return plaintext, nil
}

func (s *LocalKeyStore) Delete(ctx context.Context, serverID string) error {
	_, err := s.db.ExecContext(ctx, "DELETE FROM ssh_keys WHERE server_id = ?", serverID)
	return err
}

func deriveKey(secret string, salt []byte) []byte {
	return argon2.IDKey([]byte(secret), salt, 1, 64*1024, 4, 32)
}
