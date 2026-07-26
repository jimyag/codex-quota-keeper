package codex

type RateLimitsResponse struct {
	RateLimits            RateLimitSnapshot            `json:"rateLimits"`
	RateLimitResetCredits *ResetCreditsSummary         `json:"rateLimitResetCredits"`
	RateLimitsByLimitID   map[string]RateLimitSnapshot `json:"rateLimitsByLimitId"`
}

type RateLimitSnapshot struct {
	LimitID   *string          `json:"limitId"`
	LimitName *string          `json:"limitName"`
	PlanType  *string          `json:"planType"`
	Primary   *RateLimitWindow `json:"primary"`
	Secondary *RateLimitWindow `json:"secondary"`
}

type RateLimitWindow struct {
	UsedPercent        int    `json:"usedPercent"`
	ResetsAt           *int64 `json:"resetsAt"`
	WindowDurationMins *int64 `json:"windowDurationMins"`
}

type ResetCreditsSummary struct {
	AvailableCount int64          `json:"availableCount"`
	Credits        *[]ResetCredit `json:"credits"`
}

type ResetCredit struct {
	ID          string  `json:"id"`
	Status      string  `json:"status"`
	ResetType   string  `json:"resetType"`
	GrantedAt   int64   `json:"grantedAt"`
	ExpiresAt   *int64  `json:"expiresAt"`
	Title       *string `json:"title"`
	Description *string `json:"description"`
}
