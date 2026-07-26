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

func TestCreditWaitsForGracePeriod(t *testing.T) {
	now := time.Date(2026, 7, 26, 12, 0, 0, 0, time.UTC)
	expiresAt := now.Add(time.Hour).Unix()
	credits := []codex.ResetCredit{{
		ID:        "credit-secret-id",
		Status:    "available",
		ResetType: "codexRateLimits",
		ExpiresAt: &expiresAt,
	}}
	cfg := testConfig()
	cfg.ResetCredit.Enabled = true
	cfg.ResetCredit.UsageWindow = "secondary"
	cfg.ResetCredit.MinimumUsedPercent = 90
	cfg.ResetExpiresWithinDuration = 2 * time.Hour
	cfg.ResetGracePeriodDuration = 10 * time.Minute
	current := keeperstate.New()
	limits := codex.RateLimitsResponse{
		RateLimits: codex.RateLimitSnapshot{
			Secondary: &codex.RateLimitWindow{UsedPercent: 95},
		},
		RateLimitResetCredits: &codex.ResetCreditsSummary{
			AvailableCount: 1,
			Credits:        &credits,
		},
	}

	first := Evaluate(now, cfg, limits, &current)
	if first.CreditAction != nil {
		t.Fatal("expected the first eligible observation to start the grace period")
	}
	if first.CreditPending == nil {
		t.Fatal("expected pending-credit details for notification")
	}
	if len(current.PendingCredits) != 1 {
		t.Fatalf("expected one pending credit, got %d", len(current.PendingCredits))
	}

	second := Evaluate(now.Add(9*time.Minute), cfg, limits, &current)
	if second.CreditAction != nil {
		t.Fatal("expected no action before the grace period ends")
	}

	third := Evaluate(now.Add(10*time.Minute), cfg, limits, &current)
	if third.CreditAction == nil {
		t.Fatal("expected a credit action after the grace period")
	}
	if third.CreditAction.CreditHash != CreditHash("credit-secret-id") {
		t.Fatal("expected a stable hashed credit ID")
	}
}

func TestCreditRequiresConfiguredUsage(t *testing.T) {
	now := time.Now()
	expiresAt := now.Add(time.Hour).Unix()
	credits := []codex.ResetCredit{{
		ID:        "credit-id",
		Status:    "available",
		ExpiresAt: &expiresAt,
	}}
	cfg := testConfig()
	cfg.ResetCredit.Enabled = true
	cfg.ResetCredit.MinimumUsedPercent = 90
	cfg.ResetExpiresWithinDuration = 2 * time.Hour
	current := keeperstate.New()
	limits := codex.RateLimitsResponse{
		RateLimits: codex.RateLimitSnapshot{
			Secondary: &codex.RateLimitWindow{UsedPercent: 50},
		},
		RateLimitResetCredits: &codex.ResetCreditsSummary{
			AvailableCount: 1,
			Credits:        &credits,
		},
	}

	result := Evaluate(now, cfg, limits, &current)
	if result.CreditAction != nil || len(current.PendingCredits) != 0 {
		t.Fatal("expected low usage to block reset-credit redemption")
	}
}

func TestSkippedCreditIsNotRearmed(t *testing.T) {
	now := time.Now()
	expiresAt := now.Add(time.Hour).Unix()
	credit := codex.ResetCredit{
		ID:        "skipped-credit",
		Status:    "available",
		ExpiresAt: &expiresAt,
	}
	credits := []codex.ResetCredit{credit}
	cfg := testConfig()
	cfg.ResetCredit.Enabled = true
	cfg.ResetExpiresWithinDuration = 2 * time.Hour
	cfg.ResetCredit.MinimumUsedPercent = 90
	current := keeperstate.New()
	current.SkippedCredits[CreditHash(credit.ID)] = now
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
	if result.CreditPending != nil || result.CreditAction != nil {
		t.Fatal("expected a skipped credit to remain skipped")
	}
}

func testConfig() config.Runtime {
	return config.Runtime{
		Config: config.Defaults(),
	}
}
