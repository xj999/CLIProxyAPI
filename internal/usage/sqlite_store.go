package usage

import (
	"context"
	"database/sql"
	"fmt"
	"path/filepath"
	"strings"
	"time"

	coreusage "github.com/router-for-me/CLIProxyAPI/v6/sdk/cliproxy/usage"
	_ "modernc.org/sqlite"
)

type SQLiteStoreOptions struct {
	RecentDetailRetention time.Duration
	Now                   func() time.Time
}

type UsageQuery struct {
	Range string
	Now   time.Time
}

type SQLiteStore struct {
	db                    *sql.DB
	recentDetailRetention time.Duration
	now                   func() time.Time
}

var defaultSQLiteStore *SQLiteStore

func NewSQLiteStore(path string, opts SQLiteStoreOptions) (*SQLiteStore, error) {
	retention := opts.RecentDetailRetention
	if retention <= 0 {
		retention = 7 * 24 * time.Hour
	}
	nowFn := opts.Now
	if nowFn == nil {
		nowFn = time.Now
	}
	dsn := filepath.Clean(path)
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, fmt.Errorf("open usage sqlite db: %w", err)
	}
	store := &SQLiteStore{
		db:                    db,
		recentDetailRetention: retention,
		now:                   nowFn,
	}
	if err := store.init(); err != nil {
		_ = db.Close()
		return nil, err
	}
	return store, nil
}

func (s *SQLiteStore) Close() error {
	if s == nil || s.db == nil {
		return nil
	}
	return s.db.Close()
}

func SetDefaultSQLiteStore(store *SQLiteStore) {
	defaultSQLiteStore = store
}

func DefaultSQLiteStore() *SQLiteStore {
	return defaultSQLiteStore
}

func (s *SQLiteStore) HasData(ctx context.Context) (bool, error) {
	if s == nil || s.db == nil {
		return false, nil
	}
	if ctx == nil {
		ctx = context.Background()
	}

	var exists int
	if err := s.db.QueryRowContext(ctx, `
		SELECT EXISTS(SELECT 1 FROM usage_daily LIMIT 1)
	`).Scan(&exists); err != nil {
		return false, fmt.Errorf("query usage sqlite has data: %w", err)
	}
	return exists == 1, nil
}

func (s *SQLiteStore) Record(ctx context.Context, record coreusage.Record) error {
	if s == nil || s.db == nil {
		return nil
	}
	timestamp := record.RequestedAt
	if timestamp.IsZero() {
		timestamp = s.now()
	}
	timestamp = timestamp.UTC()
	detail := RequestDetail{
		Timestamp:          timestamp,
		LatencyMs:          normaliseLatency(record.Latency),
		Source:             strings.TrimSpace(record.Source),
		AuthIndex:          strings.TrimSpace(record.AuthIndex),
		ClientAPIKeyID:     strings.TrimSpace(record.ClientAPIKeyID),
		ClientAPIKeyMasked: strings.TrimSpace(record.ClientAPIKeyMasked),
		SessionIndex:       strings.TrimSpace(record.SessionIndex),
		Tokens:             normaliseDetail(record.Detail),
		Failed:             record.Failed,
	}
	apiKey := strings.TrimSpace(record.APIKey)
	if apiKey == "" {
		apiKey = resolveAPIIdentifier(ctx, record)
	}
	model := strings.TrimSpace(record.Model)
	if model == "" {
		model = "unknown"
	}
	return s.insertDetail(ctx, apiKey, model, detail)
}

func (s *SQLiteStore) ImportSnapshot(snapshot StatisticsSnapshot) (MergeResult, error) {
	result := MergeResult{}
	if s == nil || s.db == nil {
		return result, nil
	}
	for apiName, apiSnapshot := range snapshot.APIs {
		apiName = strings.TrimSpace(apiName)
		if apiName == "" {
			continue
		}
		for modelName, modelSnapshot := range apiSnapshot.Models {
			modelName = strings.TrimSpace(modelName)
			if modelName == "" {
				modelName = "unknown"
			}
			for _, detail := range modelSnapshot.Details {
				detail.Source = strings.TrimSpace(detail.Source)
				detail.AuthIndex = strings.TrimSpace(detail.AuthIndex)
				detail.ClientAPIKeyID = strings.TrimSpace(detail.ClientAPIKeyID)
				detail.ClientAPIKeyMasked = strings.TrimSpace(detail.ClientAPIKeyMasked)
				detail.SessionIndex = strings.TrimSpace(detail.SessionIndex)
				detail.Tokens = normaliseTokenStats(detail.Tokens)
				if detail.Timestamp.IsZero() {
					detail.Timestamp = s.now().UTC()
				} else {
					detail.Timestamp = detail.Timestamp.UTC()
				}
				if err := s.insertDetail(context.Background(), apiName, modelName, detail); err != nil {
					return result, err
				}
				result.Added++
			}
		}
	}
	return result, nil
}

