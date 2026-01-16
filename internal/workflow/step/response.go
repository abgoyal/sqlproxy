package step

import (
	"bytes"
	"context"
	"fmt"
	"net/http"
	"text/template"
	"time"
)

// ResponseStep writes an HTTP response.
type ResponseStep struct {
	Name        string
	StatusCode  int
	Template    *template.Template
	Headers     map[string]*template.Template
	ContentType string
}

// NewResponseStep creates a response step from configuration.
func NewResponseStep(name string, statusCode int, tmpl *template.Template, headers map[string]*template.Template, contentType string) *ResponseStep {
	if statusCode == 0 {
		statusCode = http.StatusOK
	}
	if contentType == "" {
		contentType = "application/json"
	}
	return &ResponseStep{
		Name:        name,
		StatusCode:  statusCode,
		Template:    tmpl,
		Headers:     headers,
		ContentType: contentType,
	}
}

func (s *ResponseStep) Type() string {
	return "response"
}

func (s *ResponseStep) Execute(ctx context.Context, data ExecutionData) (*Result, error) {
	start := time.Now()
	result := &Result{}

	if data.ResponseWriter == nil {
		result.Error = fmt.Errorf("response step called without ResponseWriter (cron trigger?)")
		result.DurationMs = time.Since(start).Milliseconds()
		return result, nil
	}

	// Render template
	var buf bytes.Buffer
	if err := s.Template.Execute(&buf, data.TemplateData); err != nil {
		result.Error = fmt.Errorf("response template error: %w", err)
		result.DurationMs = time.Since(start).Milliseconds()
		return result, nil
	}

	// Set custom headers
	for name, tmpl := range s.Headers {
		var headerBuf bytes.Buffer
		if err := tmpl.Execute(&headerBuf, data.TemplateData); err != nil {
			result.Error = fmt.Errorf("header %s template error: %w", name, err)
			result.DurationMs = time.Since(start).Milliseconds()
			return result, nil
		}
		data.ResponseWriter.Header().Set(name, headerBuf.String())
	}

	// Write response
	data.ResponseWriter.Header().Set("Content-Type", s.ContentType)
	data.ResponseWriter.WriteHeader(s.StatusCode)
	if _, err := data.ResponseWriter.Write(buf.Bytes()); err != nil {
		result.Error = fmt.Errorf("write response error: %w", err)
		result.DurationMs = time.Since(start).Milliseconds()
		return result, nil
	}

	result.Success = true
	result.StatusCode = s.StatusCode
	result.ResponseBody = buf.String()
	result.DurationMs = time.Since(start).Milliseconds()

	if data.Logger != nil {
		data.Logger.Debug("response_step_executed", map[string]any{
			"step":        s.Name,
			"status_code": s.StatusCode,
			"body_length": buf.Len(),
			"duration_ms": result.DurationMs,
		})
	}

	return result, nil
}
