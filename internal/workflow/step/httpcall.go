package step

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
)

// HTTPCallStep executes an HTTP request.
type HTTPCallStep struct {
	Name            string
	URLTemplate     *template.Template
	Method          string
	HeaderTemplates map[string]*template.Template
	BodyTemplate    *template.Template
	Parse           string // "json" | "text" | "form"
	TimeoutSec      int
	Retry           *RetryConfig
}

// RetryConfig defines retry behavior for HTTP calls.
type RetryConfig struct {
	Enabled           bool
	MaxAttempts       int
	InitialBackoffSec int
	MaxBackoffSec     int
}

// NewHTTPCallStep creates an httpcall step from configuration.
func NewHTTPCallStep(name string, urlTmpl *template.Template, method string, headerTmpls map[string]*template.Template, bodyTmpl *template.Template, parse string, timeoutSec int, retry *RetryConfig) *HTTPCallStep {
	if method == "" {
		method = "GET"
	}
	if parse == "" {
		parse = "json"
	}
	return &HTTPCallStep{
		Name:            name,
		URLTemplate:     urlTmpl,
		Method:          method,
		HeaderTemplates: headerTmpls,
		BodyTemplate:    bodyTmpl,
		Parse:           parse,
		TimeoutSec:      timeoutSec,
		Retry:           retry,
	}
}

func (s *HTTPCallStep) Type() string {
	return "httpcall"
}

func (s *HTTPCallStep) Execute(ctx context.Context, data ExecutionData) (*Result, error) {
	start := time.Now()
	result := &Result{}

	// Render URL template
	var urlBuf bytes.Buffer
	if err := s.URLTemplate.Execute(&urlBuf, data.TemplateData); err != nil {
		result.Error = fmt.Errorf("url template error: %w", err)
		result.DurationMs = time.Since(start).Milliseconds()
		return result, nil
	}
	targetURL := urlBuf.String()

	// Render body template
	var bodyReader io.Reader
	if s.BodyTemplate != nil {
		var bodyBuf bytes.Buffer
		if err := s.BodyTemplate.Execute(&bodyBuf, data.TemplateData); err != nil {
			result.Error = fmt.Errorf("body template error: %w", err)
			result.DurationMs = time.Since(start).Milliseconds()
			return result, nil
		}
		bodyReader = &bodyBuf
	}

	// Create request
	req, err := http.NewRequestWithContext(ctx, s.Method, targetURL, bodyReader)
	if err != nil {
		result.Error = fmt.Errorf("create request error: %w", err)
		result.DurationMs = time.Since(start).Milliseconds()
		return result, nil
	}

	// Set headers
	for name, tmpl := range s.HeaderTemplates {
		var headerBuf bytes.Buffer
		if err := tmpl.Execute(&headerBuf, data.TemplateData); err != nil {
			result.Error = fmt.Errorf("header '%s' template error: %w", name, err)
			result.DurationMs = time.Since(start).Milliseconds()
			return result, nil
		}
		req.Header.Set(name, headerBuf.String())
	}

	// Set default content-type if body is present and not already set
	if bodyReader != nil && req.Header.Get("Content-Type") == "" {
		req.Header.Set("Content-Type", "application/json")
	}

	// Execute request with retries
	var resp *http.Response
	var lastErr error
	maxAttempts := 1
	if s.Retry != nil && s.Retry.Enabled {
		maxAttempts = s.Retry.MaxAttempts
		if maxAttempts <= 0 {
			maxAttempts = 3
		}
	}

	backoff := 1 * time.Second
	if s.Retry != nil && s.Retry.InitialBackoffSec > 0 {
		backoff = time.Duration(s.Retry.InitialBackoffSec) * time.Second
	}
	maxBackoff := 30 * time.Second
	if s.Retry != nil && s.Retry.MaxBackoffSec > 0 {
		maxBackoff = time.Duration(s.Retry.MaxBackoffSec) * time.Second
	}

	for attempt := 1; attempt <= maxAttempts; attempt++ {
		resp, lastErr = data.HTTPClient.Do(req)
		if lastErr == nil && resp.StatusCode < 500 {
			break // Success or client error (no retry)
		}

		if lastErr != nil && data.Logger != nil {
			data.Logger.Warn("httpcall_attempt_failed", map[string]any{
				"step":    s.Name,
				"attempt": attempt,
				"error":   lastErr.Error(),
			})
		} else if resp != nil && resp.StatusCode >= 500 && data.Logger != nil {
			data.Logger.Warn("httpcall_attempt_failed", map[string]any{
				"step":        s.Name,
				"attempt":     attempt,
				"status_code": resp.StatusCode,
			})
		}

		// Close response body before retry to prevent resource leak
		if resp != nil && resp.Body != nil {
			resp.Body.Close()
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

			// Recreate request for retry
			var retryBodyReader io.Reader
			if s.BodyTemplate != nil {
				var bodyBuf bytes.Buffer
				if err := s.BodyTemplate.Execute(&bodyBuf, data.TemplateData); err != nil {
					result.Error = fmt.Errorf("body template error on retry: %w", err)
					result.DurationMs = time.Since(start).Milliseconds()
					return result, nil
				}
				retryBodyReader = &bodyBuf
			}

			var err error
			req, err = http.NewRequestWithContext(ctx, s.Method, targetURL, retryBodyReader)
			if err != nil {
				result.Error = fmt.Errorf("create request error on retry: %w", err)
				result.DurationMs = time.Since(start).Milliseconds()
				return result, nil
			}

			for name, tmpl := range s.HeaderTemplates {
				var headerBuf bytes.Buffer
				if err := tmpl.Execute(&headerBuf, data.TemplateData); err != nil {
					result.Error = fmt.Errorf("header '%s' template error on retry: %w", name, err)
					result.DurationMs = time.Since(start).Milliseconds()
					return result, nil
				}
				req.Header.Set(name, headerBuf.String())
			}
			if retryBodyReader != nil && req.Header.Get("Content-Type") == "" {
				req.Header.Set("Content-Type", "application/json")
			}
		}
	}

	if lastErr != nil {
		result.Error = lastErr
		result.DurationMs = time.Since(start).Milliseconds()
		return result, nil
	}
	defer resp.Body.Close()

	// Read response body
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

	// Parse response based on parse mode
	if result.Success {
		switch s.Parse {
		case "json":
			var parsed any
			if err := json.Unmarshal(body, &parsed); err != nil {
				result.Error = fmt.Errorf("json parse error: %w", err)
				result.Success = false
			} else {
				// Convert to data format for subsequent steps
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

	if data.Logger != nil {
		data.Logger.Debug("httpcall_step_executed", map[string]any{
			"step":        s.Name,
			"url":         targetURL,
			"method":      s.Method,
			"status_code": result.StatusCode,
			"duration_ms": result.DurationMs,
		})
	}

	return result, nil
}

// normalizeJSONResponse converts JSON response to slice of maps format.
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
