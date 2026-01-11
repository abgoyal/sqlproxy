package webhook

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"text/template"
	"time"

	"sql-proxy/internal/config"
)

const (
	// defaultMaxRetries is the number of retry attempts for failed webhooks
	defaultMaxRetries = 3

	// defaultInitialBackoff is the initial delay between retries
	defaultInitialBackoff = 1 * time.Second

	// defaultMaxBackoff is the maximum delay between retries
	defaultMaxBackoff = 30 * time.Second

	// backoffMultiplier is the factor by which backoff increases after each retry
	backoffMultiplier = 2
)

// httpClient is a shared client for all webhook requests.
// Reusing the client allows connection pooling and TLS session reuse.
var httpClient = &http.Client{
	Timeout: 30 * time.Second,
	Transport: &http.Transport{
		MaxIdleConns:        10,
		MaxIdleConnsPerHost: 5,
		IdleConnTimeout:     90 * time.Second,
	},
}

// ExecutionContext holds data available to templates
type ExecutionContext struct {
	Query      string            `json:"query"`       // Query name
	Count      int               `json:"count"`       // Row count
	Success    bool              `json:"success"`     // Whether query succeeded
	DurationMs int64             `json:"duration_ms"` // Query execution time
	Params     map[string]string `json:"params"`      // Query parameters
	Data       []map[string]any  `json:"data"`        // Query results
	Error      string            `json:"error"`       // Error message if failed
	Version    string            `json:"version"`     // Service version (injectable into templates)
	BuildTime  string            `json:"build_time"`  // Build timestamp (injectable into templates)
}

// TemplateFuncMap provides custom template functions for webhook templates.
// Exported so validate package can use the same functions for validation.
var TemplateFuncMap = template.FuncMap{
	"add": func(a, b int) int { return a + b },
	"mod": func(a, b int) int { return a % b },
	"json": func(v any) string {
		b, err := json.Marshal(v)
		if err != nil {
			return fmt.Sprintf("json error: %v", err)
		}
		return string(b)
	},
	"jsonIndent": func(v any) string {
		b, err := json.MarshalIndent(v, "", "  ")
		if err != nil {
			return fmt.Sprintf("json error: %v", err)
		}
		return string(b)
	},
}

// Execute sends a webhook with the given context, retrying on transient failures.
func Execute(ctx context.Context, webhookCfg *config.WebhookConfig, execCtx *ExecutionContext) error {
	if webhookCfg == nil {
		return nil
	}

	// Check on_empty behavior
	if execCtx.Count == 0 && webhookCfg.Body != nil && webhookCfg.Body.OnEmpty == "skip" {
		return nil // Skip webhook for empty results
	}

	// Build URL (may contain templates)
	url, err := executeTemplate("url", webhookCfg.URL, execCtx)
	if err != nil {
		return fmt.Errorf("url template error: %w", err)
	}

	// Build body
	body, err := buildBody(webhookCfg.Body, execCtx)
	if err != nil {
		return fmt.Errorf("body template error: %w", err)
	}

	// Determine method
	method := webhookCfg.Method
	if method == "" {
		method = "POST"
	}

	// Build headers map with expanded env vars
	headers := make(map[string]string)
	headers["Content-Type"] = "application/json"
	for key, value := range webhookCfg.Headers {
		headers[key] = os.ExpandEnv(value)
	}

	// Resolve retry configuration
	retryCfg := resolveRetryConfig(webhookCfg.Retry)

	// Execute with retry (if enabled)
	return doWithRetry(ctx, method, url, body, headers, retryCfg)
}

// RetryConfig holds resolved retry settings
type RetryConfig struct {
	Enabled        bool
	MaxAttempts    int
	InitialBackoff time.Duration
	MaxBackoff     time.Duration
}

// resolveRetryConfig converts WebhookRetryConfig to RetryConfig with defaults
func resolveRetryConfig(cfg *config.WebhookRetryConfig) RetryConfig {
	// Default: enabled with standard retry values
	result := RetryConfig{
		Enabled:        true,
		MaxAttempts:    defaultMaxRetries,
		InitialBackoff: defaultInitialBackoff,
		MaxBackoff:     defaultMaxBackoff,
	}

	if cfg == nil {
		return result
	}

	// Check if explicitly disabled
	if cfg.Enabled != nil && !*cfg.Enabled {
		result.Enabled = false
		return result
	}

	// Apply configured values (> 0 means explicitly set)
	if cfg.MaxAttempts > 0 {
		result.MaxAttempts = cfg.MaxAttempts
	}

	if cfg.InitialBackoffSec > 0 {
		result.InitialBackoff = time.Duration(cfg.InitialBackoffSec) * time.Second
	}

	if cfg.MaxBackoffSec > 0 {
		result.MaxBackoff = time.Duration(cfg.MaxBackoffSec) * time.Second
	}

	return result
}

