package openapi

import (
	"strconv"

	"sql-proxy/internal/config"
	"sql-proxy/internal/workflow"
)

// Spec generates an OpenAPI 3.0 specification from the config
func Spec(cfg *config.Config) map[string]any {
	// Use configured API version, default to "1.0.0" if not set
	apiVersion := cfg.Server.APIVersion
	if apiVersion == "" {
		apiVersion = "1.0.0"
	}

	spec := map[string]any{
		"openapi": "3.0.3",
		"info": map[string]any{
			"title":       "SQL Proxy API",
			"description": "Auto-generated API for workflow endpoints (SQL Server, SQLite)",
			"version":     apiVersion,
		},
		"servers": []map[string]any{
			{"url": "/", "description": "Current server"},
		},
		"paths":      buildPaths(cfg),
		"components": buildComponents(),
	}

	return spec
}

func buildPaths(cfg *config.Config) map[string]any {
	paths := make(map[string]any)

	// Add built-in endpoints (/_/ prefix is reserved for internal endpoints)
	paths["/_/health"] = map[string]any{
		"get": map[string]any{
			"summary":     "Health check",
			"description": "Returns service and database health status. Always returns 200; parse the 'status' field (healthy/degraded/unhealthy) for actual state.",
			"tags":        []string{"System"},
			"responses": map[string]any{
				"200": map[string]any{
					"description": "Health status (check 'status' field for healthy/degraded/unhealthy)",
					"content": map[string]any{
						"application/json": map[string]any{
							"schema": map[string]any{"$ref": "#/components/schemas/HealthResponse"},
						},
					},
				},
			},
		},
	}

	paths["/_/health/{dbname}"] = map[string]any{
		"get": map[string]any{
			"summary":     "Per-database health check",
			"description": "Returns health status for a specific database connection. Always returns 200 if database exists; parse 'status' field (connected/disconnected).",
			"tags":        []string{"System"},
			"parameters": []map[string]any{
				{
					"name":        "dbname",
					"in":          "path",
					"required":    true,
					"description": "Database connection name",
					"schema": map[string]any{
						"type": "string",
					},
				},
			},
			"responses": map[string]any{
				"200": map[string]any{
					"description": "Database status (check 'status' field for connected/disconnected)",
					"content": map[string]any{
						"application/json": map[string]any{
							"schema": map[string]any{"$ref": "#/components/schemas/DbHealthResponse"},
						},
					},
				},
				"404": map[string]any{
					"description": "Database not found in configuration",
				},
			},
		},
	}

	paths["/_/metrics"] = map[string]any{
		"get": map[string]any{
			"summary":     "Prometheus metrics",
			"description": "Returns metrics in Prometheus/OpenMetrics format for scraping by monitoring systems",
			"tags":        []string{"System"},
			"responses": map[string]any{
				"200": map[string]any{
					"description": "Prometheus metrics",
					"content": map[string]any{
						"text/plain": map[string]any{
							"schema": map[string]any{"type": "string"},
						},
					},
				},
			},
		},
	}

	paths["/_/metrics.json"] = map[string]any{
		"get": map[string]any{
			"summary":     "JSON metrics snapshot",
			"description": "Returns current metrics in human-readable JSON format including request counts, latencies, and error rates",
			"tags":        []string{"System"},
			"responses": map[string]any{
				"200": map[string]any{
					"description": "Metrics snapshot",
					"content": map[string]any{
						"application/json": map[string]any{
							"schema": map[string]any{"$ref": "#/components/schemas/MetricsResponse"},
						},
					},
				},
			},
		},
	}

	paths["/_/config/loglevel"] = map[string]any{
		"get": map[string]any{
			"summary": "Get current log level",
			"tags":    []string{"System"},
			"responses": map[string]any{
				"200": map[string]any{
					"description": "Current log level",
				},
			},
		},
		"post": map[string]any{
			"summary":     "Change log level",
			"description": "Change log level at runtime without restart",
			"tags":        []string{"System"},
			"parameters": []map[string]any{
				{
					"name":        "level",
					"in":          "query",
					"required":    true,
					"description": "Log level to set",
					"schema": map[string]any{
						"type": "string",
						"enum": []string{"debug", "info", "warn", "error"},
					},
				},
			},
			"responses": map[string]any{
				"200": map[string]any{
					"description": "Log level changed",
				},
			},
		},
	}

	cacheClearOp := map[string]any{
		"summary":     "Clear cache",
		"description": "Clear all cache entries or entries for a specific endpoint",
		"tags":        []string{"System"},
		"parameters": []map[string]any{
			{
				"name":        "endpoint",
				"in":          "query",
				"required":    false,
				"description": "Endpoint path to clear cache for (e.g., /api/machines). If omitted, clears all cache.",
				"schema": map[string]any{
					"type": "string",
				},
			},
		},
		"responses": map[string]any{
			"200": map[string]any{
				"description": "Cache cleared successfully",
			},
			"404": map[string]any{
				"description": "Cache not enabled",
			},
		},
	}
	paths["/_/cache/clear"] = map[string]any{
		"post":   cacheClearOp,
		"delete": cacheClearOp,
	}

	// Rate limits endpoint
	paths["/_/ratelimits"] = map[string]any{
		"get": map[string]any{
			"summary":     "Rate limit status",
			"description": "Returns rate limit configuration and current metrics for all pools",
			"tags":        []string{"System"},
			"responses": map[string]any{
				"200": map[string]any{
					"description": "Rate limit status",
					"content": map[string]any{
						"application/json": map[string]any{
							"schema": map[string]any{"$ref": "#/components/schemas/RateLimitsResponse"},
						},
					},
				},
			},
		},
	}

	// Add workflow HTTP trigger endpoints
	for _, wf := range cfg.Workflows {
		for _, trigger := range wf.Triggers {
			if trigger.Type == "http" && trigger.Path != "" {
				paths[trigger.Path] = buildWorkflowPath(wf, trigger, cfg.Server)
			}
		}
	}

	return paths
}

