package sshpool

import (
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"encoding/pem"
	"fmt"
	"net"
	"path/filepath"
	"strings"
	"testing"

	"github.com/wisbric/agentic-hive/internal/keystore"
	"github.com/wisbric/agentic-hive/internal/store"
	"golang.org/x/crypto/ssh"
)

// startSSHServer starts a minimal in-memory SSH server using the provided host key.
// It returns the listener address and a stop function. The server accepts any
// client public key and handles exec requests as a no-op.
func startSSHServer(t *testing.T, hostSigner ssh.Signer) (addr string, stop func()) {
	t.Helper()

	serverCfg := &ssh.ServerConfig{
		PublicKeyCallback: func(_ ssh.ConnMetadata, _ ssh.PublicKey) (*ssh.Permissions, error) {
			return &ssh.Permissions{}, nil
		},
	}
	serverCfg.AddHostKey(hostSigner)

	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}

	go func() {
		for {
			conn, err := ln.Accept()
			if err != nil {
				return
			}
			go serveSSHConn(conn, serverCfg)
		}
	}()

	return ln.Addr().String(), func() { ln.Close() }
}

func serveSSHConn(conn net.Conn, cfg *ssh.ServerConfig) {
	defer conn.Close()
	sshConn, chans, reqs, err := ssh.NewServerConn(conn, cfg)
	if err != nil {
		return
	}
	defer sshConn.Close()
	go ssh.DiscardRequests(reqs)
	for ch := range chans {
		if ch.ChannelType() != "session" {
			ch.Reject(ssh.UnknownChannelType, "unknown channel type")
			continue
		}
		channel, requests, err := ch.Accept()
		if err != nil {
			continue
		}
		go func(ch ssh.Channel, reqs <-chan *ssh.Request) {
			defer ch.Close()
			for req := range reqs {
				if req.WantReply {
					req.Reply(true, nil)
				}
				if req.Type == "exec" {
					ch.SendRequest("exit-status", false, []byte{0, 0, 0, 0})
					return
				}
			}
		}(channel, requests)
	}
}

// generateSigner creates a new RSA SSH signer.
func generateSigner(t *testing.T) ssh.Signer {
	t.Helper()
	priv, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatalf("generate rsa key: %v", err)
	}
	signer, err := ssh.NewSignerFromKey(priv)
	if err != nil {
		t.Fatalf("new signer: %v", err)
	}
	return signer
}

// newClientKeyPEM generates a fresh RSA private key and returns its PEM bytes
// in PKCS#1 format, suitable for ssh.ParsePrivateKey.
func newClientKeyPEM(t *testing.T) []byte {
	t.Helper()
	priv, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatalf("generate rsa key: %v", err)
	}
	der := x509.MarshalPKCS1PrivateKey(priv)
	return pem.EncodeToMemory(&pem.Block{Type: "RSA PRIVATE KEY", Bytes: der})
}

// setupPool creates a pool backed by a temporary store and registers a server
// pointing to addr. It puts a valid RSA private key PEM in the keystore.
func setupPool(t *testing.T, addr string) (*Pool, *store.Store, string) {
	t.Helper()

	dbPath := filepath.Join(t.TempDir(), "test.db")
	st, err := store.Open(dbPath)
	if err != nil {
		t.Fatalf("store.Open: %v", err)
	}
	t.Cleanup(func() { st.Close() })

	ks := keystore.NewLocal(st.DB(), "test-secret-long-enough-for-aes!")

	host, portStr, err := net.SplitHostPort(addr)
	if err != nil {
		t.Fatalf("SplitHostPort: %v", err)
	}
	port := 22
	fmt.Sscanf(portStr, "%d", &port)

	srv, err := st.CreateServer("tofu-server", host, port, "testuser", "", "local", "")
	if err != nil {
		t.Fatalf("CreateServer: %v", err)
	}

	// Store a valid client private key so pool.connect can proceed to dial.
	if err := ks.Put(t.Context(), srv.ID, newClientKeyPEM(t)); err != nil {
		t.Fatalf("keystore.Put: %v", err)
	}

	pool := New(st, ks)
	t.Cleanup(func() { pool.Close() })

	return pool, st, srv.ID
}

func TestTOFUFirstConnect(t *testing.T) {
	hostSigner := generateSigner(t)
	addr, stop := startSSHServer(t, hostSigner)
	defer stop()

	pool, st, serverID := setupPool(t, addr)

	// Before connecting, no host key should be stored.
	_, _, err := st.GetHostKey(serverID)
	if err == nil {
		t.Fatal("expected no host key before first connect")
	}

	// Trigger a connect — may succeed or fail at the auth layer, but the
	// TOFU callback fires during the SSH handshake before auth, so the key
	// is always stored on first connect as long as the TCP connection succeeds.
	_, _, _ = pool.Exec(t.Context(), serverID, "echo ok")

	storedKey, storedFP, err := st.GetHostKey(serverID)
	if err != nil {
		t.Fatalf("host key not stored after first connect: %v", err)
	}
	if len(storedKey) == 0 {
		t.Error("stored host key is empty")
	}

	expectedFP := ssh.FingerprintSHA256(hostSigner.PublicKey())
	if storedFP != expectedFP {
		t.Errorf("stored fingerprint = %q, want %q", storedFP, expectedFP)
	}
}

func TestTOFUSameKey(t *testing.T) {
	hostSigner := generateSigner(t)
	addr, stop := startSSHServer(t, hostSigner)
	defer stop()

	pool, st, serverID := setupPool(t, addr)

	// Pre-store the correct host key.
	presentedKey := hostSigner.PublicKey().Marshal()
	fp := ssh.FingerprintSHA256(hostSigner.PublicKey())
	if err := st.StoreHostKey(serverID, presentedKey, fp); err != nil {
		t.Fatalf("StoreHostKey: %v", err)
	}

	// The callback should accept the same key without error.
	_, _, err := pool.Exec(t.Context(), serverID, "echo ok")
	if err != nil && strings.Contains(err.Error(), "host key mismatch") {
		t.Errorf("unexpected host key mismatch on same key: %v", err)
	}
}

func TestTOFUKeyMismatch(t *testing.T) {
	hostSigner := generateSigner(t)
	addr, stop := startSSHServer(t, hostSigner)
	defer stop()

	pool, st, serverID := setupPool(t, addr)

	// Store a DIFFERENT (wrong) host key to simulate a key rotation / MITM.
	wrongSigner := generateSigner(t)
	wrongKey := wrongSigner.PublicKey().Marshal()
	wrongFP := ssh.FingerprintSHA256(wrongSigner.PublicKey())
	if err := st.StoreHostKey(serverID, wrongKey, wrongFP); err != nil {
		t.Fatalf("StoreHostKey: %v", err)
	}

	_, _, err := pool.Exec(t.Context(), serverID, "echo ok")
	if err == nil {
		t.Fatal("expected error on host key mismatch, got nil")
	}
	if !strings.Contains(err.Error(), "host key mismatch") {
		t.Errorf("error = %q, want to contain %q", err.Error(), "host key mismatch")
	}

	// Server status should be updated to key_mismatch.
	srv, dbErr := st.GetServer(serverID, "")
	if dbErr != nil {
		t.Fatalf("GetServer: %v", dbErr)
	}
	if srv.Status != store.StatusKeyMismatch {
		t.Errorf("status = %q, want %q", srv.Status, store.StatusKeyMismatch)
	}
}
