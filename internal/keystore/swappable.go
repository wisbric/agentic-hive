package keystore

import (
	"context"
	"sync"
)

// SwappableKeyStore wraps a KeyStore that can be hot-swapped at runtime.
type SwappableKeyStore struct {
	mu    sync.RWMutex
	inner KeyStore
}

func NewSwappable(ks KeyStore) *SwappableKeyStore {
	return &SwappableKeyStore{inner: ks}
}

func (s *SwappableKeyStore) Swap(ks KeyStore) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.inner = ks
}

func (s *SwappableKeyStore) Get(ctx context.Context, serverID string) ([]byte, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.inner.Get(ctx, serverID)
}

func (s *SwappableKeyStore) Put(ctx context.Context, serverID string, key []byte) error {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.inner.Put(ctx, serverID, key)
}

func (s *SwappableKeyStore) Delete(ctx context.Context, serverID string) error {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.inner.Delete(ctx, serverID)
}

// Backend returns a string identifying the active backend type.
func (s *SwappableKeyStore) Backend() string {
	s.mu.RLock()
	defer s.mu.RUnlock()
	switch s.inner.(type) {
	case *VaultKeyStore:
		return "vault"
	default:
		return "local"
	}
}
