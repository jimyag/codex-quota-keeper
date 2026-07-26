package main

import "testing"

func TestLogValue(t *testing.T) {
	tests := map[string]string{
		"none":            "none",
		"redeem a credit": `"redeem a credit"`,
		`contains"quote`:  `"contains\"quote"`,
	}
	for input, want := range tests {
		if got := logValue(input); got != want {
			t.Fatalf("logValue(%q) = %q, want %q", input, got, want)
		}
	}
}
