package sshpool

import (
	"bytes"
	"context"
	"fmt"
	"sync"
	"time"

	"gitlab.com/adfinisde/agentic-workspace/claude-overlay/internal/keystore"
	"gitlab.com/adfinisde/agentic-workspace/claude-overlay/internal/store"
	"golang.org/x/crypto/ssh"
)

type Pool struct {
	store    *store.Store
	keystore keystore.KeyStore
	conns    map[string]*ssh.Client
	mu       sync.RWMutex
}

func New(st *store.Store, ks keystore.KeyStore) *Pool {
	return &Pool{
		store:    st,
		keystore: ks,
		conns:    make(map[string]*ssh.Client),
	}
}

func (p *Pool) connect(ctx context.Context, serverID string) (*ssh.Client, error) {
	srv, err := p.store.GetServer(serverID)
	if err != nil {
		return nil, fmt.Errorf("get server: %w", err)
	}

	keyBytes, err := p.keystore.Get(ctx, serverID)
	if err != nil {
		return nil, fmt.Errorf("get ssh key: %w", err)
	}

	signer, err := ssh.ParsePrivateKey(keyBytes)
	if err != nil {
		return nil, fmt.Errorf("parse private key: %w", err)
	}

	config := &ssh.ClientConfig{
		User: srv.SSHUser,
		Auth: []ssh.AuthMethod{
			ssh.PublicKeys(signer),
		},
		HostKeyCallback: ssh.InsecureIgnoreHostKey(), //nolint:gosec
		Timeout:         10 * time.Second,
	}

	addr := fmt.Sprintf("%s:%d", srv.Host, srv.Port)
	client, err := ssh.Dial("tcp", addr, config)
	if err != nil {
		return nil, fmt.Errorf("ssh dial %s: %w", addr, err)
	}

	return client, nil
}

func (p *Pool) getOrConnect(ctx context.Context, serverID string) (*ssh.Client, error) {
	p.mu.RLock()
	client, ok := p.conns[serverID]
	p.mu.RUnlock()

	if ok {
		return client, nil
	}

	client, err := p.connect(ctx, serverID)
	if err != nil {
		return nil, err
	}

	p.mu.Lock()
	// Check if another goroutine already connected
	if existing, ok := p.conns[serverID]; ok {
		p.mu.Unlock()
		client.Close()
		return existing, nil
	}
	p.conns[serverID] = client
	p.mu.Unlock()

	return client, nil
}

func (p *Pool) Exec(ctx context.Context, serverID, cmd string) (string, string, error) {
	client, err := p.getOrConnect(ctx, serverID)
	if err != nil {
		return "", "", err
	}

	session, err := client.NewSession()
	if err != nil {
		// Try reconnect once
		p.mu.Lock()
		delete(p.conns, serverID)
		p.mu.Unlock()

		client, err = p.connect(ctx, serverID)
		if err != nil {
			return "", "", fmt.Errorf("reconnect failed: %w", err)
		}

		p.mu.Lock()
		p.conns[serverID] = client
		p.mu.Unlock()

		session, err = client.NewSession()
		if err != nil {
			return "", "", fmt.Errorf("new session after reconnect: %w", err)
		}
	}
	defer session.Close()

	var stdout, stderr bytes.Buffer
	session.Stdout = &stdout
	session.Stderr = &stderr

	err = session.Run(cmd)
	return stdout.String(), stderr.String(), err
}

func (p *Pool) Session(ctx context.Context, serverID string) (*ssh.Client, *ssh.Session, error) {
	client, err := p.getOrConnect(ctx, serverID)
	if err != nil {
		return nil, nil, err
	}

	session, err := client.NewSession()
	if err != nil {
		return nil, nil, fmt.Errorf("new session: %w", err)
	}

	return client, session, nil
}

func (p *Pool) Remove(serverID string) {
	p.mu.Lock()
	defer p.mu.Unlock()

	if client, ok := p.conns[serverID]; ok {
		client.Close()
		delete(p.conns, serverID)
	}
}

func (p *Pool) Close() {
	p.mu.Lock()
	defer p.mu.Unlock()

	for id, client := range p.conns {
		client.Close()
		delete(p.conns, id)
	}
}
