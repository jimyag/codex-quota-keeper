package policy

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"sort"
	"time"

	"github.com/jimyag/codex-quota-keeper/internal/codex"
	"github.com/jimyag/codex-quota-keeper/internal/config"
	keeperstate "github.com/jimyag/codex-quota-keeper/internal/state"
)

type Result struct {
	WindowAction *WindowAction `json:"window_action,omitempty"`
	CreditAction *CreditAction `json:"credit_action,omitempty"`
	Messages     []string      `json:"messages"`
}

type WindowAction struct {
	ResetAt int64  `json:"reset_at"`
	Key     string `json:"key"`
}

type CreditAction struct {
	Credit     codex.ResetCredit `json:"-"`
	CreditHash string            `json:"credit_hash"`
	ExpiresAt  time.Time         `json:"expires_at"`
}

func Evaluate(
	now time.Time,
	cfg config.Runtime,
	limits codex.RateLimitsResponse,
	current *keeperstate.State,
) Result {
	result := Result{}
	hadObservedReset := current.LastPrimaryResetAt != 0
	updateObservedReset(limits.RateLimits.Primary, current)

	if cfg.Window.Enabled {
		if action, message := evaluateWindow(now, cfg, current, hadObservedReset); action != nil {
			result.WindowAction = action
		} else if message != "" {
			result.Messages = append(result.Messages, message)
		}
	}

	if cfg.ResetCredit.Enabled {
		action, messages := evaluateCredits(now, cfg, limits, current)
		result.CreditAction = action
		result.Messages = append(result.Messages, messages...)
	}
	return result
}

func MarkWindowStarted(current *keeperstate.State, action WindowAction, now time.Time) {
	current.WindowStarted[action.Key] = now
}

func MarkCreditRedeemed(current *keeperstate.State, action CreditAction, now time.Time) {
	current.RedeemedCredits[action.CreditHash] = now
}

func CreditHash(id string) string {
	sum := sha256.Sum256([]byte(id))
	return hex.EncodeToString(sum[:8])
}

func updateObservedReset(window *codex.RateLimitWindow, current *keeperstate.State) {
	if window == nil || window.ResetsAt == nil {
		return
	}
	if *window.ResetsAt > current.LastPrimaryResetAt {
		current.LastPrimaryResetAt = *window.ResetsAt
	}
}

func evaluateWindow(
	now time.Time,
	cfg config.Runtime,
	current *keeperstate.State,
	hadObservedReset bool,
) (*WindowAction, string) {
	if !hadObservedReset {
		return nil, "window starter: recorded the first primary reset timestamp"
	}
	if current.LastPrimaryResetAt == 0 {
		return nil, "window starter: waiting to observe a primary reset timestamp"
	}
	if now.Unix() < current.LastPrimaryResetAt {
		return nil, ""
	}
	key := fmt.Sprintf("%d", current.LastPrimaryResetAt)
	if _, ok := current.WindowStarted[key]; ok {
		return nil, ""
	}
	if !withinWorkingHours(now, cfg.Window.WorkingHours) {
		return nil, "window starter: reset passed, but current time is outside working hours"
	}
	return &WindowAction{
		ResetAt: current.LastPrimaryResetAt,
		Key:     key,
	}, ""
}

func evaluateCredits(
	now time.Time,
	cfg config.Runtime,
	limits codex.RateLimitsResponse,
	current *keeperstate.State,
) (*CreditAction, []string) {
	summary := limits.RateLimitResetCredits
	if summary == nil || summary.Credits == nil {
		return nil, []string{"reset credit: no detailed credit inventory available"}
	}

	credits := append([]codex.ResetCredit(nil), (*summary.Credits)...)
	sort.Slice(credits, func(i, j int) bool {
		if credits[i].ExpiresAt == nil {
			return false
		}
		if credits[j].ExpiresAt == nil {
			return true
		}
		return *credits[i].ExpiresAt < *credits[j].ExpiresAt
	})

	for _, credit := range credits {
		if credit.Status != "available" || credit.ExpiresAt == nil {
			continue
		}
		expiresAt := time.Unix(*credit.ExpiresAt, 0)
		untilExpiry := expiresAt.Sub(now)
		if untilExpiry <= 0 || untilExpiry >= cfg.ResetRedeemBeforeDuration {
			continue
		}

		hash := CreditHash(credit.ID)
		if _, redeemed := current.RedeemedCredits[hash]; redeemed {
			continue
		}
		return &CreditAction{
				Credit:     credit,
				CreditHash: hash,
				ExpiresAt:  expiresAt,
			}, []string{fmt.Sprintf(
				"reset credit %s expires in %s; redeeming to avoid wasting it",
				hash,
				untilExpiry.Round(time.Second),
			)}
	}
	return nil, nil
}

func withinWorkingHours(now time.Time, value string) bool {
	start, end, err := config.ParseWorkingHours(value)
	if err != nil {
		return false
	}
	current := time.Duration(now.Hour())*time.Hour + time.Duration(now.Minute())*time.Minute
	if start == end {
		return true
	}
	if start < end {
		return current >= start && current < end
	}
	return current >= start || current < end
}
