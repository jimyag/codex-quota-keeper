package policy

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"sort"
	"time"

	"github.com/jimyag/codex-quota-keeper/internal/codex"
	keeperstate "github.com/jimyag/codex-quota-keeper/internal/state"
)

type Result struct {
	CreditAction *CreditAction `json:"credit_action,omitempty"`
	Messages     []string      `json:"messages"`
}

type CreditAction struct {
	Credit     codex.ResetCredit `json:"-"`
	CreditHash string            `json:"credit_hash"`
	ExpiresAt  time.Time         `json:"expires_at"`
}

func Evaluate(
	now time.Time,
	redeemBeforeExpiry time.Duration,
	summary *codex.ResetCreditsSummary,
	current *keeperstate.State,
) Result {
	if summary == nil || summary.Credits == nil {
		return Result{Messages: []string{"reset credit: no detailed credit inventory available"}}
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
		if untilExpiry <= 0 || untilExpiry >= redeemBeforeExpiry {
			continue
		}

		hash := CreditHash(credit.ID)
		if _, redeemed := current.RedeemedCredits[hash]; redeemed {
			continue
		}
		return Result{
			CreditAction: &CreditAction{
				Credit:     credit,
				CreditHash: hash,
				ExpiresAt:  expiresAt,
			},
			Messages: []string{fmt.Sprintf(
				"reset credit %s expires in %s; redeeming to avoid wasting it",
				hash,
				untilExpiry.Round(time.Second),
			)},
		}
	}
	return Result{}
}

func MarkCreditRedeemed(current *keeperstate.State, action CreditAction, now time.Time) {
	current.RedeemedCredits[action.CreditHash] = now
}

func CreditHash(id string) string {
	sum := sha256.Sum256([]byte(id))
	return hex.EncodeToString(sum[:8])
}
