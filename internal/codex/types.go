package codex

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
