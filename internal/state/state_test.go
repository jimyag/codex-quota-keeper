package state

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestSaveAndLoad(t *testing.T) {
	path := filepath.Join(t.TempDir(), "nested", "state.json")
	input := New()
	input.LastPrimaryResetAt = 123
	input.PendingCredits["abc"] = Pending{
		FirstSeen: time.Unix(100, 0),
		ExpiresAt: time.Unix(200, 0),
	}

	if err := input.Save(path); err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0o600 {
		t.Fatalf("expected state mode 0600, got %o", info.Mode().Perm())
	}

	output, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if output.LastPrimaryResetAt != input.LastPrimaryResetAt {
		t.Fatalf("unexpected reset timestamp: %d", output.LastPrimaryResetAt)
	}
	if _, ok := output.PendingCredits["abc"]; !ok {
		t.Fatal("expected pending credit to survive a round trip")
	}
}
