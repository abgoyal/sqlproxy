package scheduler

import (
	"context"
	"database/sql"
	"encoding/json"
	"regexp"
	"strings"
	"time"

	"github.com/robfig/cron/v3"

	"sql-proxy/internal/config"
	"sql-proxy/internal/db"
	"sql-proxy/internal/logging"
)

const (
	maxRetries     = 3
	maxSampleRows  = 10
	queryTimeout   = 60 * time.Second
)

// Scheduler manages scheduled query execution
type Scheduler struct {
	cron      *cron.Cron
	db        *db.DB
	serverCfg config.ServerConfig
}

// New creates a new scheduler for the given queries
func New(database *db.DB, queries []config.QueryConfig, serverCfg config.ServerConfig) *Scheduler {
	s := &Scheduler{
		cron:      cron.New(),
		db:        database,
		serverCfg: serverCfg,
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
			"cron":       q.Schedule.Cron,
			"error":      err.Error(),
		})
		return
	}

	logging.Info("scheduler_job_added", map[string]any{
		"query_name": q.Name,
		"cron":       q.Schedule.Cron,
	})
}

func (s *Scheduler) executeJob(q config.QueryConfig) {
	logging.Info("scheduled_query_started", map[string]any{
		"query_name": q.Name,
		"cron":       q.Schedule.Cron,
	})

	startTime := time.Now()
	var lastErr error
	var results []map[string]any

	// Retry with exponential backoff: 1s, 5s, 25s
	backoffs := []time.Duration{0, 1 * time.Second, 5 * time.Second, 25 * time.Second}

	for attempt := 1; attempt <= maxRetries; attempt++ {
		if attempt > 1 {
			time.Sleep(backoffs[attempt-1])
			logging.Debug("scheduled_query_retry", map[string]any{
				"query_name": q.Name,
				"attempt":    attempt,
			})
		}

		results, lastErr = s.runQuery(q)
		if lastErr == nil {
			break
		}

		logging.Warn("scheduled_query_attempt_failed", map[string]any{
			"query_name": q.Name,
			"attempt":    attempt,
			"error":      lastErr.Error(),
		})
	}

	duration := time.Since(startTime)

	if lastErr != nil {
		logging.Error("scheduled_query_failed", map[string]any{
			"query_name":  q.Name,
			"error":       lastErr.Error(),
			"attempts":    maxRetries,
			"duration_ms": duration.Milliseconds(),
		})
		return
	}

	// Success - log results
	logFields := map[string]any{
		"query_name":  q.Name,
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
}

func (s *Scheduler) runQuery(q config.QueryConfig) ([]map[string]any, error) {
	// Resolve timeout
	timeout := queryTimeout
	if q.TimeoutSec > 0 {
		timeout = time.Duration(q.TimeoutSec) * time.Second
	} else if s.serverCfg.DefaultTimeoutSec > 0 {
		timeout = time.Duration(s.serverCfg.DefaultTimeoutSec) * time.Second
	}

	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	// Build query args from schedule params
	args, err := s.buildArgs(q)
	if err != nil {
		return nil, err
	}

	return s.db.Query(ctx, q.SQL, args...)
}

func (s *Scheduler) buildArgs(q config.QueryConfig) ([]any, error) {
	// Find @params in SQL
	re := regexp.MustCompile(`@(\w+)`)
	matches := re.FindAllStringSubmatch(q.SQL, -1)

	addedParams := make(map[string]bool)
	var args []any

	for _, match := range matches {
		paramName := match[1]
		if addedParams[paramName] {
			continue
		}

		// Get value from schedule params or parameter default
		var value any
		if strVal, ok := q.Schedule.Params[paramName]; ok {
			value = s.resolveValue(strVal, paramName, q.Parameters)
		} else {
			// Check for default in parameter config
			for _, p := range q.Parameters {
				if p.Name == paramName && p.Default != "" {
					value = s.resolveValue(p.Default, paramName, q.Parameters)
					break
				}
			}
		}

		args = append(args, sql.Named(paramName, value))
		addedParams[paramName] = true
	}

	return args, nil
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
		var i int
		if _, err := time.Parse("2006-01-02", strVal); err != nil {
			// Try parsing as int
			if n, err := parseInt(strVal); err == nil {
				return n
			}
		}
		return i
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

func parseInt(s string) (int, error) {
	var n int
	for _, c := range s {
		if c < '0' || c > '9' {
			return 0, nil
		}
		n = n*10 + int(c-'0')
	}
	return n, nil
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
