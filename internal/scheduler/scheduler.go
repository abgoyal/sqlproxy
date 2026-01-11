package scheduler

import (
	"context"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/robfig/cron/v3"

	"sql-proxy/internal/config"
	"sql-proxy/internal/db"
	"sql-proxy/internal/logging"
	"sql-proxy/internal/webhook"
)

const (
	maxRetries     = 3
	maxSampleRows  = 10
	queryTimeout   = 60 * time.Second
)

// Scheduler manages scheduled query execution
type Scheduler struct {
	cron      *cron.Cron
	dbManager *db.Manager
	serverCfg config.ServerConfig
	stopCh    chan struct{} // Signal channel for graceful shutdown
}

// New creates a new scheduler for the given queries
func New(dbManager *db.Manager, queries []config.QueryConfig, serverCfg config.ServerConfig) *Scheduler {
	s := &Scheduler{
		cron:      cron.New(),
		dbManager: dbManager,
		serverCfg: serverCfg,
		stopCh:    make(chan struct{}),
	}

	for _, q := range queries {
		if q.Schedule != nil {
			s.addJob(q)
		}
	}

	return s
}

// Start begins executing scheduled jobs
func (s *Scheduler) Start() {
	s.cron.Start()
	logging.Info("scheduler_started", map[string]any{
		"jobs": len(s.cron.Entries()),
	})
}

// Stop gracefully stops the scheduler
func (s *Scheduler) Stop() {
	// Signal any running jobs to stop
	close(s.stopCh)
	// Wait for cron to stop
	ctx := s.cron.Stop()
	<-ctx.Done()
	logging.Info("scheduler_stopped", nil)
}

func (s *Scheduler) addJob(q config.QueryConfig) {
	_, err := s.cron.AddFunc(q.Schedule.Cron, func() {
		s.executeJob(q)
	})
	if err != nil {
		logging.Error("scheduler_add_job_failed", map[string]any{
			"query_name": q.Name,
			"database":   q.Database,
			"cron":       q.Schedule.Cron,
			"error":      err.Error(),
		})
		return
	}

	logging.Info("scheduler_job_added", map[string]any{
		"query_name": q.Name,
		"database":   q.Database,
		"cron":       q.Schedule.Cron,
	})
}

func (s *Scheduler) executeJob(q config.QueryConfig) {
	logging.Info("scheduled_query_started", map[string]any{
		"query_name": q.Name,
		"database":   q.Database,
		"cron":       q.Schedule.Cron,
	})

	startTime := time.Now()
	var lastErr error
	var results []map[string]any

	// Retry with exponential backoff: 1s, 5s, 25s
	backoffs := []time.Duration{0, 1 * time.Second, 5 * time.Second, 25 * time.Second}

	for attempt := 1; attempt <= maxRetries; attempt++ {
		if attempt > 1 {
			// Interruptible backoff - respect shutdown signal
			select {
			case <-s.stopCh:
				logging.Info("scheduled_query_cancelled", map[string]any{
					"query_name": q.Name,
					"database":   q.Database,
					"attempt":    attempt,
					"reason":     "scheduler stopping",
				})
				return
			case <-time.After(backoffs[attempt-1]):
				// Backoff completed, continue with retry
			}
			logging.Debug("scheduled_query_retry", map[string]any{
				"query_name": q.Name,
				"database":   q.Database,
				"attempt":    attempt,
			})
		}

		results, lastErr = s.runQuery(q)
		if lastErr == nil {
			break
		}

		logging.Warn("scheduled_query_attempt_failed", map[string]any{
			"query_name": q.Name,
			"database":   q.Database,
			"attempt":    attempt,
			"error":      lastErr.Error(),
		})
	}

	duration := time.Since(startTime)

	if lastErr != nil {
		logging.Error("scheduled_query_failed", map[string]any{
			"query_name":  q.Name,
			"database":    q.Database,
			"error":       lastErr.Error(),
			"attempts":    maxRetries,
			"duration_ms": duration.Milliseconds(),
		})

		// Send failure webhook if configured
		if q.Schedule.Webhook != nil {
			s.sendWebhook(q, nil, lastErr, duration)
		}
		return
	}

	// Success - log results
	logFields := map[string]any{
		"query_name":  q.Name,
		"database":    q.Database,
		"row_count":   len(results),
		"duration_ms": duration.Milliseconds(),
	}

	if q.Schedule.LogResults && len(results) > 0 {
		// Include sample rows (up to maxSampleRows)
		sampleRows := results
		if len(sampleRows) > maxSampleRows {
			sampleRows = sampleRows[:maxSampleRows]
		}
		// Convert to JSON for logging
		if sample, err := json.Marshal(sampleRows); err == nil {
			logFields["sample_rows"] = string(sample)
		}
	}

	logging.Info("scheduled_query_completed", logFields)

	// Send success webhook if configured
	if q.Schedule.Webhook != nil {
		s.sendWebhook(q, results, nil, duration)
	}
}

