package store

import (
	"strings"
	"testing"
)

func TestStoreAndGetHostKey(t *testing.T) {
	s := testStore(t)

	srv, err := s.CreateServer("hk-test", "hk.example.com", 22, "root")
	if err != nil {
		t.Fatalf("CreateServer failed: %v", err)
	}

	key := []byte("fake-wire-format-host-key")
	fingerprint := "SHA256:abcdefghij"

	if err := s.StoreHostKey(srv.ID, key, fingerprint); err != nil {
		t.Fatalf("StoreHostKey failed: %v", err)
	}

	gotKey, gotFP, err := s.GetHostKey(srv.ID)
	if err != nil {
		t.Fatalf("GetHostKey failed: %v", err)
	}
	if string(gotKey) != string(key) {
		t.Errorf("hostKey = %q, want %q", gotKey, key)
	}
	if gotFP != fingerprint {
		t.Errorf("fingerprint = %q, want %q", gotFP, fingerprint)
	}
}

func TestGetHostKeyMissing(t *testing.T) {
	s := testStore(t)

	srv, err := s.CreateServer("hk-missing", "hk2.example.com", 22, "root")
	if err != nil {
		t.Fatalf("CreateServer failed: %v", err)
	}

	_, _, err = s.GetHostKey(srv.ID)
	if err == nil {
		t.Fatal("expected error for missing host key, got nil")
	}
	if !strings.Contains(err.Error(), "no host key") {
		t.Errorf("error = %q, want to contain %q", err.Error(), "no host key")
	}
}

func TestDeleteHostKey(t *testing.T) {
	s := testStore(t)

	srv, err := s.CreateServer("hk-del", "hk3.example.com", 22, "root")
	if err != nil {
		t.Fatalf("CreateServer failed: %v", err)
	}

	if err := s.StoreHostKey(srv.ID, []byte("key"), "fp"); err != nil {
		t.Fatalf("StoreHostKey failed: %v", err)
	}

	if err := s.DeleteHostKey(srv.ID); err != nil {
		t.Fatalf("DeleteHostKey failed: %v", err)
	}

	_, _, err = s.GetHostKey(srv.ID)
	if err == nil {
		t.Fatal("expected error after delete, got nil")
	}
	if !strings.Contains(err.Error(), "no host key") {
		t.Errorf("error = %q, want to contain %q", err.Error(), "no host key")
	}
}

func TestDeleteHostKeyNonexistent(t *testing.T) {
	s := testStore(t)

	// Should not error even if row doesn't exist.
	if err := s.DeleteHostKey("nonexistent-id"); err != nil {
		t.Fatalf("DeleteHostKey on nonexistent should not fail: %v", err)
	}
}

func TestStoreHostKeyUpsert(t *testing.T) {
	s := testStore(t)

	srv, err := s.CreateServer("hk-upsert", "hk4.example.com", 22, "root")
	if err != nil {
		t.Fatalf("CreateServer failed: %v", err)
	}

	if err := s.StoreHostKey(srv.ID, []byte("key-v1"), "fp-v1"); err != nil {
		t.Fatalf("first StoreHostKey failed: %v", err)
	}

	// Upsert with a new key.
	if err := s.StoreHostKey(srv.ID, []byte("key-v2"), "fp-v2"); err != nil {
		t.Fatalf("second StoreHostKey (upsert) failed: %v", err)
	}

	_, gotFP, err := s.GetHostKey(srv.ID)
	if err != nil {
		t.Fatalf("GetHostKey after upsert failed: %v", err)
	}
	if gotFP != "fp-v2" {
		t.Errorf("fingerprint after upsert = %q, want %q", gotFP, "fp-v2")
	}
}
