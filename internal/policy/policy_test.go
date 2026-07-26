package policy

import (
	"testing"
	"time"

	"github.com/jimyag/codex-quota-keeper/internal/codex"
	keeperstate "github.com/jimyag/codex-quota-keeper/internal/state"
)

func TestCreditRedeemsOnlyNearExpiry(t *testing.T) {
	now := time.Date(2026, 7, 26, 12, 0, 0, 0, time.UTC)
	expiresAt := now.Add(9 * time.Minute).Unix()
	credits := []codex.ResetCredit{{
		ID:        "credit-secret-id",
		Status:    "available",
		ResetType: "codexRateLimits",
		ExpiresAt: &expiresAt,
	}}
	current := keeperstate.New()
	summary := &codex.ResetCreditsSummary{
		AvailableCount: 1,
		Credits:        &credits,
	}

	result := Evaluate(now, 10*time.Minute, summary, &current)
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
	current := keeperstate.New()
	summary := &codex.ResetCreditsSummary{
		AvailableCount: 1,
		Credits:        &credits,
	}

	result := Evaluate(now, 10*time.Minute, summary, &current)
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
	current := keeperstate.New()
	summary := &codex.ResetCreditsSummary{
		AvailableCount: 1,
		Credits:        &credits,
	}

	result := Evaluate(now, 10*time.Minute, summary, &current)
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
	current := keeperstate.New()
	current.RedeemedCredits[CreditHash(credit.ID)] = now
	summary := &codex.ResetCreditsSummary{
		AvailableCount: 1,
		Credits:        &credits,
	}

	result := Evaluate(now, 10*time.Minute, summary, &current)
	if result.CreditAction != nil {
		t.Fatal("expected an already redeemed credit to remain ignored")
	}
}