func (s *SQLiteStore) QuerySnapshot(query UsageQuery) (StatisticsSnapshot, error) {
	snapshot := StatisticsSnapshot{
		APIs:           make(map[string]APISnapshot),
		RequestsByDay:  make(map[string]int64),
		RequestsByHour: make(map[string]int64),
		TokensByDay:    make(map[string]int64),
		TokensByHour:   make(map[string]int64),
	}
	if s == nil || s.db == nil {
		return snapshot, nil
	}
	now := query.Now
	if now.IsZero() {
		now = s.now()
	}
	now = now.UTC()
	rangeKey := strings.TrimSpace(query.Range)
	if rangeKey == "" {
		rangeKey = "24h"
	}

	if rangeKey == "all" {
		if err := s.fillDailySummary(&snapshot); err != nil {
			return StatisticsSnapshot{}, err
		}
		detailStart := now.Add(-s.recentDetailRetention)
		if err := s.fillRecentDetails(&snapshot, detailStart.UnixMilli(), now.UnixMilli()); err != nil {
			return StatisticsSnapshot{}, err
		}
		if err := s.fillDailyTimeseries(&snapshot); err != nil {
			return StatisticsSnapshot{}, err
		}
		if err := s.fillRecentHourTimeseries(&snapshot, detailStart.UnixMilli(), now.UnixMilli()); err != nil {
			return StatisticsSnapshot{}, err
		}
		return snapshot, nil
	}

	start, err := resolveRangeStart(rangeKey, now)
	if err != nil {
		return StatisticsSnapshot{}, err
	}
	startMs := start.UnixMilli()
	endMs := now.UnixMilli()
	if err := s.fillRecentSummary(&snapshot, startMs, endMs); err != nil {
		return StatisticsSnapshot{}, err
	}
	if err := s.fillRecentDetails(&snapshot, startMs, endMs); err != nil {
		return StatisticsSnapshot{}, err
	}
	if err := s.fillRecentTimeseries(&snapshot, startMs, endMs); err != nil {
		return StatisticsSnapshot{}, err
	}
	return snapshot, nil
}

func (s *SQLiteStore) init() error {
	stmts := []string{
		`PRAGMA journal_mode=WAL;`,
		`CREATE TABLE IF NOT EXISTS usage_daily (
			usage_date TEXT NOT NULL,
			api_key TEXT NOT NULL,
			model TEXT NOT NULL,
			source TEXT NOT NULL,
			auth_index TEXT NOT NULL,
			client_api_key_id TEXT NOT NULL,
			client_api_key_masked TEXT NOT NULL,
			session_index TEXT NOT NULL,
			failed INTEGER NOT NULL,
			requests INTEGER NOT NULL,
			input_tokens INTEGER NOT NULL,
			output_tokens INTEGER NOT NULL,
			reasoning_tokens INTEGER NOT NULL,
			cached_tokens INTEGER NOT NULL,
			total_tokens INTEGER NOT NULL,
			latency_ms_sum INTEGER NOT NULL,
			PRIMARY KEY (
				usage_date, api_key, model, source, auth_index,
				client_api_key_id, client_api_key_masked, session_index, failed
			)
		);`,
		`CREATE TABLE IF NOT EXISTS usage_recent_requests (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			requested_at_ms INTEGER NOT NULL,
			requested_at TEXT NOT NULL,
			usage_date TEXT NOT NULL,
			usage_hour TEXT NOT NULL,
			api_key TEXT NOT NULL,
			model TEXT NOT NULL,
			source TEXT NOT NULL,
			auth_index TEXT NOT NULL,
			client_api_key_id TEXT NOT NULL,
			client_api_key_masked TEXT NOT NULL,
			session_index TEXT NOT NULL,
			failed INTEGER NOT NULL,
			latency_ms INTEGER NOT NULL,
			input_tokens INTEGER NOT NULL,
			output_tokens INTEGER NOT NULL,
			reasoning_tokens INTEGER NOT NULL,
			cached_tokens INTEGER NOT NULL,
			total_tokens INTEGER NOT NULL
		);`,
		`CREATE INDEX IF NOT EXISTS idx_usage_recent_requested_at_ms
			ON usage_recent_requests (requested_at_ms);`,
		`CREATE INDEX IF NOT EXISTS idx_usage_recent_api_model
			ON usage_recent_requests (api_key, model, requested_at_ms);`,
	}
	for _, stmt := range stmts {
		if _, err := s.db.Exec(stmt); err != nil {
			return fmt.Errorf("init usage sqlite schema: %w", err)
		}
	}
	return nil
}

