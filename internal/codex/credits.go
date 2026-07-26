package codex

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/google/uuid"
)

type authFile struct {
	Tokens struct {
		AccessToken string `json:"access_token"`
		AccountID   string `json:"account_id"`
	} `json:"tokens"`
}

type ConsumeResult struct {
	Code         string `json:"code"`
	WindowsReset int    `json:"windows_reset"`
	Credit       struct {
		Status     string  `json:"status"`
		RedeemedAt *string `json:"redeemed_at"`
	} `json:"credit"`
}

func ListResetCredits(
	ctx context.Context,
	authPath string,
	baseURL string,
	timeout time.Duration,
) (*ResetCreditsSummary, error) {
	auth, err := loadAuth(authPath)
	if err != nil {
		return nil, err
	}

	endpoint := strings.TrimRight(baseURL, "/") + "/wham/rate-limit-reset-credits"
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return nil, err
	}
	setAuthHeaders(request, auth)

	body, err := doRequest(request, timeout)
	if err != nil {
		return nil, fmt.Errorf("list reset credits: %w", err)
	}

	var response providerCreditsResponse
	if err := json.Unmarshal(body, &response); err != nil {
		return nil, fmt.Errorf("decode reset-credit inventory: %w", err)
	}
	credits := make([]ResetCredit, 0, len(response.Credits))
	for _, credit := range response.Credits {
		grantedAt, err := parseProviderTime(credit.GrantedAt)
		if err != nil {
			return nil, fmt.Errorf("decode reset credit granted_at: %w", err)
		}
		expiresAt, err := parseProviderTime(credit.ExpiresAt)
		if err != nil {
			return nil, fmt.Errorf("decode reset credit expires_at: %w", err)
		}
		credits = append(credits, ResetCredit{
			ID:          credit.ID,
			Status:      credit.Status,
			ResetType:   normalizeResetType(credit.ResetType),
			GrantedAt:   valueOrZero(grantedAt),
			ExpiresAt:   expiresAt,
			Title:       credit.Title,
			Description: credit.Description,
		})
	}
	return &ResetCreditsSummary{
		AvailableCount: response.AvailableCount,
		Credits:        &credits,
	}, nil
}

func ConsumeResetCredit(
	ctx context.Context,
	authPath string,
	baseURL string,
	creditID string,
	timeout time.Duration,
) (ConsumeResult, error) {
	auth, err := loadAuth(authPath)
	if err != nil {
		return ConsumeResult{}, err
	}

	payload, err := json.Marshal(map[string]string{
		"credit_id":         creditID,
		"redeem_request_id": uuid.NewString(),
	})
	if err != nil {
		return ConsumeResult{}, err
	}

	endpoint := strings.TrimRight(baseURL, "/") + "/wham/rate-limit-reset-credits/consume"
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(payload))
	if err != nil {
		return ConsumeResult{}, err
	}
	setAuthHeaders(request, auth)
	request.Header.Set("Content-Type", "application/json")

	body, err := doRequest(request, timeout)
	if err != nil {
		return ConsumeResult{}, fmt.Errorf("consume reset credit: %w", err)
	}

	var result ConsumeResult
	if err := json.Unmarshal(body, &result); err != nil {
		return ConsumeResult{}, fmt.Errorf("decode consume response: %w", err)
	}
	if result.Code != "reset" || result.Credit.Status != "redeemed" {
		return ConsumeResult{}, fmt.Errorf(
			"unexpected consume response: code=%q status=%q",
			result.Code,
			result.Credit.Status,
		)
	}
	return result, nil
}

type providerCreditsResponse struct {
	AvailableCount int64                 `json:"available_count"`
	Credits        []providerResetCredit `json:"credits"`
}

type providerResetCredit struct {
	ID          string          `json:"id"`
	Status      string          `json:"status"`
	ResetType   string          `json:"reset_type"`
	GrantedAt   json.RawMessage `json:"granted_at"`
	ExpiresAt   json.RawMessage `json:"expires_at"`
	Title       *string         `json:"title"`
	Description *string         `json:"description"`
}

func loadAuth(path string) (authFile, error) {
	authData, err := os.ReadFile(path)
	if err != nil {
		return authFile{}, fmt.Errorf("read Codex auth file: %w", err)
	}
	var auth authFile
	if err := json.Unmarshal(authData, &auth); err != nil {
		return authFile{}, fmt.Errorf("decode Codex auth file: %w", err)
	}
	if auth.Tokens.AccessToken == "" || auth.Tokens.AccountID == "" {
		return authFile{}, fmt.Errorf(
			"Codex auth file must contain tokens.access_token and tokens.account_id",
		)
	}
	return auth, nil
}

func setAuthHeaders(request *http.Request, auth authFile) {
	request.Header.Set("Authorization", "Bearer "+auth.Tokens.AccessToken)
	request.Header.Set("ChatGPT-Account-Id", auth.Tokens.AccountID)
	request.Header.Set("User-Agent", "codex-quota-keeper/0.1.0")
}

func doRequest(request *http.Request, timeout time.Duration) ([]byte, error) {
	client := &http.Client{Timeout: timeout}
	response, err := client.Do(request)
	if err != nil {
		return nil, err
	}
	defer response.Body.Close()

	body, err := io.ReadAll(io.LimitReader(response.Body, 1<<20))
	if err != nil {
		return nil, err
	}
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		message := strings.TrimSpace(string(body))
		if len(message) > 4096 {
			message = message[:4096]
		}
		return nil, fmt.Errorf("HTTP %d: %s", response.StatusCode, message)
	}
	return body, nil
}

func parseProviderTime(raw json.RawMessage) (*int64, error) {
	if len(raw) == 0 || bytes.Equal(raw, []byte("null")) {
		return nil, nil
	}
	var unix int64
	if err := json.Unmarshal(raw, &unix); err == nil {
		return &unix, nil
	}
	var value string
	if err := json.Unmarshal(raw, &value); err != nil {
		return nil, err
	}
	parsed, err := time.Parse(time.RFC3339Nano, value)
	if err != nil {
		return nil, err
	}
	unix = parsed.Unix()
	return &unix, nil
}

func normalizeResetType(value string) string {
	if value == "codex_rate_limits" {
		return "codexRateLimits"
	}
	return value
}

func valueOrZero(value *int64) int64 {
	if value == nil {
		return 0
	}
	return *value
}