func (s *Scheduler) sendWebhook(q config.QueryConfig, results []map[string]any, queryErr error, duration time.Duration) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	// Build execution context
	execCtx := &webhook.ExecutionContext{
		Query:      q.Name,
		Count:      len(results),
		Success:    queryErr == nil,
		DurationMs: duration.Milliseconds(),
		Params:     q.Schedule.Params,
		Data:       results,
		Version:    s.serverCfg.Version,
		BuildTime:  s.serverCfg.BuildTime,
	}
	if queryErr != nil {
		execCtx.Error = queryErr.Error()
	}

	err := webhook.Execute(ctx, q.Schedule.Webhook, execCtx)
	if err != nil {
		logging.Error("webhook_failed", map[string]any{
			"query_name": q.Name,
			"url":        q.Schedule.Webhook.URL,
			"error":      err.Error(),
		})
	} else {
		logging.Debug("webhook_sent", map[string]any{
			"query_name": q.Name,
			"url":        q.Schedule.Webhook.URL,
			"success":    execCtx.Success,
			"count":      execCtx.Count,
		})
	}
}

func (s *Scheduler) runQuery(q config.QueryConfig) ([]map[string]any, error) {
	// Get database connection
	database, err := s.dbManager.Get(q.Database)
	if err != nil {
		return nil, fmt.Errorf("failed to get database connection %s: %w", q.Database, err)
	}

	// Resolve session config (query overrides > connection defaults > implicit defaults)
	sessionCfg := config.ResolveSessionConfig(database.Config(), q)

	// Resolve timeout
	timeout := queryTimeout
	if q.TimeoutSec > 0 {
		timeout = time.Duration(q.TimeoutSec) * time.Second
	} else if s.serverCfg.DefaultTimeoutSec > 0 {
		timeout = time.Duration(s.serverCfg.DefaultTimeoutSec) * time.Second
	}

	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	// Build query params from schedule params
	params := s.buildParams(q)

	return database.Query(ctx, sessionCfg, q.SQL, params)
}

// buildParams builds the parameter map for a scheduled query
func (s *Scheduler) buildParams(q config.QueryConfig) map[string]any {
	params := make(map[string]any)

	// Process each defined parameter
	for _, p := range q.Parameters {
		var value any

		// Get value from schedule params first
		if strVal, ok := q.Schedule.Params[p.Name]; ok {
			value = s.resolveValue(strVal, p.Name, q.Parameters)
		} else if p.Default != "" {
			// Fall back to default value
			value = s.resolveValue(p.Default, p.Name, q.Parameters)
		}
		// If still nil, the driver will handle it (pass NULL)

		params[p.Name] = value
	}

	return params
}

func (s *Scheduler) resolveValue(strVal, paramName string, params []config.ParamConfig) any {
	// Find parameter type
	var paramType string
	for _, p := range params {
		if p.Name == paramName {
			paramType = strings.ToLower(p.Type)
			break
		}
	}

	// Handle dynamic dates
	lower := strings.ToLower(strVal)
	now := time.Now()

	switch lower {
	case "now":
		return now
	case "today":
		return time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, now.Location())
	case "yesterday":
		yesterday := now.AddDate(0, 0, -1)
		return time.Date(yesterday.Year(), yesterday.Month(), yesterday.Day(), 0, 0, 0, 0, now.Location())
	case "tomorrow":
		tomorrow := now.AddDate(0, 0, 1)
		return time.Date(tomorrow.Year(), tomorrow.Month(), tomorrow.Day(), 0, 0, 0, 0, now.Location())
	}

	// Convert based on type
	switch paramType {
	case "int", "integer":
		if n, err := strconv.Atoi(strVal); err == nil {
			return n
		}
		return 0
	case "datetime", "date":
		// Try parsing various date formats
		formats := []string{
			time.RFC3339,
			"2006-01-02T15:04:05",
			"2006-01-02 15:04:05",
			"2006-01-02",
		}
		for _, f := range formats {
			if t, err := time.Parse(f, strVal); err == nil {
				return t
			}
		}
		return strVal
	case "bool", "boolean":
		return strings.ToLower(strVal) == "true" || strVal == "1"
	default:
		return strVal
	}
}


// HasScheduledQueries returns true if any queries have schedules
func HasScheduledQueries(queries []config.QueryConfig) bool {
	for _, q := range queries {
		if q.Schedule != nil {
			return true
		}
	}
	return false
}
