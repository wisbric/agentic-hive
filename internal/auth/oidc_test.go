package auth

import (
	"testing"
)

func TestClaimString(t *testing.T) {
	claims := map[string]any{
		"preferred_username": "stefan",
		"email":             "stefan@example.com",
		"number":            42,
	}

	if got := claimString(claims, "preferred_username"); got != "stefan" {
		t.Errorf("claimString(preferred_username) = %q, want %q", got, "stefan")
	}
	if got := claimString(claims, "missing"); got != "" {
		t.Errorf("claimString(missing) = %q, want empty", got)
	}
	if got := claimString(claims, "number"); got != "" {
		t.Errorf("claimString(number) = %q, want empty (wrong type)", got)
	}
}

func TestClaimStringSlice(t *testing.T) {
	claims := map[string]any{
		"groups": []any{"overlay-admin", "developers"},
		"name":   "stefan",
	}

	groups := claimStringSlice(claims, "groups")
	if len(groups) != 2 {
		t.Fatalf("len(groups) = %d, want 2", len(groups))
	}
	if groups[0] != "overlay-admin" {
		t.Errorf("groups[0] = %q, want %q", groups[0], "overlay-admin")
	}

	if got := claimStringSlice(claims, "missing"); got != nil {
		t.Errorf("claimStringSlice(missing) = %v, want nil", got)
	}
	if got := claimStringSlice(claims, "name"); got != nil {
		t.Errorf("claimStringSlice(name) = %v, want nil (wrong type)", got)
	}
}
