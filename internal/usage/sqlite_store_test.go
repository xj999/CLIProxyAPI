package usage

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	coreusage "github.com/router-for-me/CLIProxyAPI/v6/sdk/cliproxy/usage"
)

func TestSQLiteStoreQuerySnapshotByRange(t *testing.T) {
	now := time.Date(2026, 4, 30, 12, 0, 0, 0, time.UTC)
	store, err := NewSQLiteStore(filepath.Join(t.TempDir(), "usage.sqlite"), SQLiteStoreOptions{
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

	records := []coreusage.Record{
		{
			APIKey:             "client-a",
			Model:              "gpt-5.4",
			Source:             "provider-a",
			AuthIndex:          "auth-a",
			ClientAPIKeyID:     "k:client-a",
			ClientAPIKeyMasked: "cl*****-a",
			RequestedAt:        now.Add(-2 * time.Hour),
			Latency:            1500 * time.Millisecond,
			Detail: coreusage.Detail{
				InputTokens:  100,
				OutputTokens: 20,
				TotalTokens:  120,
			},
		},
		{
			APIKey:             "client-a",
			Model:              "gpt-5.4",
			Source:             "provider-a",
			AuthIndex:          "auth-a",
			ClientAPIKeyID:     "k:client-a",
			ClientAPIKeyMasked: "cl*****-a",
			RequestedAt:        now.Add(-25 * time.Hour),
			Latency:            2 * time.Second,
			Failed:             true,
			Detail: coreusage.Detail{
				InputTokens:  80,
				OutputTokens: 0,
				TotalTokens:  80,
			},
		},
		{
			APIKey:             "client-a",
			Model:              "gpt-5.5",
			Source:             "provider-b",
			AuthIndex:          "auth-b",
			ClientAPIKeyID:     "k:client-a",
			ClientAPIKeyMasked: "cl*****-a",
			RequestedAt:        now.Add(-10 * 24 * time.Hour),
			Latency:            3 * time.Second,
			Detail: coreusage.Detail{
				InputTokens:  200,
				OutputTokens: 40,
				TotalTokens:  240,
			},
		},
	}
	for _, record := range records {
		if err := store.Record(context.Background(), record); err != nil {
			t.Fatalf("Record error: %v", err)
		}
	}

	last24h, err := store.QuerySnapshot(UsageQuery{
		Range: "24h",
		Now:   now,
	})
	if err != nil {
		t.Fatalf("QuerySnapshot(24h) error: %v", err)
	}
	if last24h.TotalRequests != 1 {
		t.Fatalf("24h total_requests = %d, want 1", last24h.TotalRequests)
	}
	if last24h.SuccessCount != 1 || last24h.FailureCount != 0 {
		t.Fatalf("24h success/failure = %d/%d, want 1/0", last24h.SuccessCount, last24h.FailureCount)
	}
	if got := len(last24h.APIs["client-a"].Models["gpt-5.4"].Details); got != 1 {
		t.Fatalf("24h details len = %d, want 1", got)
	}

	allTime, err := store.QuerySnapshot(UsageQuery{
		Range: "all",
		Now:   now,
	})
	if err != nil {
		t.Fatalf("QuerySnapshot(all) error: %v", err)
	}
	if allTime.TotalRequests != 3 {
		t.Fatalf("all total_requests = %d, want 3", allTime.TotalRequests)
	}
	if allTime.SuccessCount != 2 || allTime.FailureCount != 1 {
		t.Fatalf("all success/failure = %d/%d, want 2/1", allTime.SuccessCount, allTime.FailureCount)
	}
	if got := allTime.TotalTokens; got != 440 {
		t.Fatalf("all total_tokens = %d, want 440", got)
	}
	if got := len(allTime.APIs["client-a"].Models["gpt-5.5"].Details); got != 0 {
		t.Fatalf("all old detail retention len = %d, want 0", got)
	}
	if got := allTime.APIs["client-a"].Models["gpt-5.5"].TotalRequests; got != 1 {
		t.Fatalf("all gpt-5.5 total_requests = %d, want 1", got)
	}
}

func TestSQLiteStoreQuerySnapshotNaturalRanges(t *testing.T) {
	now := time.Date(2026, 5, 15, 10, 0, 0, 0, time.UTC)
	store, err := NewSQLiteStore(filepath.Join(t.TempDir(), "usage.sqlite"), SQLiteStoreOptions{
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

	records := []coreusage.Record{
		{
			APIKey:      "client-a",
			Model:       "gpt-5.5",
			Source:      "provider-a",
			AuthIndex:   "auth-a",
			RequestedAt: now.Add(-2 * time.Hour),
			Detail: coreusage.Detail{
				InputTokens:  100,
				OutputTokens: 20,
				TotalTokens:  120,
			},
		},
		{
			APIKey:      "client-a",
			Model:       "gpt-5.5",
			Source:      "provider-a",
			AuthIndex:   "auth-a",
			RequestedAt: time.Date(2026, 5, 9, 13, 0, 0, 0, time.UTC),
			Failed:      true,
			Detail: coreusage.Detail{
				InputTokens:  80,
				OutputTokens: 0,
				TotalTokens:  80,
			},
		},
		{
			APIKey:      "client-b",
			Model:       "claude-opus-4-1",
			Source:      "provider-b",
			AuthIndex:   "auth-b",
			RequestedAt: time.Date(2026, 5, 2, 9, 30, 0, 0, time.UTC),
			Detail: coreusage.Detail{
				InputTokens:  200,
				OutputTokens: 40,
				TotalTokens:  240,
			},
		},
		{
			APIKey:      "client-c",
			Model:       "claude-sonnet-4",
			Source:      "provider-c",
			AuthIndex:   "auth-c",
			RequestedAt: time.Date(2026, 4, 30, 8, 0, 0, 0, time.UTC),
			Detail: coreusage.Detail{
				InputTokens:  50,
				OutputTokens: 10,
				TotalTokens:  60,
			},
		},
	}
	for _, record := range records {
		if err := store.Record(context.Background(), record); err != nil {
			t.Fatalf("Record error: %v", err)
		}
	}

	today, err := store.QuerySnapshot(UsageQuery{
		Range: "today",
		Now:   now,
	})
	if err != nil {
		t.Fatalf("QuerySnapshot(today) error: %v", err)
	}
	if today.TotalRequests != 1 || today.TotalTokens != 120 {
		t.Fatalf("today totals = %d requests / %d tokens, want 1 / 120", today.TotalRequests, today.TotalTokens)
	}

	last7d, err := store.QuerySnapshot(UsageQuery{
		Range: "7d",
		Now:   now,
	})
	if err != nil {
		t.Fatalf("QuerySnapshot(7d) error: %v", err)
	}
	if last7d.TotalRequests != 2 {
		t.Fatalf("7d total_requests = %d, want 2", last7d.TotalRequests)
	}
	if last7d.SuccessCount != 1 || last7d.FailureCount != 1 {
		t.Fatalf("7d success/failure = %d/%d, want 1/1", last7d.SuccessCount, last7d.FailureCount)
	}
	if got := last7d.TotalTokens; got != 200 {
		t.Fatalf("7d total_tokens = %d, want 200", got)
	}
	if got := len(last7d.APIs["client-a"].Models["gpt-5.5"].Details); got != 2 {
		t.Fatalf("7d details len = %d, want 2", got)
	}

	thisMonth, err := store.QuerySnapshot(UsageQuery{
		Range: "this_month",
		Now:   now,
	})
	if err != nil {
		t.Fatalf("QuerySnapshot(this_month) error: %v", err)
	}
	if thisMonth.TotalRequests != 3 {
		t.Fatalf("this_month total_requests = %d, want 3", thisMonth.TotalRequests)
	}
	if got := thisMonth.TotalTokens; got != 440 {
		t.Fatalf("this_month total_tokens = %d, want 440", got)
	}
	if got := thisMonth.APIs["client-b"].Models["claude-opus-4-1"].TotalRequests; got != 1 {
		t.Fatalf("this_month client-b requests = %d, want 1", got)
	}
	if got := len(thisMonth.APIs["client-b"].Models["claude-opus-4-1"].Details); got != 0 {
		t.Fatalf("this_month retained details len = %d, want 0", got)
	}
	if got := thisMonth.RequestsByDay["2026-05-02"]; got != 1 {
		t.Fatalf("this_month requests_by_day[2026-05-02] = %d, want 1", got)
	}
	if got := thisMonth.TokensByDay["2026-05-02"]; got != 240 {
		t.Fatalf("this_month tokens_by_day[2026-05-02] = %d, want 240", got)
	}
	if got := thisMonth.RequestsByDay["2026-04-30"]; got != 0 {
		t.Fatalf("this_month requests_by_day should exclude prior month, got %d", got)
	}
}

func TestSQLiteStoreImportSnapshotPreservesRecentDetailsOnly(t *testing.T) {
	now := time.Date(2026, 4, 30, 12, 0, 0, 0, time.UTC)
	store, err := NewSQLiteStore(filepath.Join(t.TempDir(), "usage.sqlite"), SQLiteStoreOptions{
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

	snapshot := StatisticsSnapshot{
		APIs: map[string]APISnapshot{
			"client-import": {
				Models: map[string]ModelSnapshot{
					"claude-opus-4-7": {
						Details: []RequestDetail{
							{
								Timestamp: now.Add(-48 * time.Hour),
								Source:    "provider-c",
								AuthIndex: "auth-c",
								Tokens: TokenStats{
									InputTokens:  50,
									OutputTokens: 10,
									TotalTokens:  60,
								},
							},
							{
								Timestamp: now.Add(-14 * 24 * time.Hour),
								Source:    "provider-c",
								AuthIndex: "auth-c",
								Tokens: TokenStats{
									InputTokens:  30,
									OutputTokens: 5,
									TotalTokens:  35,
								},
								Failed: true,
							},
						},
					},
				},
			},
		},
	}

	result, err := store.ImportSnapshot(snapshot)
	if err != nil {
		t.Fatalf("ImportSnapshot error: %v", err)
	}
	if result.Added != 2 || result.Skipped != 0 {
		t.Fatalf("ImportSnapshot result = %+v, want added=2 skipped=0", result)
	}

	queried, err := store.QuerySnapshot(UsageQuery{
		Range: "all",
		Now:   now,
	})
	if err != nil {
		t.Fatalf("QuerySnapshot(all) error: %v", err)
	}
	if queried.TotalRequests != 2 {
		t.Fatalf("all total_requests = %d, want 2", queried.TotalRequests)
	}
	if got := len(queried.APIs["client-import"].Models["claude-opus-4-7"].Details); got != 1 {
		t.Fatalf("recent details len = %d, want 1", got)
	}
}
