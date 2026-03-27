package keystore

import (
	"context"
	"fmt"

	"github.com/hashicorp/vault/api"
)

type VaultKeyStore struct {
	client     *api.Client
	secretPath string
}

func NewVault(addr, token, secretPath string) (*VaultKeyStore, error) {
	cfg := api.DefaultConfig()
	cfg.Address = addr

	client, err := api.NewClient(cfg)
	if err != nil {
		return nil, fmt.Errorf("create vault client: %w", err)
	}
	client.SetToken(token)

	return &VaultKeyStore{
		client:     client,
		secretPath: secretPath,
	}, nil
}

func (s *VaultKeyStore) keyPath(serverID string) string {
	return fmt.Sprintf("%s/%s", s.secretPath, serverID)
}

func (s *VaultKeyStore) Put(ctx context.Context, serverID string, key []byte) error {
	path := s.keyPath(serverID)
	_, err := s.client.KVv2("secret").Put(ctx, path, map[string]any{
		"private_key": string(key),
	})
	if err != nil {
		return fmt.Errorf("vault put: %w", err)
	}
	return nil
}

func (s *VaultKeyStore) Get(ctx context.Context, serverID string) ([]byte, error) {
	path := s.keyPath(serverID)
	secret, err := s.client.KVv2("secret").Get(ctx, path)
	if err != nil {
		return nil, fmt.Errorf("vault get: %w", err)
	}

	keyStr, ok := secret.Data["private_key"].(string)
	if !ok {
		return nil, fmt.Errorf("vault: private_key not found or wrong type")
	}

	return []byte(keyStr), nil
}

func (s *VaultKeyStore) Delete(ctx context.Context, serverID string) error {
	path := s.keyPath(serverID)
	err := s.client.KVv2("secret").Delete(ctx, path)
	if err != nil {
		return fmt.Errorf("vault delete: %w", err)
	}
	return nil
}
