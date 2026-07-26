package policy

import (
	"testing"
	"time"

	"github.com/jimyag/codex-quota-keeper/internal/codex"
	"github.com/jimyag/codex-quota-keeper/internal/config"
	keeperstate "github.com/jimyag/codex-quota-keeper/internal/state"
)

func TestWindowStartsOnceAfterObservedReset(t *testing.T) {
	now := time.Date(2026, 7, 26, 12, 0, 0, 0, time.Local)
	resetAt := now.Add(-time.Minute).Unix()
	cfg := testConfig()
	cfg.Window.Enabled = true
	cfg.Window.WorkingHours = "09:00-23:00"
	current := keeperstate.New()
	current.LastPrimaryResetAt = resetAt
	limits := codex.RateLimitsResponse{
		RateLimits: codex.RateLimitSnapshot{
			Primary: &codex.RateLimitWindow{
				UsedPercent: 100,
				ResetsAt:    &resetAt,
			},
		},
	}

	result := Evaluate(now, cfg, limits, &current)
	if result.WindowAction == nil {
		t.Fatal("expected a window action")
	}

	MarkWindowStarted(&current, *result.WindowAction, now)
	result = Evaluate(now.Add(time.Minute), cfg, limits, &current)
	if result.WindowAction != nil {
		t.Fatal("expected the observed reset to trigger only once")
	}
}

func TestWindowDoesNotStartOutsideWorkingHours(t *testing.T) {
	now := time.Date(2026, 7, 26, 3, 0, 0, 0, time.Local)
	resetAt := now.Add(-time.Minute).Unix()
	cfg := testConfig()
	cfg.Window.Enabled = true
	cfg.Window.WorkingHours = "09:00-23:00"
	current := keeperstate.New()
	limits := codex.RateLimitsResponse{
		RateLimits: codex.RateLimitSnapshot{
			Primary: &codex.RateLimitWindow{ResetsAt: &resetAt},
		},
	}

	result := Evaluate(now, cfg, limits, &current)
	if result.WindowAction != nil {
		t.Fatal("expected no window action outside working hours")
	}
}

func TestCreditRedeemsOnlyNearExpiry(t *testing.T) {
	now := time.Date(2026, 7, 26, 12, 0, 0, 0, time.UTC)
	expiresAt := now.Add(9 * time.Minute).Unix()
	credits := []codex.ResetCredit{{
		ID:        "credit-secret-id",
		Status:    "available",
		ResetType: "codexRateLimits",
		ExpiresAt: &expiresAt,
	}}
	cfg := testConfig()
	cfg.ResetCredit.Enabled = true
	cfg.ResetRedeemBeforeDuration = 10 * time.Minute
	current := keeperstate.New()
	limits := codex.RateLimitsResponse{
		RateLimitResetCredits: &codex.ResetCreditsSummary{
			AvailableCount: 1,
			Credits:        &credits,
		},
	}

	result := Evaluate(now, cfg, limits, &current)
	if result.CreditAction == nil {
		t.Fatal("expected a near-expiry credit action")
	}
	if result.CreditAction.CreditHash != CreditHash("credit-secret-id") {
		t.Fatal("expected a stable hashed credit ID")
	}
}

func TestCreditWaitsUntilNearExpiry(t *testing.T) {
	now := time.Now()
	expiresAt := now.Add(time.Hour).Unix()
	credits := []codex.ResetCredit{{
		ID:        "credit-id",
		Status:    "available",
		ExpiresAt: &expiresAt,
	}}
	cfg := testConfig()
	cfg.ResetCredit.Enabled = true
	cfg.ResetRedeemBeforeDuration = 10 * time.Minute
	current := keeperstate.New()
	limits := codex.RateLimitsResponse{
		RateLimits: codex.RateLimitSnapshot{
			Secondary: &codex.RateLimitWindow{UsedPercent: 100},
		},
		RateLimitResetCredits: &codex.ResetCreditsSummary{
			AvailableCount: 1,
			Credits:        &credits,
		},
	}

	result := Evaluate(now, cfg, limits, &current)
	if result.CreditAction != nil {
		t.Fatal("expected a credit with more than ten minutes remaining to be left for manual use")
	}
}

func TestCreditWaitsAtExactExpiryThreshold(t *testing.T) {
	now := time.Date(2026, 7, 26, 12, 0, 0, 0, time.UTC)
	expiresAt := now.Add(10 * time.Minute).Unix()
	credits := []codex.ResetCredit{{
		ID:        "threshold-credit",
		Status:    "available",
		ExpiresAt: &expiresAt,
	}}
	cfg := testConfig()
	cfg.ResetCredit.Enabled = true
	cfg.ResetRedeemBeforeDuration = 10 * time.Minute
	current := keeperstate.New()
	limits := codex.RateLimitsResponse{
		RateLimitResetCredits: &codex.ResetCreditsSummary{
			AvailableCount: 1,
			Credits:        &credits,
		},
	}

	result := Evaluate(now, cfg, limits, &current)
	if result.CreditAction != nil {
		t.Fatal("expected redemption only when remaining time is less than the threshold")
	}
}

func TestRedeemedCreditIsNotRedeemedAgain(t *testing.T) {
	now := time.Date(2026, 7, 26, 12, 0, 0, 0, time.UTC)
	expiresAt := now.Add(9 * time.Minute).Unix()
	credit := codex.ResetCredit{
		ID:        "expiring-credit",
		Status:    "available",
		ExpiresAt: &expiresAt,
	}
	credits := []codex.ResetCredit{credit}
	cfg := testConfig()
	cfg.ResetCredit.Enabled = true
	cfg.ResetRedeemBeforeDuration = 10 * time.Minute
	current := keeperstate.New()
	current.RedeemedCredits[CreditHash(credit.ID)] = now
	limits := codex.RateLimitsResponse{
		RateLimitResetCredits: &codex.ResetCreditsSummary{
			AvailableCount: 1,
			Credits:        &credits,
		},
	}

	result := Evaluate(now, cfg, limits, &current)
	if result.CreditAction != nil {
		t.Fatal("expected an already redeemed credit to remain ignored")
	}
}

func testConfig() config.Runtime {
	return config.Runtime{
		Config: config.Defaults(),
	}
}
