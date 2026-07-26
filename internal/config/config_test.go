package config

import (
	"testing"
	"time"
)

func TestDefaultRedeemBeforeExpiry(t *testing.T) {
	runtime, err := Defaults().runtime()
	if err != nil {
		t.Fatal(err)
	}
	if runtime.ResetRedeemBeforeDuration != 10*time.Minute {
		t.Fatalf(
			"expected a 10-minute redemption window, got %s",
			runtime.ResetRedeemBeforeDuration,
		)
	}
}

func TestRedeemBeforeExpiryMustBePositive(t *testing.T) {
	cfg := Defaults()
	cfg.ResetCredit.RedeemBeforeExpiry = "0s"
	if _, err := cfg.runtime(); err == nil {
		t.Fatal("expected a non-positive redemption window to be rejected")
	}
}
