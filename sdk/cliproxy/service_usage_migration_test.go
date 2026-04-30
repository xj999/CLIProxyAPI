package cliproxy

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	internalusage "github.com/router-for-me/CLIProxyAPI/v6/internal/usage"
	coreusage "github.com/router-for-me/CLIProxyAPI/v6/sdk/cliproxy/usage"
)

func TestMigrateLegacyUsageSnapshotIfNeededImportsWhenStoreEmpty(t *testing.T) {
	now := time.Date(2026, time.April, 30, 12, 0, 0, 0, time.UTC)
	tempDir := t.TempDir()
	legacyPath := filepath.Join(tempDir, "usage-statistics.json")
	storePath := filepath.Join(tempDir, "usage.sqlite")

	seed := internalusage.NewRequestStatistics()
	seed.Record(context.Background(), coreusage.Record{
		APIKey:      "client-a",
		Model:       "claude-sonnet-4",
		RequestedAt: now.Add(-2 * time.Hour),
		Detail: coreusage.Detail{
			InputTokens:  120,
			OutputTokens: 30,
			TotalTokens:  150,
		},
	})
	persister := internalusage.NewStatisticsPersister(seed, legacyPath)
	if err := persister.Flush(); err != nil {
		t.Fatalf("Flush legacy snapshot: %v", err)
	}

	store, err := internalusage.NewSQLiteStore(storePath, internalusage.SQLiteStoreOptions{
		Now: func() time.Time { return now },
	})
	if err != nil {
		t.Fatalf("NewSQLiteStore error: %v", err)
	}
	defer func() {
		if err := store.Close(); err != nil {
			t.Fatalf("Close error: %v", err)
		}
	}()

	if err := migrateLegacyUsageSnapshotIfNeeded(store, legacyPath); err != nil {
		t.Fatalf("migrateLegacyUsageSnapshotIfNeeded error: %v", err)
	}

	snapshot, err := store.QuerySnapshot(internalusage.UsageQuery{Range: "all", Now: now})
	if err != nil {
		t.Fatalf("QuerySnapshot(all) error: %v", err)
	}
	if snapshot.TotalRequests != 1 {
		t.Fatalf("total requests = %d, want 1", snapshot.TotalRequests)
	}
	if snapshot.TotalTokens != 150 {
		t.Fatalf("total tokens = %d, want 150", snapshot.TotalTokens)
	}
}

func TestMigrateLegacyUsageSnapshotIfNeededSkipsWhenStoreHasData(t *testing.T) {
	now := time.Date(2026, time.April, 30, 12, 0, 0, 0, time.UTC)
	tempDir := t.TempDir()
	legacyPath := filepath.Join(tempDir, "usage-statistics.json")
	storePath := filepath.Join(tempDir, "usage.sqlite")

	seed := internalusage.NewRequestStatistics()
	seed.Record(context.Background(), coreusage.Record{
		APIKey:      "client-legacy",
		Model:       "claude-opus-4-7",
		RequestedAt: now.Add(-3 * time.Hour),
		Detail: coreusage.Detail{
			InputTokens:  200,
			OutputTokens: 50,
			TotalTokens:  250,
		},
	})
	persister := internalusage.NewStatisticsPersister(seed, legacyPath)
	if err := persister.Flush(); err != nil {
		t.Fatalf("Flush legacy snapshot: %v", err)
	}

	store, err := internalusage.NewSQLiteStore(storePath, internalusage.SQLiteStoreOptions{
		Now: func() time.Time { return now },
	})
	if err != nil {
		t.Fatalf("NewSQLiteStore error: %v", err)
	}
	defer func() {
		if err := store.Close(); err != nil {
			t.Fatalf("Close error: %v", err)
		}
	}()

	if err := store.Record(context.Background(), coreusage.Record{
		APIKey:      "client-live",
		Model:       "gpt-5.4",
		RequestedAt: now.Add(-1 * time.Hour),
		Detail: coreusage.Detail{
			InputTokens:  10,
			OutputTokens: 5,
			TotalTokens:  15,
		},
	}); err != nil {
		t.Fatalf("Record existing sqlite data: %v", err)
	}

	if err := migrateLegacyUsageSnapshotIfNeeded(store, legacyPath); err != nil {
		t.Fatalf("migrateLegacyUsageSnapshotIfNeeded error: %v", err)
	}

	snapshot, err := store.QuerySnapshot(internalusage.UsageQuery{Range: "all", Now: now})
	if err != nil {
		t.Fatalf("QuerySnapshot(all) error: %v", err)
	}
	if snapshot.TotalRequests != 1 {
		t.Fatalf("total requests = %d, want 1", snapshot.TotalRequests)
	}
	if snapshot.TotalTokens != 15 {
		t.Fatalf("total tokens = %d, want 15", snapshot.TotalTokens)
	}
	if _, ok := snapshot.APIs["client-legacy"]; ok {
		t.Fatalf("legacy snapshot should not be imported when sqlite already has data")
	}
}
