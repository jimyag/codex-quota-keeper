package codex

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/google/uuid"
)

func TestListAndConsumeResetCredit(t *testing.T) {
	const accessToken = "test-access-token"
	const accountID = "test-account"
	const creditID = "credit-123"
	expiry := time.Now().Add(time.Hour).UTC().Truncate(time.Second)

	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if request.Header.Get("Authorization") != "Bearer "+accessToken {
			t.Error("missing access-token header")
		}
		if request.Header.Get("ChatGPT-Account-Id") != accountID {
			t.Error("missing account header")
		}
		switch {
		case request.Method == http.MethodGet &&
			request.URL.Path == "/wham/rate-limit-reset-credits":
			_ = json.NewEncoder(writer).Encode(map[string]any{
				"available_count": 1,
				"credits": []map[string]any{{
					"id":          creditID,
					"status":      "available",
					"reset_type":  "codex_rate_limits",
					"granted_at":  time.Now().Add(-time.Hour).UTC().Format(time.RFC3339),
					"expires_at":  expiry.Format(time.RFC3339),
					"title":       "Reset",
					"description": "Reset Codex limits",
				}},
			})
		case request.Method == http.MethodPost &&
			request.URL.Path == "/wham/rate-limit-reset-credits/consume":
			body, err := io.ReadAll(request.Body)
			if err != nil {
				t.Error(err)
			}
			var payload map[string]string
			if err := json.Unmarshal(body, &payload); err != nil {
				t.Error(err)
			}
			if payload["credit_id"] != creditID || payload["redeem_request_id"] == "" {
				t.Errorf("unexpected consume payload: %#v", payload)
			}
			if _, err := uuid.Parse(payload["redeem_request_id"]); err != nil {
				t.Errorf("invalid redeem request UUID: %v", err)
			}
			_ = json.NewEncoder(writer).Encode(map[string]any{
				"code":          "reset",
				"windows_reset": 1,
				"credit": map[string]any{
					"status":      "redeemed",
					"redeemed_at": time.Now().UTC().Format(time.RFC3339),
				},
			})
		default:
			http.NotFound(writer, request)
		}
	}))
	defer server.Close()

	authPath := writeTestAuth(t, accessToken, accountID)
	credits, err := ListResetCredits(
		context.Background(),
		authPath,
		server.URL,
		time.Second,
	)
	if err != nil {
		t.Fatal(err)
	}
	if credits.AvailableCount != 1 || credits.Credits == nil || len(*credits.Credits) != 1 {
		t.Fatalf("unexpected credits: %#v", credits)
	}
	credit := (*credits.Credits)[0]
	if credit.ID != creditID || credit.ResetType != "codexRateLimits" {
		t.Fatalf("unexpected credit: %#v", credit)
	}
	if credit.ExpiresAt == nil || *credit.ExpiresAt != expiry.Unix() {
		t.Fatalf("unexpected expiry: %#v", credit.ExpiresAt)
	}

	result, err := ConsumeResetCredit(
		context.Background(),
		authPath,
		server.URL,
		credit.ID,
		time.Second,
	)
	if err != nil {
		t.Fatal(err)
	}
	if result.Code != "reset" || result.WindowsReset != 1 {
		t.Fatalf("unexpected consume result: %#v", result)
	}
}

func writeTestAuth(t *testing.T, accessToken, accountID string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "auth.json")
	data, err := json.Marshal(map[string]any{
		"tokens": map[string]string{
			"access_token": accessToken,
			"account_id":   accountID,
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}