func buildWorkflowPath(wf workflow.WorkflowConfig, trigger workflow.TriggerConfig, serverCfg config.ServerConfig) map[string]any {
	method := "get"
	if trigger.Method == "POST" {
		method = "post"
	}

	// Build parameters
	params := []map[string]any{
		{
			"name":        "_timeout",
			"in":          "query",
			"required":    false,
			"description": "Workflow timeout in seconds (max: " + strconv.Itoa(serverCfg.MaxTimeoutSec) + ")",
			"schema": map[string]any{
				"type":    "integer",
				"default": serverCfg.DefaultTimeoutSec,
				"maximum": serverCfg.MaxTimeoutSec,
			},
		},
		{
			"name":        "_nocache",
			"in":          "query",
			"required":    false,
			"description": "Bypass cache and fetch fresh data (set to 1 to enable)",
			"schema": map[string]any{
				"type": "integer",
				"enum": []int{0, 1},
			},
		},
	}

	for _, p := range trigger.Parameters {
		param := map[string]any{
			"name":        p.Name,
			"in":          "query",
			"required":    p.Required,
			"description": buildParamDescription(p),
			"schema":      paramTypeToSchema(p.Type, p.Default),
		}
		params = append(params, param)
	}

	effectiveTimeout := serverCfg.DefaultTimeoutSec
	if wf.TimeoutSec > 0 {
		effectiveTimeout = wf.TimeoutSec
	}

	responses := map[string]any{
		"200": map[string]any{
			"description": "Successful workflow execution",
			"content": map[string]any{
				"application/json": map[string]any{
					"schema": map[string]any{"$ref": "#/components/schemas/WorkflowResponse"},
				},
			},
		},
		"400": map[string]any{
			"description": "Bad request (missing or invalid parameters)",
			"content": map[string]any{
				"application/json": map[string]any{
					"schema": map[string]any{"$ref": "#/components/schemas/ErrorResponse"},
				},
			},
		},
		"500": map[string]any{
			"description": "Workflow execution failed",
			"content": map[string]any{
				"application/json": map[string]any{
					"schema": map[string]any{"$ref": "#/components/schemas/ErrorResponse"},
				},
			},
		},
		"504": map[string]any{
			"description": "Workflow timeout",
			"content": map[string]any{
				"application/json": map[string]any{
					"schema": map[string]any{"$ref": "#/components/schemas/ErrorResponse"},
				},
			},
		},
	}

	// Add 429 response if rate limiting is configured for this trigger
	if len(trigger.RateLimit) > 0 {
		responses["429"] = map[string]any{
			"description": "Rate limit exceeded",
			"headers": map[string]any{
				"Retry-After": map[string]any{
					"description": "Seconds to wait before retrying",
					"schema": map[string]any{
						"type": "integer",
					},
				},
			},
			"content": map[string]any{
				"application/json": map[string]any{
					"schema": map[string]any{"$ref": "#/components/schemas/RateLimitErrorResponse"},
				},
			},
		}
	}

	return map[string]any{
		method: map[string]any{
			"summary":     wf.Name,
			"description": "Workflow endpoint (default timeout: " + strconv.Itoa(effectiveTimeout) + "s)",
			"tags":        []string{"Workflows"},
			"operationId": wf.Name,
			"parameters":  params,
			"responses":   responses,
		},
	}
}

