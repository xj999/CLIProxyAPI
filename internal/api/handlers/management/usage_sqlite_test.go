package management

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/router-for-me/CLIProxyAPI/v6/internal/config"
	"github.com/router-for-me/CLIProxyAPI/v6/internal/usage"
	coreusage "github.com/router-for-me/CLIProxyAPI/v6/sdk/cliproxy/usage"
)

func TestGetUsageStatisticsUsesSQLiteStoreRangeQuery(t *testing.T) {
	gin.SetMode(gin.TestMode)

	now := time.Date(2026, 4, 30, 12, 0, 0, 0, time.UTC)
	store, err := usage.NewSQLiteStore(filepath.Join(t.TempDir(), "usage.sqlite"), usage.SQLiteStoreOptions{
		RecentDetailRetention: 7 * 24 * time.Hour,
		Now: func() time.Time {
			return now
		},
	})
	if err != nil {
		t.Fatalf("NewSQLiteStore error: %v", err)
	}
	defer func() {
		if err := store.Close(); err != nil {
			t.Fatalf("Close error: %v", err)
		}
	}()

	for _, record := range []coreusage.Record{
		{
			APIKey:      "client-a",
			Model:       "gpt-5.4",
			RequestedAt: now.Add(-2 * time.Hour),
			Detail: coreusage.Detail{
				InputTokens:  10,
				OutputTokens: 20,
				TotalTokens:  30,
			},
		},
		{
			APIKey:      "client-a",
			Model:       "gpt-5.4",
			RequestedAt: now.Add(-26 * time.Hour),
			Failed:      true,
			Detail: coreusage.Detail{
				InputTokens:  40,
				OutputTokens: 0,
				TotalTokens:  40,
			},
		},
	} {
		if err := store.Record(nil, record); err != nil {
			t.Fatalf("Record error: %v", err)
		}
	}

	handler := NewHandlerWithoutConfigFilePath(&config.Config{}, nil)
	handler.SetUsageStore(store)

	rec := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(rec)
	ctx.Request = httptest.NewRequest(http.MethodGet, "/v0/management/usage?range=24h", nil)

	handler.GetUsageStatistics(ctx)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200, body=%s", rec.Code, rec.Body.String())
	}

	var payload struct {
		Usage usage.StatisticsSnapshot `json:"usage"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &payload); err != nil {
		t.Fatalf("unmarshal response: %v", err)
	}
	if payload.Usage.TotalRequests != 1 {
		t.Fatalf("24h total_requests = %d, want 1", payload.Usage.TotalRequests)
	}
}
