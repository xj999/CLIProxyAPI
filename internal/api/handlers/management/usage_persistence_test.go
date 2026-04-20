package management

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/router-for-me/CLIProxyAPI/v6/internal/config"
	"github.com/router-for-me/CLIProxyAPI/v6/internal/usage"
)

func TestImportUsageStatisticsTriggersPersistenceSave(t *testing.T) {
	gin.SetMode(gin.TestMode)

	stats := usage.NewRequestStatistics()
	path := filepath.Join(t.TempDir(), "usage-statistics.json")
	persister := usage.NewStatisticsPersister(stats, path)
	persister.SetDebounce(5 * time.Millisecond)
	usage.SetDefaultStatisticsPersister(persister)
	defer usage.SetDefaultStatisticsPersister(nil)

	persister.Start()
	defer func() {
		if err := persister.Stop(); err != nil {
			t.Fatalf("stop persister: %v", err)
		}
	}()

	handler := NewHandlerWithoutConfigFilePath(&config.Config{}, nil)
	handler.SetUsageStatistics(stats)

	payload := usageImportPayload{
		Version: 1,
		Usage: usage.StatisticsSnapshot{
			APIs: map[string]usage.APISnapshot{
				"key-imported": {
					Models: map[string]usage.ModelSnapshot{
						"gpt-5.4": {
							Details: []usage.RequestDetail{
								{
									Timestamp: time.Date(2026, time.April, 20, 13, 32, 0, 0, time.UTC),
									Tokens: usage.TokenStats{
										InputTokens:  50,
										OutputTokens: 25,
										TotalTokens:  75,
									},
								},
							},
						},
					},
				},
			},
		},
	}

	body, err := json.Marshal(payload)
	if err != nil {
		t.Fatalf("marshal payload: %v", err)
	}

	rec := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(rec)
	req := httptest.NewRequest(http.MethodPost, "/v0/management/usage/import", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	ctx.Request = req

	handler.ImportUsageStatistics(ctx)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected import status 200, got %d with body %s", rec.Code, rec.Body.String())
	}

	deadline := time.Now().Add(500 * time.Millisecond)
	for time.Now().Before(deadline) {
		restored := usage.NewRequestStatistics()
		reader := usage.NewStatisticsPersister(restored, path)
		loaded, loadErr := reader.Load()
		if loadErr == nil && loaded && restored.Snapshot().TotalRequests == 1 {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}

	t.Fatalf("expected import to trigger persisted snapshot write")
}
