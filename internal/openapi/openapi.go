package openapi

import (
	"strconv"

	"sql-proxy/internal/config"
)

// Spec generates an OpenAPI 3.0 specification from the config
func Spec(cfg *config.Config) map[string]any {
	spec := map[string]any{
		"openapi": "3.0.3",
		"info": map[string]any{
			"title":       "SQL Proxy API",
			"description": "Auto-generated API for database query endpoints (SQL Server, SQLite)",
			"version":     "1.0.0",
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

	// Add built-in endpoints
	paths["/health"] = map[string]any{
		"get": map[string]any{
			"summary":     "Health check",
			"description": "Returns service and database health status",
			"tags":        []string{"System"},
			"responses": map[string]any{
				"200": map[string]any{
					"description": "Service is healthy",
					"content": map[string]any{
						"application/json": map[string]any{
							"schema": map[string]any{"$ref": "#/components/schemas/HealthResponse"},
						},
					},
				},
				"503": map[string]any{
					"description": "Service is degraded (database disconnected)",
				},
			},
		},
	}

	paths["/health/{dbname}"] = map[string]any{
		"get": map[string]any{
			"summary":     "Per-database health check",
			"description": "Returns health status for a specific database connection",
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
					"description": "Database is connected",
					"content": map[string]any{
						"application/json": map[string]any{
							"schema": map[string]any{"$ref": "#/components/schemas/DbHealthResponse"},
						},
					},
				},
				"404": map[string]any{
					"description": "Database not found",
				},
				"503": map[string]any{
					"description": "Database is disconnected",
				},
			},
		},
	}

	paths["/metrics"] = map[string]any{
		"get": map[string]any{
			"summary":     "Metrics snapshot",
			"description": "Returns current metrics including request counts, latencies, and error rates",
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

	paths["/config/loglevel"] = map[string]any{
		"get": map[string]any{
			"summary":     "Get current log level",
			"tags":        []string{"System"},
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

	paths["/cache/clear"] = map[string]any{
		"post": map[string]any{
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
		},
	}

	// Add query endpoints (skip schedule-only queries without HTTP paths)
	for _, q := range cfg.Queries {
		if q.Path != "" {
			paths[q.Path] = buildQueryPath(q, cfg.Server)
		}
	}

	return paths
}

func buildQueryPath(q config.QueryConfig, serverCfg config.ServerConfig) map[string]any {
	method := "get"
	if q.Method == "POST" {
		method = "post"
	}

	// Build parameters
	params := []map[string]any{
		{
			"name":        "_timeout",
			"in":          "query",
			"required":    false,
			"description": "Query timeout in seconds (max: " + strconv.Itoa(serverCfg.MaxTimeoutSec) + ")",
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

	for _, p := range q.Parameters {
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
	if q.TimeoutSec > 0 {
		effectiveTimeout = q.TimeoutSec
	}

	return map[string]any{
		method: map[string]any{
			"summary":     q.Name,
			"description": q.Description + " (default timeout: " + strconv.Itoa(effectiveTimeout) + "s)",
			"tags":        []string{"Queries"},
			"operationId": q.Name,
			"parameters":  params,
			"responses": map[string]any{
				"200": map[string]any{
					"description": "Successful query",
					"content": map[string]any{
						"application/json": map[string]any{
							"schema": map[string]any{"$ref": "#/components/schemas/QueryResponse"},
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
					"description": "Query execution failed",
					"content": map[string]any{
						"application/json": map[string]any{
							"schema": map[string]any{"$ref": "#/components/schemas/ErrorResponse"},
						},
					},
				},
				"504": map[string]any{
					"description": "Query timeout",
					"content": map[string]any{
						"application/json": map[string]any{
							"schema": map[string]any{"$ref": "#/components/schemas/ErrorResponse"},
						},
					},
				},
			},
		},
	}
}

func buildParamDescription(p config.ParamConfig) string {
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
			"QueryResponse": map[string]any{
				"type": "object",
				"properties": map[string]any{
					"success": map[string]any{
						"type":    "boolean",
						"example": true,
					},
					"data": map[string]any{
						"type":        "array",
						"items":       map[string]any{"type": "object"},
						"description": "Array of result rows",
					},
					"count": map[string]any{
						"type":        "integer",
						"description": "Number of rows returned",
					},
					"timeout_sec": map[string]any{
						"type":        "integer",
						"description": "Timeout used for this query",
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
						"enum": []string{"healthy", "degraded"},
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
		},
	}
}

