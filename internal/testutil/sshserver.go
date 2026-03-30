package testutil

import (
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"encoding/pem"
	"net"
	"testing"

	"golang.org/x/crypto/ssh"
)

// StartSSHServer starts a minimal in-memory SSH server using the provided host key.
// It returns the listener address and registers cleanup with t.Cleanup.
// The server accepts any client public key and handles exec requests as a no-op.
func StartSSHServer(t *testing.T, hostSigner ssh.Signer) string {
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
	t.Cleanup(func() { ln.Close() })

	go func() {
		for {
			conn, err := ln.Accept()
			if err != nil {
				return
			}
			go serveSSHConn(conn, serverCfg)
		}
	}()

	return ln.Addr().String()
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
		go handleSSHChannel(channel, requests)
	}
}

func handleSSHChannel(ch ssh.Channel, reqs <-chan *ssh.Request) {
	defer ch.Close()
	for req := range reqs {
		switch req.Type {
		case "exec":
			if req.WantReply {
				req.Reply(true, nil)
			}
			ch.SendRequest("exit-status", false, []byte{0, 0, 0, 0})
			return
		case "pty-req":
			if req.WantReply {
				req.Reply(true, nil)
			}
		case "shell":
			if req.WantReply {
				req.Reply(true, nil)
			}
			// Echo stdin to stdout until channel closes (for terminal tests).
			buf := make([]byte, 1024)
			for {
				n, err := ch.Read(buf)
				if err != nil {
					return
				}
				if _, err := ch.Write(buf[:n]); err != nil {
					return
				}
			}
		case "window-change":
			if req.WantReply {
				req.Reply(true, nil)
			}
		case "signal":
			if req.WantReply {
				req.Reply(true, nil)
			}
			return
		default:
			if req.WantReply {
				req.Reply(true, nil)
			}
		}
	}
}

// GenerateSigner creates a new RSA SSH signer for testing.
func GenerateSigner(t *testing.T) ssh.Signer {
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

// NewClientKeyPEM generates a fresh RSA private key and returns its PEM bytes
// in PKCS#1 format, suitable for ssh.ParsePrivateKey.
func NewClientKeyPEM(t *testing.T) []byte {
	t.Helper()
	priv, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatalf("generate rsa key: %v", err)
	}
	der := x509.MarshalPKCS1PrivateKey(priv)
	return pem.EncodeToMemory(&pem.Block{Type: "RSA PRIVATE KEY", Bytes: der})
}