func (s *SQLiteStore) insertDetail(ctx context.Context, apiKey, model string, detail RequestDetail) error {
	if ctx == nil {
		ctx = context.Background()
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin usage sqlite tx: %w", err)
	}
	defer func() {
		if err != nil {
			_ = tx.Rollback()
		}
	}()

	dateKey := detail.Timestamp.Format("2006-01-02")
	hourKey := detail.Timestamp.Format("15")
	failedInt := 0
	if detail.Failed {
		failedInt = 1
	}

	if _, err = tx.ExecContext(ctx, `
		INSERT INTO usage_daily (
			usage_date, api_key, model, source, auth_index, client_api_key_id,
			client_api_key_masked, session_index, failed, requests, input_tokens,
			output_tokens, reasoning_tokens, cached_tokens, total_tokens, latency_ms_sum
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, 1, ?, ?, ?, ?, ?, ?)
		ON CONFLICT (
			usage_date, api_key, model, source, auth_index,
			client_api_key_id, client_api_key_masked, session_index, failed
		) DO UPDATE SET
			requests = requests + 1,
			input_tokens = input_tokens + excluded.input_tokens,
			output_tokens = output_tokens + excluded.output_tokens,
			reasoning_tokens = reasoning_tokens + excluded.reasoning_tokens,
			cached_tokens = cached_tokens + excluded.cached_tokens,
			total_tokens = total_tokens + excluded.total_tokens,
			latency_ms_sum = latency_ms_sum + excluded.latency_ms_sum;
	`,
		dateKey,
		apiKey,
		model,
		detail.Source,
		detail.AuthIndex,
		detail.ClientAPIKeyID,
		detail.ClientAPIKeyMasked,
		detail.SessionIndex,
		failedInt,
		detail.Tokens.InputTokens,
		detail.Tokens.OutputTokens,
		detail.Tokens.ReasoningTokens,
		detail.Tokens.CachedTokens,
		detail.Tokens.TotalTokens,
		detail.LatencyMs,
	); err != nil {
		return fmt.Errorf("upsert usage daily row: %w", err)
	}

	retentionCutoff := s.now().UTC().Add(-s.recentDetailRetention).UnixMilli()
	if detail.Timestamp.UnixMilli() >= retentionCutoff {
		if _, err = tx.ExecContext(ctx, `
			INSERT INTO usage_recent_requests (
				requested_at_ms, requested_at, usage_date, usage_hour, api_key, model,
				source, auth_index, client_api_key_id, client_api_key_masked, session_index,
				failed, latency_ms, input_tokens, output_tokens, reasoning_tokens, cached_tokens,
				total_tokens
			) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		`,
			detail.Timestamp.UnixMilli(),
			detail.Timestamp.Format(time.RFC3339Nano),
			dateKey,
			hourKey,
			apiKey,
			model,
			detail.Source,
			detail.AuthIndex,
			detail.ClientAPIKeyID,
			detail.ClientAPIKeyMasked,
			detail.SessionIndex,
			failedInt,
			detail.LatencyMs,
			detail.Tokens.InputTokens,
			detail.Tokens.OutputTokens,
			detail.Tokens.ReasoningTokens,
			detail.Tokens.CachedTokens,
			detail.Tokens.TotalTokens,
		); err != nil {
			return fmt.Errorf("insert usage recent request: %w", err)
		}
	}

	if _, err = tx.ExecContext(ctx, `
		DELETE FROM usage_recent_requests WHERE requested_at_ms < ?
	`, retentionCutoff); err != nil {
		return fmt.Errorf("prune usage recent requests: %w", err)
	}

	if err = tx.Commit(); err != nil {
		return fmt.Errorf("commit usage sqlite tx: %w", err)
	}
	return nil
}

