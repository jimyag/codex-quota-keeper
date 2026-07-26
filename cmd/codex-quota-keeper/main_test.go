package main

import (
	"bytes"
	"strings"
	"testing"
	"time"

	"github.com/jimyag/codex-quota-keeper/internal/policy"
)

func TestPrintHumanWritesOneConsoleLine(t *testing.T) {
	var output bytes.Buffer
	printHuman(
		newConsoleLogger(&output),
		time.Date(2026, 7, 26, 23, 43, 46, 0, time.FixedZone("CST", 8*60*60)),
		false,
		nil,
		policy.Result{Messages: []string{"automatic redemption is disabled"}},
		nil,
	)

	got := output.String()
	if strings.Count(got, "\n") != 1 {
		t.Fatalf("expected one log line, got %q", got)
	}
	for _, want := range []string{
		"INF scan",
		"mode=dry-run",
		"available=unknown",
		"next_expiry=none",
		"action=none",
		`info="automatic redemption is disabled"`,
	} {
		if !strings.Contains(got, want) {
			t.Fatalf("expected log output to contain %q, got %q", want, got)
		}
	}
}
