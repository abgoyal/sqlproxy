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

// ExecutionContext holds data available to templates
type ExecutionContext struct {
	Query      string            `json:"query"`       // Query name
	Count      int               `json:"count"`       // Row count
	Success    bool              `json:"success"`     // Whether query succeeded
	DurationMs int64             `json:"duration_ms"` // Query execution time
	Params     map[string]string `json:"params"`      // Query parameters
	Data       []map[string]any  `json:"data"`        // Query results
	Error      string            `json:"error"`       // Error message if failed
}

// funcMap provides custom template functions
var funcMap = template.FuncMap{
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

// Execute sends a webhook with the given context
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

	// Create request
	req, err := http.NewRequestWithContext(ctx, method, url, strings.NewReader(body))
	if err != nil {
		return fmt.Errorf("creating request: %w", err)
	}

	// Set default content type
	req.Header.Set("Content-Type", "application/json")

	// Apply headers (expand env vars)
	for key, value := range webhookCfg.Headers {
		req.Header.Set(key, os.ExpandEnv(value))
	}

	// Send request
	client := &http.Client{Timeout: 30 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("sending webhook: %w", err)
	}
	defer resp.Body.Close()

	// Read response for error reporting
	respBody, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))

	if resp.StatusCode >= 400 {
		return fmt.Errorf("webhook returned %d: %s", resp.StatusCode, string(respBody))
	}

	return nil
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
	t, err := template.New(name).Funcs(funcMap).Parse(tmpl)
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