func (s *SQLiteStore) fillDailySummary(snapshot *StatisticsSnapshot) error {
	rows, err := s.db.Query(`
		SELECT
			api_key,
			model,
			SUM(requests),
			SUM(CASE WHEN failed = 0 THEN requests ELSE 0 END),
			SUM(CASE WHEN failed = 1 THEN requests ELSE 0 END),
			SUM(total_tokens)
		FROM usage_daily
		GROUP BY api_key, model
	`)
	if err != nil {
		return fmt.Errorf("query usage daily summary: %w", err)
	}
	defer rows.Close()

	for rows.Next() {
		var apiKey, model string
		var totalRequests, successCount, failureCount, totalTokens int64
		if err := rows.Scan(&apiKey, &model, &totalRequests, &successCount, &failureCount, &totalTokens); err != nil {
			return fmt.Errorf("scan usage daily summary: %w", err)
		}
		addSnapshotSummary(snapshot, apiKey, model, totalRequests, successCount, failureCount, totalTokens)
	}
	return rows.Err()
}

func (s *SQLiteStore) fillRecentSummary(snapshot *StatisticsSnapshot, startMs, endMs int64) error {
	rows, err := s.db.Query(`
		SELECT
			api_key,
			model,
			COUNT(*),
			SUM(CASE WHEN failed = 0 THEN 1 ELSE 0 END),
			SUM(CASE WHEN failed = 1 THEN 1 ELSE 0 END),
			SUM(total_tokens)
		FROM usage_recent_requests
		WHERE requested_at_ms >= ? AND requested_at_ms <= ?
		GROUP BY api_key, model
	`, startMs, endMs)
	if err != nil {
		return fmt.Errorf("query usage recent summary: %w", err)
	}
	defer rows.Close()

	for rows.Next() {
		var apiKey, model string
		var totalRequests, successCount, failureCount, totalTokens int64
		if err := rows.Scan(&apiKey, &model, &totalRequests, &successCount, &failureCount, &totalTokens); err != nil {
			return fmt.Errorf("scan usage recent summary: %w", err)
		}
		addSnapshotSummary(snapshot, apiKey, model, totalRequests, successCount, failureCount, totalTokens)
	}
	return rows.Err()
}

func (s *SQLiteStore) fillRecentDetails(snapshot *StatisticsSnapshot, startMs, endMs int64) error {
	rows, err := s.db.Query(`
		SELECT
			api_key,
			model,
			requested_at,
			latency_ms,
			source,
			auth_index,
			client_api_key_id,
			client_api_key_masked,
			session_index,
			failed,
			input_tokens,
			output_tokens,
			reasoning_tokens,
			cached_tokens,
			total_tokens
		FROM usage_recent_requests
		WHERE requested_at_ms >= ? AND requested_at_ms <= ?
		ORDER BY requested_at_ms DESC
	`, startMs, endMs)
	if err != nil {
		return fmt.Errorf("query usage recent details: %w", err)
	}
	defer rows.Close()

	for rows.Next() {
		var apiKey, model, requestedAt, source, authIndex, clientAPIKeyID, clientAPIKeyMasked, sessionIndex string
		var latencyMs, failedInt, inputTokens, outputTokens, reasoningTokens, cachedTokens, totalTokens int64
		if err := rows.Scan(
			&apiKey,
			&model,
			&requestedAt,
			&latencyMs,
			&source,
			&authIndex,
			&clientAPIKeyID,
			&clientAPIKeyMasked,
			&sessionIndex,
			&failedInt,
			&inputTokens,
			&outputTokens,
			&reasoningTokens,
			&cachedTokens,
			&totalTokens,
		); err != nil {
			return fmt.Errorf("scan usage recent detail: %w", err)
		}
		timestamp, err := time.Parse(time.RFC3339Nano, requestedAt)
		if err != nil {
			return fmt.Errorf("parse usage recent timestamp: %w", err)
		}
		apiSnapshot := snapshot.APIs[apiKey]
		if apiSnapshot.Models == nil {
			apiSnapshot.Models = make(map[string]ModelSnapshot)
		}
		modelSnapshot := apiSnapshot.Models[model]
		modelSnapshot.Details = append(modelSnapshot.Details, RequestDetail{
			Timestamp:          timestamp,
			LatencyMs:          latencyMs,
			Source:             source,
			AuthIndex:          authIndex,
			ClientAPIKeyID:     clientAPIKeyID,
			ClientAPIKeyMasked: clientAPIKeyMasked,
			SessionIndex:       sessionIndex,
			Failed:             failedInt == 1,
			Tokens: TokenStats{
				InputTokens:     inputTokens,
				OutputTokens:    outputTokens,
				ReasoningTokens: reasoningTokens,
				CachedTokens:    cachedTokens,
				TotalTokens:     totalTokens,
			},
		})
		apiSnapshot.Models[model] = modelSnapshot
		snapshot.APIs[apiKey] = apiSnapshot
	}
	return rows.Err()
}

