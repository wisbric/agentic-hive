package keystore

import (
	"context"
	"errors"
	"sync"
	"testing"
)

// mockKeyStore is a simple in-memory KeyStore for testing.
type mockKeyStore struct {
	mu   sync.Mutex
	data map[string][]byte
	name string
}

func newMock(name string) *mockKeyStore {
	return &mockKeyStore{data: make(map[string][]byte), name: name}
}

func (m *mockKeyStore) Get(_ context.Context, id string) ([]byte, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	v, ok := m.data[id]
	if !ok {
		return nil, errors.New("not found")
	}
	return v, nil
}

func (m *mockKeyStore) GetFromPath(_ context.Context, _ string) ([]byte, error) {
	return nil, errors.New("GetFromPath not supported on mock keystore")
}

func (m *mockKeyStore) Put(_ context.Context, id string, key []byte) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.data[id] = key
	return nil
}

func (m *mockKeyStore) Delete(_ context.Context, id string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	delete(m.data, id)
	return nil
}

func TestSwappableKeyStore_Delegation(t *testing.T) {
	ctx := context.Background()
	mock := newMock("a")
	s := NewSwappable(mock)

	if err := s.Put(ctx, "server1", []byte("mykey")); err != nil {
		t.Fatalf("Put: %v", err)
	}

	got, err := s.Get(ctx, "server1")
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if string(got) != "mykey" {
		t.Errorf("Get = %q, want %q", got, "mykey")
	}

	if err := s.Delete(ctx, "server1"); err != nil {
		t.Fatalf("Delete: %v", err)
	}

	if _, err := s.Get(ctx, "server1"); err == nil {
		t.Error("expected error after delete, got nil")
	}
}

func TestSwappableKeyStore_Swap(t *testing.T) {
	ctx := context.Background()
	a := newMock("a")
	b := newMock("b")

	// Seed different data in each backend.
	_ = a.Put(ctx, "x", []byte("from-a"))
	_ = b.Put(ctx, "x", []byte("from-b"))

	s := NewSwappable(a)

	v, _ := s.Get(ctx, "x")
	if string(v) != "from-a" {
		t.Errorf("before swap: got %q, want from-a", v)
	}

	s.Swap(b)

	v, _ = s.Get(ctx, "x")
	if string(v) != "from-b" {
		t.Errorf("after swap: got %q, want from-b", v)
	}
}

func TestSwappableKeyStore_Backend(t *testing.T) {
	mock := newMock("local")
	s := NewSwappable(mock)

	if got := s.Backend(); got != "local" {
		t.Errorf("Backend() = %q, want local", got)
	}

	vault := &VaultKeyStore{}
	s.Swap(vault)

	if got := s.Backend(); got != "vault" {
		t.Errorf("Backend() after swap to vault = %q, want vault", got)
	}
}

func TestSwappableKeyStore_ConcurrentAccess(t *testing.T) {
	ctx := context.Background()
	a := newMock("a")
	b := newMock("b")
	_ = a.Put(ctx, "k", []byte("va"))
	_ = b.Put(ctx, "k", []byte("vb"))

	s := NewSwappable(a)

	var wg sync.WaitGroup
	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, _ = s.Get(ctx, "k")
			_ = s.Backend()
		}()
	}
	for i := 0; i < 5; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			s.Swap(a)
			s.Swap(b)
		}()
	}
	wg.Wait()
}
