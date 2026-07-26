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
	WindowAction  *WindowAction  `json:"window_action,omitempty"`
	CreditPending *CreditPending `json:"credit_pending,omitempty"`
	CreditAction  *CreditAction  `json:"credit_action,omitempty"`
	Messages      []string       `json:"messages"`
}

type WindowAction struct {
	ResetAt int64  `json:"reset_at"`
	Key     string `json:"key"`
}

type CreditAction struct {
	Credit      codex.ResetCredit `json:"-"`
	CreditHash  string            `json:"credit_hash"`
	ExpiresAt   time.Time         `json:"expires_at"`
	UsedPercent int               `json:"used_percent"`
}

type CreditPending struct {
	CreditHash string    `json:"credit_hash"`
	ExpiresAt  time.Time `json:"expires_at"`
	ReadyAt    time.Time `json:"ready_at"`
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
		pending, action, messages := evaluateCredits(now, cfg, limits, current)
		result.CreditPending = pending
		result.CreditAction = action
		result.Messages = append(result.Messages, messages...)
	}
	return result
}

func MarkWindowStarted(current *keeperstate.State, action WindowAction, now time.Time) {
	current.WindowStarted[action.Key] = now
}

func MarkCreditRedeemed(current *keeperstate.State, action CreditAction, now time.Time) {
	delete(current.PendingCredits, action.CreditHash)
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
) (*CreditPending, *CreditAction, []string) {
	summary := limits.RateLimitResetCredits
	if summary == nil || summary.Credits == nil {
		return nil, nil, []string{"reset credit: no detailed credit inventory available"}
	}

	used, available := usedPercent(cfg.ResetCredit.UsageWindow, limits.RateLimits)
	if !available {
		return nil, nil, []string{"reset credit: selected usage window is unavailable"}
	}
	if used < cfg.ResetCredit.MinimumUsedPercent {
		return nil, nil, []string{fmt.Sprintf(
			"reset credit: usage is %d%%, below the %d%% threshold",
			used,
			cfg.ResetCredit.MinimumUsedPercent,
		)}
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
		if untilExpiry <= 0 || untilExpiry > cfg.ResetExpiresWithinDuration {
			continue
		}

		hash := CreditHash(credit.ID)
		if _, redeemed := current.RedeemedCredits[hash]; redeemed {
			continue
		}
		if _, skipped := current.SkippedCredits[hash]; skipped {
			continue
		}
		pending, exists := current.PendingCredits[hash]
		if !exists {
			current.PendingCredits[hash] = keeperstate.Pending{
				FirstSeen: now,
				ExpiresAt: expiresAt,
			}
			return &CreditPending{
					CreditHash: hash,
					ExpiresAt:  expiresAt,
					ReadyAt:    now.Add(cfg.ResetGracePeriodDuration),
				}, nil, []string{fmt.Sprintf(
					"reset credit %s is eligible and enters a %s grace period",
					hash,
					cfg.ResetGracePeriodDuration,
				)}
		}
		if now.Sub(pending.FirstSeen) < cfg.ResetGracePeriodDuration {
			return nil, nil, []string{fmt.Sprintf(
				"reset credit %s is pending; grace period ends in %s",
				hash,
				cfg.ResetGracePeriodDuration-now.Sub(pending.FirstSeen),
			)}
		}
		return nil, &CreditAction{
			Credit:      credit,
			CreditHash:  hash,
			ExpiresAt:   expiresAt,
			UsedPercent: used,
		}, nil
	}
	return nil, nil, nil
}

func usedPercent(window string, limits codex.RateLimitSnapshot) (int, bool) {
	primary, primaryOK := percent(limits.Primary)
	secondary, secondaryOK := percent(limits.Secondary)
	switch window {
	case "primary":
		return primary, primaryOK
	case "secondary":
		return secondary, secondaryOK
	case "either":
		switch {
		case primaryOK && secondaryOK:
			return max(primary, secondary), true
		case primaryOK:
			return primary, true
		case secondaryOK:
			return secondary, true
		}
	}
	return 0, false
}

func percent(window *codex.RateLimitWindow) (int, bool) {
	if window == nil {
		return 0, false
	}
	return window.UsedPercent, true
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