func buildParamDescription(p workflow.ParamConfig) string {
	desc := "Type: " + p.Type
	if p.Default != "" {
		desc += ", Default: " + p.Default
	}
	return desc
}

func paramTypeToSchema(typeName string, defaultVal string) map[string]any {
	schema := make(map[string]any)

	switch typeName {
	case "int", "integer":
		schema["type"] = "integer"
		if defaultVal != "" {
			// Parse default to int for correct schema type
			if v, err := strconv.Atoi(defaultVal); err == nil {
				schema["default"] = v
			}
		}
	case "float", "double":
		schema["type"] = "number"
		schema["format"] = "double"
		if defaultVal != "" {
			// Parse default to float for correct schema type
			if v, err := strconv.ParseFloat(defaultVal, 64); err == nil {
				schema["default"] = v
			}
		}
	case "bool", "boolean":
		schema["type"] = "boolean"
		if defaultVal != "" {
			// Parse default to bool for correct schema type
			if v, err := strconv.ParseBool(defaultVal); err == nil {
				schema["default"] = v
			}
		}
	case "datetime", "date":
		schema["type"] = "string"
		schema["format"] = "date-time"
		if defaultVal != "" {
			schema["default"] = defaultVal
		}
	case "json":
		// JSON type accepts any JSON value (object, array, primitive)
		// Passed to SQL as a JSON string for use with JSON_VALUE/json_extract
		schema["type"] = "string"
		schema["description"] = "JSON value (object, array, or primitive). Use JSON functions in SQL to extract fields."
		if defaultVal != "" {
			schema["default"] = defaultVal
		}
	case "int[]":
		// Array of integers, passed as JSON array string
		schema["type"] = "array"
		schema["items"] = map[string]any{"type": "integer"}
		schema["description"] = "Array of integers. Passed as JSON array for use with json_each (SQLite) or OPENJSON (SQL Server)."
	case "string[]":
		schema["type"] = "array"
		schema["items"] = map[string]any{"type": "string"}
		schema["description"] = "Array of strings. Passed as JSON array for use with json_each (SQLite) or OPENJSON (SQL Server)."
	case "float[]":
		schema["type"] = "array"
		schema["items"] = map[string]any{"type": "number"}
		schema["description"] = "Array of numbers. Passed as JSON array for use with json_each (SQLite) or OPENJSON (SQL Server)."
	case "bool[]":
		schema["type"] = "array"
		schema["items"] = map[string]any{"type": "boolean"}
		schema["description"] = "Array of booleans. Passed as JSON array for use with json_each (SQLite) or OPENJSON (SQL Server)."
	default: // string
		schema["type"] = "string"
		if defaultVal != "" {
			schema["default"] = defaultVal
		}
	}

	return schema
}

