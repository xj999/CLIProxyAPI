package usage

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	coreusage "github.com/router-for-me/CLIProxyAPI/v6/sdk/cliproxy/usage"
)

func TestStatisticsPersisterLoadRestoresSnapshot(t *testing.T) {
	path := filepath.Join(t.TempDir(), "usage-statistics.json")

	seed := NewRequestStatistics()
	seed.Record(context.Background(), coreusage.Record{
		APIKey:      "key-1",
		Model:       "gpt-5.4",
		RequestedAt: time.Date(2026, time.April, 20, 13, 30, 0, 0, time.UTC),
		Detail: coreusage.Detail{
			InputTokens:  120,
			OutputTokens: 30,
			TotalTokens:  150,
		},
	})

	writer := NewStatisticsPersister(seed, path)
	if err := writer.Flush(); err != nil {
		t.Fatalf("flush snapshot: %v", err)
	}

	restored := NewRequestStatistics()
	reader := NewStatisticsPersister(restored, path)
	loaded, err := reader.Load()
	if err != nil {
		t.Fatalf("load snapshot: %v", err)
	}
	if !loaded {
		t.Fatalf("expected snapshot to be loaded")
	}

	snapshot := restored.Snapshot()
	if snapshot.TotalRequests != 1 {
		t.Fatalf("total requests = %d, want 1", snapshot.TotalRequests)
	}
	if snapshot.TotalTokens != 150 {
		t.Fatalf("total tokens = %d, want 150", snapshot.TotalTokens)
	}
	apiSnapshot, ok := snapshot.APIs["key-1"]
	if !ok {
		t.Fatalf("expected api key snapshot to exist")
	}
	modelSnapshot, ok := apiSnapshot.Models["gpt-5.4"]
	if !ok {
		t.Fatalf("expected model snapshot to exist")
	}
	if len(modelSnapshot.Details) != 1 {
		t.Fatalf("details len = %d, want 1", len(modelSnapshot.Details))
	}
}

func TestStatisticsPersisterStopFlushesPendingSnapshot(t *testing.T) {
	path := filepath.Join(t.TempDir(), "usage-statistics.json")

	stats := NewRequestStatistics()
	persister := NewStatisticsPersister(stats, path)
	persister.debounce = time.Hour
	persister.Start()

	stats.Record(context.Background(), coreusage.Record{
		APIKey:      "key-2",
		Model:       "gpt-5.4",
		RequestedAt: time.Date(2026, time.April, 20, 13, 31, 0, 0, time.UTC),
		Detail: coreusage.Detail{
			InputTokens:  80,
			OutputTokens: 20,
			TotalTokens:  100,
		},
	})
	persister.MarkDirty()

	if err := persister.Stop(); err != nil {
		t.Fatalf("stop persister: %v", err)
	}

	restored := NewRequestStatistics()
	reader := NewStatisticsPersister(restored, path)
	loaded, err := reader.Load()
	if err != nil {
		t.Fatalf("load snapshot after stop: %v", err)
	}
	if !loaded {
		t.Fatalf("expected snapshot file after stop")
	}

	snapshot := restored.Snapshot()
	if snapshot.TotalRequests != 1 {
		t.Fatalf("total requests = %d, want 1", snapshot.TotalRequests)
	}
	if snapshot.TotalTokens != 100 {
		t.Fatalf("total tokens = %d, want 100", snapshot.TotalTokens)
	}
}