func (s *SQLiteStore) fillDailyTimeseries(snapshot *StatisticsSnapshot) error {
	rows, err := s.db.Query(`
		SELECT usage_date, SUM(requests), SUM(total_tokens)
		FROM usage_daily
		GROUP BY usage_date
		ORDER BY usage_date ASC
	`)
	if err != nil {
		return fmt.Errorf("query usage daily timeseries: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		var day string
		var requests, tokens int64
		if err := rows.Scan(&day, &requests, &tokens); err != nil {
			return fmt.Errorf("scan usage daily timeseries: %w", err)
		}
		snapshot.RequestsByDay[day] = requests
		snapshot.TokensByDay[day] = tokens
	}
	return rows.Err()
}

func (s *SQLiteStore) fillRecentHourTimeseries(snapshot *StatisticsSnapshot, startMs, endMs int64) error {
	rows, err := s.db.Query(`
		SELECT usage_hour, COUNT(*), SUM(total_tokens)
		FROM usage_recent_requests
		WHERE requested_at_ms >= ? AND requested_at_ms <= ?
		GROUP BY usage_hour
	`, startMs, endMs)
	if err != nil {
		return fmt.Errorf("query usage recent hour timeseries: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		var hour string
		var requests, tokens int64
		if err := rows.Scan(&hour, &requests, &tokens); err != nil {
			return fmt.Errorf("scan usage recent hour timeseries: %w", err)
		}
		snapshot.RequestsByHour[hour] = requests
		snapshot.TokensByHour[hour] = tokens
	}
	return rows.Err()
}

func (s *SQLiteStore) fillRecentTimeseries(snapshot *StatisticsSnapshot, startMs, endMs int64) error {
	dayRows, err := s.db.Query(`
		SELECT usage_date, COUNT(*), SUM(total_tokens)
		FROM usage_recent_requests
		WHERE requested_at_ms >= ? AND requested_at_ms <= ?
		GROUP BY usage_date
		ORDER BY usage_date ASC
	`, startMs, endMs)
	if err != nil {
		return fmt.Errorf("query usage recent day timeseries: %w", err)
	}
	defer dayRows.Close()
	for dayRows.Next() {
		var day string
		var requests, tokens int64
		if err := dayRows.Scan(&day, &requests, &tokens); err != nil {
			return fmt.Errorf("scan usage recent day timeseries: %w", err)
		}
		snapshot.RequestsByDay[day] = requests
		snapshot.TokensByDay[day] = tokens
	}
	if err := dayRows.Err(); err != nil {
		return err
	}

	return s.fillRecentHourTimeseries(snapshot, startMs, endMs)
}

func resolveRangeStart(rangeKey string, now time.Time) (time.Time, error) {
	switch rangeKey {
	case "today":
		start := now
		start = time.Date(start.Year(), start.Month(), start.Day(), 0, 0, 0, 0, start.Location())
		return start, nil
	case "7h":
		return now.Add(-7 * time.Hour), nil
	case "24h":
		return now.Add(-24 * time.Hour), nil
	case "7d":
		return now.Add(-7 * 24 * time.Hour), nil
	default:
		return time.Time{}, fmt.Errorf("unsupported usage range %q", rangeKey)
	}
}

func addSnapshotSummary(snapshot *StatisticsSnapshot, apiKey, model string, totalRequests, successCount, failureCount, totalTokens int64) {
	apiSnapshot := snapshot.APIs[apiKey]
	if apiSnapshot.Models == nil {
		apiSnapshot.Models = make(map[string]ModelSnapshot)
	}
	modelSnapshot := apiSnapshot.Models[model]

	modelSnapshot.TotalRequests += totalRequests
	modelSnapshot.SuccessCount += successCount
	modelSnapshot.FailureCount += failureCount
	modelSnapshot.TotalTokens += totalTokens

	apiSnapshot.TotalRequests += totalRequests
	apiSnapshot.SuccessCount += successCount
	apiSnapshot.FailureCount += failureCount
	apiSnapshot.TotalTokens += totalTokens
	apiSnapshot.Models[model] = modelSnapshot
	snapshot.APIs[apiKey] = apiSnapshot

	snapshot.TotalRequests += totalRequests
	snapshot.SuccessCount += successCount
	snapshot.FailureCount += failureCount
	snapshot.TotalTokens += totalTokens
}
