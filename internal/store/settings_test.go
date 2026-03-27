package store

import "testing"

func TestSetAndGetSetting(t *testing.T) {
	s := testStore(t)

	if err := s.SetSetting("foo", "bar"); err != nil {
		t.Fatalf("SetSetting failed: %v", err)
	}

	got, err := s.GetSetting("foo")
	if err != nil {
		t.Fatalf("GetSetting failed: %v", err)
	}
	if got != "bar" {
		t.Errorf("GetSetting = %q, want %q", got, "bar")
	}
}

func TestGetSettingNotFound(t *testing.T) {
	s := testStore(t)

	got, err := s.GetSetting("nonexistent")
	if err != nil {
		t.Fatalf("GetSetting failed: %v", err)
	}
	if got != "" {
		t.Errorf("GetSetting = %q, want %q", got, "")
	}
}

func TestGetAllSettings(t *testing.T) {
	s := testStore(t)

	_ = s.SetSetting("k1", "v1")
	_ = s.SetSetting("k2", "v2")
	_ = s.SetSetting("k3", "v3")

	all, err := s.GetAllSettings()
	if err != nil {
		t.Fatalf("GetAllSettings failed: %v", err)
	}
	if len(all) != 3 {
		t.Errorf("len = %d, want 3", len(all))
	}
	if all["k1"] != "v1" {
		t.Errorf("k1 = %q, want %q", all["k1"], "v1")
	}
	if all["k2"] != "v2" {
		t.Errorf("k2 = %q, want %q", all["k2"], "v2")
	}
	if all["k3"] != "v3" {
		t.Errorf("k3 = %q, want %q", all["k3"], "v3")
	}
}

func TestDeleteSetting(t *testing.T) {
	s := testStore(t)

	_ = s.SetSetting("to-delete", "value")

	if err := s.DeleteSetting("to-delete"); err != nil {
		t.Fatalf("DeleteSetting failed: %v", err)
	}

	got, err := s.GetSetting("to-delete")
	if err != nil {
		t.Fatalf("GetSetting after delete failed: %v", err)
	}
	if got != "" {
		t.Errorf("GetSetting after delete = %q, want empty", got)
	}
}

func TestDeleteSettingNotFound(t *testing.T) {
	s := testStore(t)

	// Deleting a non-existent key should not error
	if err := s.DeleteSetting("ghost"); err != nil {
		t.Fatalf("DeleteSetting on non-existent key failed: %v", err)
	}
}

func TestSetSettingOverwrite(t *testing.T) {
	s := testStore(t)

	_ = s.SetSetting("key", "original")
	_ = s.SetSetting("key", "updated")

	got, err := s.GetSetting("key")
	if err != nil {
		t.Fatalf("GetSetting failed: %v", err)
	}
	if got != "updated" {
		t.Errorf("GetSetting = %q, want %q", got, "updated")
	}
}
