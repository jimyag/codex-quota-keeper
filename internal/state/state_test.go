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
	input.RedeemedCredits["abc"] = time.Unix(100, 0)

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
	if _, ok := output.RedeemedCredits["abc"]; !ok {
		t.Fatal("expected redeemed credit to survive a round trip")
	}
}