func buildComponents() map[string]any {
	return map[string]any{
		"schemas": map[string]any{
			"WorkflowResponse": map[string]any{
				"type": "object",
				"properties": map[string]any{
					"success": map[string]any{
						"type":    "boolean",
						"example": true,
					},
					"data": map[string]any{
						"type":        "array",
						"items":       map[string]any{"type": "object"},
						"description": "Array of result rows (from query steps)",
					},
					"count": map[string]any{
						"type":        "integer",
						"description": "Number of rows returned",
					},
					"request_id": map[string]any{
						"type":        "string",
						"description": "Unique request ID for tracing",
					},
				},
			},
			"ErrorResponse": map[string]any{
				"type": "object",
				"properties": map[string]any{
					"success": map[string]any{
						"type":    "boolean",
						"example": false,
					},
					"error": map[string]any{
						"type":        "string",
						"description": "Error message",
					},
					"request_id": map[string]any{
						"type":        "string",
						"description": "Unique request ID for tracing",
					},
				},
			},
			"HealthResponse": map[string]any{
				"type": "object",
				"properties": map[string]any{
					"status": map[string]any{
						"type": "string",
						"enum": []string{"healthy", "degraded", "unhealthy"},
					},
					"databases": map[string]any{
						"type":        "object",
						"description": "Per-database connection status (connected/disconnected)",
						"additionalProperties": map[string]any{
							"type": "string",
							"enum": []string{"connected", "disconnected"},
						},
					},
					"uptime": map[string]any{
						"type":        "string",
						"description": "Service uptime",
					},
				},
			},
			"DbHealthResponse": map[string]any{
				"type": "object",
				"properties": map[string]any{
					"database": map[string]any{
						"type":        "string",
						"description": "Database connection name",
					},
					"status": map[string]any{
						"type": "string",
						"enum": []string{"connected", "disconnected"},
					},
					"type": map[string]any{
						"type":        "string",
						"description": "Database type (sqlserver, sqlite)",
					},
					"readonly": map[string]any{
						"type":        "boolean",
						"description": "Whether connection is read-only",
					},
				},
			},
			"MetricsResponse": map[string]any{
				"type": "object",
				"properties": map[string]any{
					"timestamp": map[string]any{
						"type":   "string",
						"format": "date-time",
					},
					"uptime_sec": map[string]any{
						"type": "integer",
					},
					"total_requests": map[string]any{
						"type": "integer",
					},
					"total_errors": map[string]any{
						"type": "integer",
					},
					"db_healthy": map[string]any{
						"type": "boolean",
					},
					"endpoints": map[string]any{
						"type":        "object",
						"description": "Per-endpoint statistics",
					},
				},
			},
			"RateLimitsResponse": map[string]any{
				"type": "object",
				"properties": map[string]any{
					"enabled": map[string]any{
						"type": "boolean",
					},
					"total_allowed": map[string]any{
						"type":        "integer",
						"description": "Total requests allowed since startup",
					},
					"total_denied": map[string]any{
						"type":        "integer",
						"description": "Total requests denied since startup",
					},
					"pools": map[string]any{
						"type":        "array",
						"description": "Rate limit pool configurations and metrics",
						"items": map[string]any{
							"type": "object",
							"properties": map[string]any{
								"name": map[string]any{
									"type": "string",
								},
								"requests_per_second": map[string]any{
									"type": "integer",
								},
								"burst": map[string]any{
									"type": "integer",
								},
								"allowed": map[string]any{
									"type": "integer",
								},
								"denied": map[string]any{
									"type": "integer",
								},
								"active_buckets": map[string]any{
									"type":        "integer",
									"description": "Number of active client buckets",
								},
							},
						},
					},
				},
			},
			"RateLimitErrorResponse": map[string]any{
				"type": "object",
				"properties": map[string]any{
					"success": map[string]any{
						"type":    "boolean",
						"example": false,
					},
					"error": map[string]any{
						"type":    "string",
						"example": "rate limit exceeded",
					},
					"retry_after_sec": map[string]any{
						"type":        "integer",
						"description": "Seconds to wait before retrying",
					},
				},
			},
		},
	}
}
