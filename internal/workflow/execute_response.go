package workflow

import (
	"bytes"
	"context"
	"fmt"
	"net/http"
	"time"

	"sql-proxy/internal/workflow/step"
)

func (e *Executor) executeResponseStep(ctx context.Context, cs *CompiledStep, execData step.ExecutionData) (*StepResult, error) {
	start := time.Now()
	result := &StepResult{}

	if execData.ResponseWriter == nil {
		result.Error = fmt.Errorf("response step called without ResponseWriter (cron trigger?)")
		result.DurationMs = time.Since(start).Milliseconds()
		return result, nil
	}

	var buf bytes.Buffer
	if err := cs.TemplateTmpl.Execute(&buf, execData.TemplateData); err != nil {
		result.Error = fmt.Errorf("response template error: %w", err)
		result.DurationMs = time.Since(start).Milliseconds()
		return result, nil
	}

	for name, tmpl := range cs.HeaderTmpls {
		var headerBuf bytes.Buffer
		if err := tmpl.Execute(&headerBuf, execData.TemplateData); err != nil {
			result.Error = fmt.Errorf("header %s template error: %w", name, err)
			result.DurationMs = time.Since(start).Milliseconds()
			return result, nil
		}
		execData.ResponseWriter.Header().Set(name, headerBuf.String())
	}

	statusCode := cs.Config.StatusCode
	if statusCode == 0 {
		statusCode = http.StatusOK
	}

	execData.ResponseWriter.Header().Set("Content-Type", "application/json")
	execData.ResponseWriter.WriteHeader(statusCode)
	if _, err := execData.ResponseWriter.Write(buf.Bytes()); err != nil {
		result.Error = fmt.Errorf("write response error: %w", err)
		result.DurationMs = time.Since(start).Milliseconds()
		return result, nil
	}

	result.Success = true
	result.StatusCode = statusCode
	result.ResponseBody = buf.String()
	result.DurationMs = time.Since(start).Milliseconds()

	e.logger.Debug("response_step_executed", map[string]any{
		"step":        cs.Config.Name,
		"status_code": statusCode,
		"body_length": buf.Len(),
		"duration_ms": result.DurationMs,
	})

	return result, nil
}
