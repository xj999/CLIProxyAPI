package usage

import (
	"context"
	"testing"
	"time"

	coreusage "github.com/router-for-me/CLIProxyAPI/v6/sdk/cliproxy/usage"
)

func TestRequestStatisticsRecordIncludesClientAPIKeyAndSessionIndex(t *testing.T) {
	t.Parallel()

	stats := NewRequestStatistics()
	stats.Record(context.Background(), coreusage.Record{
		APIKey:             "test-key",
		Model:              "gpt-5.4",
		RequestedAt:        time.Date(2026, 4, 18, 12, 0, 0, 0, time.UTC),
		ClientAPIKeyID:     "k:abc123",
		ClientAPIKeyMasked: "sk******xy",
		SessionIndex:       "conv:session-1",
		Detail: coreusage.Detail{
			InputTokens:  10,
			OutputTokens: 20,
			TotalTokens:  30,
		},
	})

	snapshot := stats.Snapshot()
	details := snapshot.APIs["test-key"].Models["gpt-5.4"].Details
	if len(details) != 1 {
		t.Fatalf("details len = %d, want 1", len(details))
	}
	if details[0].ClientAPIKeyID != "k:abc123" {
		t.Fatalf("ClientAPIKeyID = %q, want k:abc123", details[0].ClientAPIKeyID)
	}
	if details[0].ClientAPIKeyMasked != "sk******xy" {
		t.Fatalf("ClientAPIKeyMasked = %q, want sk******xy", details[0].ClientAPIKeyMasked)
	}
	if details[0].SessionIndex != "conv:session-1" {
		t.Fatalf("SessionIndex = %q, want conv:session-1", details[0].SessionIndex)
	}
}

func TestRequestStatisticsMergeSnapshotDedupSeparatesClientAPIKeyAndSessionIndex(t *testing.T) {
	t.Parallel()

	stats := NewRequestStatistics()
	timestamp := time.Date(2026, 4, 18, 12, 0, 0, 0, time.UTC)
	snapshot := StatisticsSnapshot{
		APIs: map[string]APISnapshot{
			"test-key": {
				Models: map[string]ModelSnapshot{
					"gpt-5.4": {
						Details: []RequestDetail{
							{
								Timestamp:          timestamp,
								Source:             "user@example.com",
								AuthIndex:          "0",
								ClientAPIKeyID:     "k:first",
								ClientAPIKeyMasked: "sk******01",
								SessionIndex:       "conv:a",
								Tokens: TokenStats{
									InputTokens:  10,
									OutputTokens: 20,
									TotalTokens:  30,
								},
							},
							{
								Timestamp:          timestamp,
								Source:             "user@example.com",
								AuthIndex:          "0",
								ClientAPIKeyID:     "k:second",
								ClientAPIKeyMasked: "sk******02",
								SessionIndex:       "conv:b",
								Tokens: TokenStats{
									InputTokens:  10,
									OutputTokens: 20,
									TotalTokens:  30,
								},
							},
						},
					},
				},
			},
		},
	}

	result := stats.MergeSnapshot(snapshot)
	if result.Added != 2 || result.Skipped != 0 {
		t.Fatalf("merge result = %+v, want added=2 skipped=0", result)
	}
}
