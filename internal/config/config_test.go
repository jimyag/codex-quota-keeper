package config

import (
	"encoding/json"
	"strings"
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
	if runtime.ScanIntervalDuration != 10*time.Minute {
		t.Fatalf("expected a 10-minute scan interval, got %s", runtime.ScanIntervalDuration)
	}
}

func TestDefaultsDoNotIncludeEnabledSwitch(t *testing.T) {
	data, err := json.Marshal(Defaults())
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(data), `"enabled"`) {
		t.Fatalf("default config must not contain an enabled switch: %s", data)
	}
}

func TestRedeemBeforeExpiryMustBePositive(t *testing.T) {
	cfg := Defaults()
	cfg.ResetCredit.RedeemBeforeExpiry = "0s"
	if _, err := cfg.runtime(); err == nil {
		t.Fatal("expected a non-positive redemption window to be rejected")
	}
}
