package workflow

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"text/template"
	"time"

	"sql-proxy/internal/workflow/step"
)

func (e *Executor) executeHTTPCallStep(ctx context.Context, cs *CompiledStep, execData step.ExecutionData) (*StepResult, error) {
	start := time.Now()
	result := &StepResult{}

	method := cs.Config.HTTPMethod
	if method == "" {
		method = "GET"
	}
	parse := cs.Config.Parse
	if parse == "" {
		parse = "json"
	}

	if cs.Config.TimeoutSec > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, time.Duration(cs.Config.TimeoutSec)*time.Second)
		defer cancel()
	}

	// Render URL (done once - URL doesn't change between retries)
	var urlBuf bytes.Buffer
	if err := cs.URLTmpl.Execute(&urlBuf, execData.TemplateData); err != nil {
		result.Error = fmt.Errorf("url template error: %w", err)
		result.DurationMs = time.Since(start).Milliseconds()
		return result, nil
	}
	targetURL := urlBuf.String()

	// Build initial request
	req, err := buildHTTPRequest(ctx, method, targetURL, cs.BodyTmpl, cs.HeaderTmpls, execData.TemplateData)
	if err != nil {
		result.Error = err
		result.DurationMs = time.Since(start).Milliseconds()
		return result, nil
	}

	// Retry configuration
	maxAttempts := 1
	if cs.Config.Retry != nil && cs.Config.Retry.Enabled {
		maxAttempts = cs.Config.Retry.MaxAttempts
		if maxAttempts <= 0 {
			maxAttempts = 3
		}
	}

	backoff := 1 * time.Second
	if cs.Config.Retry != nil && cs.Config.Retry.InitialBackoffSec > 0 {
		backoff = time.Duration(cs.Config.Retry.InitialBackoffSec) * time.Second
	}
	maxBackoff := 30 * time.Second
	if cs.Config.Retry != nil && cs.Config.Retry.MaxBackoffSec > 0 {
		maxBackoff = time.Duration(cs.Config.Retry.MaxBackoffSec) * time.Second
	}

	// Execute with retries
	var resp *http.Response
	var lastErr error

	for attempt := 1; attempt <= maxAttempts; attempt++ {
		resp, lastErr = e.httpClient.Do(req)
		if lastErr == nil && resp.StatusCode < 500 {
			break
		}

		if lastErr != nil {
			e.logger.Warn("httpcall_attempt_failed", map[string]any{
				"step":    cs.Config.Name,
				"attempt": attempt,
				"error":   lastErr.Error(),
			})
		} else if resp != nil && resp.StatusCode >= 500 {
			e.logger.Warn("httpcall_attempt_failed", map[string]any{
				"step":        cs.Config.Name,
				"attempt":     attempt,
				"status_code": resp.StatusCode,
			})
		}

		if resp != nil && resp.Body != nil {
			_ = resp.Body.Close()
		}

		if attempt < maxAttempts {
			select {
			case <-ctx.Done():
				result.Error = ctx.Err()
				result.DurationMs = time.Since(start).Milliseconds()
				return result, nil
			case <-time.After(backoff):
			}
			backoff *= 2
			if backoff > maxBackoff {
				backoff = maxBackoff
			}

			req, err = buildHTTPRequest(ctx, method, targetURL, cs.BodyTmpl, cs.HeaderTmpls, execData.TemplateData)
			if err != nil {
				result.Error = fmt.Errorf("%w (on retry)", err)
				result.DurationMs = time.Since(start).Milliseconds()
				return result, nil
			}
		}
	}

	if lastErr != nil {
		result.Error = lastErr
		result.DurationMs = time.Since(start).Milliseconds()
		return result, nil
	}
	defer func() { _ = resp.Body.Close() }()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		result.Error = fmt.Errorf("read response error: %w", err)
		result.DurationMs = time.Since(start).Milliseconds()
		return result, nil
	}

	result.StatusCode = resp.StatusCode
	result.Headers = resp.Header
	result.ResponseBody = string(body)
	result.Success = resp.StatusCode >= 200 && resp.StatusCode < 300
	result.DurationMs = time.Since(start).Milliseconds()

	if result.Success {
		switch parse {
		case "json":
			var parsed any
			if err := json.Unmarshal(body, &parsed); err != nil {
				result.Error = fmt.Errorf("json parse error: %w", err)
				result.Success = false
			} else {
				result.Data = normalizeJSONResponse(parsed)
				result.Count = len(result.Data)
			}
		case "form":
			values, err := url.ParseQuery(string(body))
			if err != nil {
				result.Error = fmt.Errorf("form parse error: %w", err)
				result.Success = false
			} else {
				row := make(map[string]any)
				for k, v := range values {
					if len(v) == 1 {
						row[k] = v[0]
					} else {
						row[k] = v
					}
				}
				result.Data = []map[string]any{row}
				result.Count = 1
			}
		case "text":
			result.Data = []map[string]any{{"body": string(body)}}
			result.Count = 1
		}
	}

	e.logger.Debug("httpcall_step_executed", map[string]any{
		"step":        cs.Config.Name,
		"url":         targetURL,
		"method":      method,
		"status_code": result.StatusCode,
		"duration_ms": result.DurationMs,
	})

	return result, nil
}

// buildHTTPRequest creates an HTTP request with rendered body and headers.
func buildHTTPRequest(ctx context.Context, method, url string, bodyTmpl *template.Template, headerTmpls map[string]*template.Template, data map[string]any) (*http.Request, error) {
	var bodyReader io.Reader
	if bodyTmpl != nil {
		var bodyBuf bytes.Buffer
		if err := bodyTmpl.Execute(&bodyBuf, data); err != nil {
			return nil, fmt.Errorf("body template error: %w", err)
		}
		bodyReader = &bodyBuf
	}

	req, err := http.NewRequestWithContext(ctx, method, url, bodyReader)
	if err != nil {
		return nil, fmt.Errorf("create request error: %w", err)
	}

	for name, tmpl := range headerTmpls {
		var headerBuf bytes.Buffer
		if err := tmpl.Execute(&headerBuf, data); err != nil {
			return nil, fmt.Errorf("header '%s' template error: %w", name, err)
		}
		req.Header.Set(name, headerBuf.String())
	}

	if bodyReader != nil && req.Header.Get("Content-Type") == "" {
		req.Header.Set("Content-Type", "application/json")
	}

	return req, nil
}

func normalizeJSONResponse(v any) []map[string]any {
	switch val := v.(type) {
	case []any:
		result := make([]map[string]any, 0, len(val))
		for _, item := range val {
			if m, ok := item.(map[string]any); ok {
				result = append(result, m)
			} else {
				result = append(result, map[string]any{"value": item})
			}
		}
		return result
	case map[string]any:
		return []map[string]any{val}
	default:
		return []map[string]any{{"value": val}}
	}
}