// doWithRetry executes an HTTP request with exponential backoff retry on transient failures.
func doWithRetry(ctx context.Context, method, url, body string, headers map[string]string, retryCfg RetryConfig) error {
	var lastErr error
	backoff := retryCfg.InitialBackoff

	// Calculate max attempts: 1 initial + N retries
	maxAttempts := 1
	if retryCfg.Enabled {
		maxAttempts = 1 + retryCfg.MaxAttempts
	}

	for attempt := 0; attempt < maxAttempts; attempt++ {
		if attempt > 0 {
			// Wait before retry, respecting context cancellation
			select {
			case <-ctx.Done():
				return fmt.Errorf("webhook cancelled after %d attempts: %w", attempt, ctx.Err())
			case <-time.After(backoff):
			}

			// Increase backoff for next attempt, capped at max
			backoff *= backoffMultiplier
			if backoff > retryCfg.MaxBackoff {
				backoff = retryCfg.MaxBackoff
			}
		}

		// Create fresh request for each attempt (body reader cannot be reused)
		req, err := http.NewRequestWithContext(ctx, method, url, strings.NewReader(body))
		if err != nil {
			return fmt.Errorf("creating request: %w", err)
		}

		for key, value := range headers {
			req.Header.Set(key, value)
		}

		resp, err := httpClient.Do(req)
		if err != nil {
			// Network error - retryable if retries enabled
			lastErr = fmt.Errorf("sending webhook: %w", err)
			if !retryCfg.Enabled {
				return lastErr
			}
			continue
		}

		// Read response body for error reporting, then drain for connection reuse
		respBody, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		io.Copy(io.Discard, resp.Body)
		resp.Body.Close()

		// Success
		if resp.StatusCode < 400 {
			return nil
		}

		// 4xx client errors are not retryable
		if resp.StatusCode >= 400 && resp.StatusCode < 500 {
			return fmt.Errorf("webhook returned %d: %s", resp.StatusCode, string(respBody))
		}

		// 5xx server errors are retryable if retries enabled
		lastErr = fmt.Errorf("webhook returned %d: %s", resp.StatusCode, string(respBody))
		if !retryCfg.Enabled {
			return lastErr
		}
	}

	return fmt.Errorf("webhook failed after %d attempts: %w", maxAttempts, lastErr)
}

// buildBody creates the webhook body based on configuration
func buildBody(bodyCfg *config.WebhookBodyConfig, execCtx *ExecutionContext) (string, error) {
	// If no body config, send raw JSON results
	if bodyCfg == nil {
		b, err := json.Marshal(execCtx)
		return string(b), err
	}

	// Handle empty results with alternate template
	if execCtx.Count == 0 && bodyCfg.Empty != "" {
		return executeTemplate("empty", bodyCfg.Empty, execCtx)
	}

	// Build header/item/footer body
	var buf bytes.Buffer

	// Header
	if bodyCfg.Header != "" {
		header, err := executeTemplate("header", bodyCfg.Header, execCtx)
		if err != nil {
			return "", fmt.Errorf("header: %w", err)
		}
		buf.WriteString(header)
	}

	// Items
	separator := bodyCfg.Separator
	if separator == "" {
		separator = ","
	}

	for i, row := range execCtx.Data {
		if i > 0 {
			buf.WriteString(separator)
		}
		item, err := executeItemTemplate(bodyCfg.Item, row, i, execCtx.Count)
		if err != nil {
			return "", fmt.Errorf("item[%d]: %w", i, err)
		}
		buf.WriteString(item)
	}

	// Footer
	if bodyCfg.Footer != "" {
		footer, err := executeTemplate("footer", bodyCfg.Footer, execCtx)
		if err != nil {
			return "", fmt.Errorf("footer: %w", err)
		}
		buf.WriteString(footer)
	}

	return buf.String(), nil
}

// executeTemplate executes a template with the execution context
func executeTemplate(name, tmpl string, data any) (string, error) {
	t, err := template.New(name).Funcs(TemplateFuncMap).Parse(tmpl)
	if err != nil {
		return "", err
	}

	var buf bytes.Buffer
	if err := t.Execute(&buf, data); err != nil {
		return "", err
	}

	return buf.String(), nil
}

// executeItemTemplate executes an item template with row data and index/count
func executeItemTemplate(tmpl string, row map[string]any, index, count int) (string, error) {
	// Build item context with row fields + _index and _count
	itemCtx := make(map[string]any)
	for k, v := range row {
		itemCtx[k] = v
	}
	itemCtx["_index"] = index
	itemCtx["_count"] = count

	return executeTemplate("item", tmpl, itemCtx)
}
