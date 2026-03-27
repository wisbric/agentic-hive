package terminal

import (
	"encoding/json"
	"testing"
)

func TestResizeMsgParsing(t *testing.T) {
	msg := `{"type":"resize","cols":120,"rows":40}`
	var resize resizeMsg
	if err := json.Unmarshal([]byte(msg), &resize); err != nil {
		t.Fatalf("unmarshal failed: %v", err)
	}
	if resize.Type != "resize" {
		t.Errorf("Type = %q, want %q", resize.Type, "resize")
	}
	if resize.Cols != 120 {
		t.Errorf("Cols = %d, want 120", resize.Cols)
	}
	if resize.Rows != 40 {
		t.Errorf("Rows = %d, want 40", resize.Rows)
	}
}
