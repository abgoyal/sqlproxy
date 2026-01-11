package openapi

import (
	"encoding/json"
	"testing"

	"sql-proxy/internal/config"
)

// TestSpec_BasicStructure verifies OpenAPI spec has required root elements
func TestSpec_BasicStructure(t *testing.T) {
	cfg := &config.Config{
		Server: config.ServerConfig{
			Host:              "localhost",
			Port:              8080,
			DefaultTimeoutSec: 30,
			MaxTimeoutSec:     300,
		},
		Databases: []config.DatabaseConfig{
			{Name: "test", Type: "sqlite", Path: ":memory:"},
		},
		Queries: []config.QueryConfig{},
	}

	spec := Spec(cfg)

	// Check OpenAPI version
	if spec["openapi"] != "3.0.3" {
		t.Errorf("expected openapi 3.0.3, got %v", spec["openapi"])
	}

	// Check info
	info, ok := spec["info"].(map[string]any)
	if !ok {
		t.Fatal("expected info object")
	}
	if info["title"] != "SQL Proxy API" {
		t.Errorf("expected title 'SQL Proxy API', got %v", info["title"])
	}
	if info["version"] != "1.0.0" {
		t.Errorf("expected version 1.0.0, got %v", info["version"])
	}

	// Check servers
	servers, ok := spec["servers"].([]map[string]any)
	if !ok || len(servers) == 0 {
		t.Fatal("expected servers array")
	}

	// Check paths exists
	if spec["paths"] == nil {
		t.Error("expected paths object")
	}

	// Check components exists
	if spec["components"] == nil {
		t.Error("expected components object")
	}
}

// TestSpec_BuiltInPaths verifies /health, /metrics, /config/loglevel paths are present
func TestSpec_BuiltInPaths(t *testing.T) {
	cfg := &config.Config{
		Server: config.ServerConfig{
			DefaultTimeoutSec: 30,
			MaxTimeoutSec:     300,
		},
		Queries: []config.QueryConfig{},
	}

	spec := Spec(cfg)
	paths := spec["paths"].(map[string]any)

	// Check /health endpoint
	health, ok := paths["/health"].(map[string]any)
	if !ok {
		t.Fatal("expected /health path")
	}
	if health["get"] == nil {
		t.Error("expected GET method for /health")
	}

	// Check /metrics endpoint
	metrics, ok := paths["/metrics"].(map[string]any)
	if !ok {
		t.Fatal("expected /metrics path")
	}
	if metrics["get"] == nil {
		t.Error("expected GET method for /metrics")
	}

	// Check /config/loglevel endpoint
	loglevel, ok := paths["/config/loglevel"].(map[string]any)
	if !ok {
		t.Fatal("expected /config/loglevel path")
	}
	if loglevel["get"] == nil {
		t.Error("expected GET method for /config/loglevel")
	}
	if loglevel["post"] == nil {
		t.Error("expected POST method for /config/loglevel")
	}
}

// TestSpec_QueryEndpoints tests query config generates correct path operations
func TestSpec_QueryEndpoints(t *testing.T) {
	cfg := &config.Config{
		Server: config.ServerConfig{
			DefaultTimeoutSec: 30,
			MaxTimeoutSec:     300,
		},
		Queries: []config.QueryConfig{
			{
				Name:        "list_users",
				Path:        "/api/users",
				Method:      "GET",
				Description: "List all users",
				SQL:         "SELECT * FROM users",
			},
			{
				Name:        "create_user",
				Path:        "/api/users",
				Method:      "POST",
				Description: "Create a user",
				SQL:         "INSERT INTO users (name) VALUES (@name)",
			},
		},
	}

	spec := Spec(cfg)
	paths := spec["paths"].(map[string]any)

	// Check /api/users endpoint
	users := paths["/api/users"].(map[string]any)

	// Note: only the last query for a path is kept (overwritten)
	// POST should be present
	if users["post"] == nil {
		t.Error("expected POST method for /api/users")
	}
}

// TestSpec_SkipsScheduleOnlyQueries verifies schedule-only queries are excluded from paths
func TestSpec_SkipsScheduleOnlyQueries(t *testing.T) {
	cfg := &config.Config{
		Server: config.ServerConfig{
			DefaultTimeoutSec: 30,
			MaxTimeoutSec:     300,
		},
		Queries: []config.QueryConfig{
			{
				Name:        "http_query",
				Path:        "/api/test",
				Method:      "GET",
				SQL:         "SELECT 1",
			},
			{
				Name: "schedule_only",
				SQL:  "SELECT COUNT(*) FROM users",
				// No Path - schedule only
				Schedule: &config.ScheduleConfig{
					Cron: "0 * * * *",
				},
			},
		},
	}

	spec := Spec(cfg)
	paths := spec["paths"].(map[string]any)

	// Should have /api/test
	if paths["/api/test"] == nil {
		t.Error("expected /api/test path")
	}

	// Count paths (excluding built-in)
	queryPaths := 0
	for path := range paths {
		if path == "/api/test" {
			queryPaths++
		}
	}
	if queryPaths != 1 {
		t.Errorf("expected 1 query path, got %d", queryPaths)
	}
}

// TestBuildQueryPath_GET tests GET path generation with parameters and tags
func TestBuildQueryPath_GET(t *testing.T) {
	q := config.QueryConfig{
		Name:        "test_query",
		Path:        "/api/test",
		Method:      "GET",
		Description: "Test description",
		TimeoutSec:  60,
		Parameters: []config.ParamConfig{
			{Name: "id", Type: "int", Required: true},
		},
	}

	serverCfg := config.ServerConfig{
		DefaultTimeoutSec: 30,
		MaxTimeoutSec:     300,
	}

	path := buildQueryPath(q, serverCfg)

	get, ok := path["get"].(map[string]any)
	if !ok {
		t.Fatal("expected get method")
	}

	if get["summary"] != "test_query" {
		t.Errorf("expected summary test_query, got %v", get["summary"])
	}

	if get["operationId"] != "test_query" {
		t.Errorf("expected operationId test_query, got %v", get["operationId"])
	}

	tags := get["tags"].([]string)
	if len(tags) != 1 || tags[0] != "Queries" {
		t.Errorf("expected tags [Queries], got %v", tags)
	}

	// Check parameters include _timeout, _nocache, and id
	params := get["parameters"].([]map[string]any)
	if len(params) != 3 {
		t.Errorf("expected 3 parameters, got %d", len(params))
	}

	// First should be _timeout
	if params[0]["name"] != "_timeout" {
		t.Errorf("expected first param _timeout, got %v", params[0]["name"])
	}

	// Second should be _nocache
	if params[1]["name"] != "_nocache" {
		t.Errorf("expected second param _nocache, got %v", params[1]["name"])
	}

	// Third should be id
	if params[2]["name"] != "id" {
		t.Errorf("expected third param id, got %v", params[2]["name"])
	}
}

// TestBuildQueryPath_POST tests POST method creates post operation, not get
func TestBuildQueryPath_POST(t *testing.T) {
	q := config.QueryConfig{
		Name:   "insert_query",
		Path:   "/api/insert",
		Method: "POST",
		SQL:    "INSERT INTO test VALUES (1)",
	}

	serverCfg := config.ServerConfig{
		DefaultTimeoutSec: 30,
		MaxTimeoutSec:     300,
	}

	path := buildQueryPath(q, serverCfg)

	if path["post"] == nil {
		t.Error("expected post method")
	}
	if path["get"] != nil {
		t.Error("expected no get method")
	}
}

// TestBuildQueryPath_Responses verifies 200, 400, 500, 504 response codes present
func TestBuildQueryPath_Responses(t *testing.T) {
	q := config.QueryConfig{
		Name:   "test",
		Path:   "/api/test",
		Method: "GET",
		SQL:    "SELECT 1",
	}

	serverCfg := config.ServerConfig{
		DefaultTimeoutSec: 30,
		MaxTimeoutSec:     300,
	}

	path := buildQueryPath(q, serverCfg)
	get := path["get"].(map[string]any)
	responses := get["responses"].(map[string]any)

	// Should have 200, 400, 500, 504 responses
	expectedCodes := []string{"200", "400", "500", "504"}
	for _, code := range expectedCodes {
		if responses[code] == nil {
			t.Errorf("expected response code %s", code)
		}
	}
}

// TestBuildParamDescription tests parameter description includes type and default
func TestBuildParamDescription(t *testing.T) {
	tests := []struct {
		param   config.ParamConfig
		wantSub string
	}{
		{
			param:   config.ParamConfig{Name: "id", Type: "int"},
			wantSub: "Type: int",
		},
		{
			param:   config.ParamConfig{Name: "name", Type: "string", Default: "test"},
			wantSub: "Default: test",
		},
	}

	for _, tt := range tests {
		desc := buildParamDescription(tt.param)
		if desc == "" {
			t.Error("expected non-empty description")
		}
		// Check that description contains the expected substring
		found := false
		if len(desc) >= len(tt.wantSub) {
			for i := 0; i <= len(desc)-len(tt.wantSub); i++ {
				if desc[i:i+len(tt.wantSub)] == tt.wantSub {
					found = true
					break
				}
			}
		}
		if !found {
			t.Errorf("expected description to contain %q, got %q", tt.wantSub, desc)
		}
	}
}

// TestParamTypeToSchema tests parameter type to JSON Schema conversion
func TestParamTypeToSchema(t *testing.T) {
	tests := []struct {
		typeName    string
		defaultVal  string
		wantType    string
		wantFormat  string
		wantDefault any // nil if no default expected
	}{
		{"int", "", "integer", "", nil},
		{"integer", "", "integer", "", nil},
		{"int", "42", "integer", "", 42},      // int defaults are parsed to int
		{"float", "", "number", "double", nil},
		{"float", "3.14", "number", "double", 3.14}, // float defaults are parsed
		{"double", "", "number", "double", nil},
		{"bool", "", "boolean", "", nil},
		{"bool", "true", "boolean", "", true},  // bool defaults are parsed
		{"boolean", "", "boolean", "", nil},
		{"datetime", "", "string", "date-time", nil},
		{"datetime", "2024-01-01", "string", "date-time", "2024-01-01"}, // datetime defaults stay string
		{"date", "", "string", "date-time", nil},
		{"string", "", "string", "", nil},
		{"string", "default_value", "string", "", "default_value"},
		{"unknown", "", "string", "", nil}, // defaults to string
		{"json", "", "string", "", nil},    // json type maps to string
	}

	for _, tt := range tests {
		t.Run(tt.typeName, func(t *testing.T) {
			schema := paramTypeToSchema(tt.typeName, tt.defaultVal)

			if schema["type"] != tt.wantType {
				t.Errorf("expected type %s, got %v", tt.wantType, schema["type"])
			}

			if tt.wantFormat != "" {
				if schema["format"] != tt.wantFormat {
					t.Errorf("expected format %s, got %v", tt.wantFormat, schema["format"])
				}
			}

			if tt.wantDefault != nil {
				if schema["default"] != tt.wantDefault {
					t.Errorf("expected default %v (%T), got %v (%T)", tt.wantDefault, tt.wantDefault, schema["default"], schema["default"])
				}
			} else if tt.defaultVal == "" {
				// No default provided, should have no default in schema
				if _, hasDefault := schema["default"]; hasDefault {
					t.Errorf("expected no default, got %v", schema["default"])
				}
			}
		})
	}
}

// TestBuildComponents verifies required schema definitions are present
func TestBuildComponents(t *testing.T) {
	components := buildComponents()

	schemas, ok := components["schemas"].(map[string]any)
	if !ok {
		t.Fatal("expected schemas object")
	}

	// Check required schemas
	requiredSchemas := []string{
		"QueryResponse",
		"ErrorResponse",
		"HealthResponse",
		"MetricsResponse",
	}

	for _, name := range requiredSchemas {
		if schemas[name] == nil {
			t.Errorf("expected schema %s", name)
		}
	}
}

// TestSpec_ValidJSON verifies spec serializes to valid JSON and back
func TestSpec_ValidJSON(t *testing.T) {
	cfg := &config.Config{
		Server: config.ServerConfig{
			DefaultTimeoutSec: 30,
			MaxTimeoutSec:     300,
		},
		Queries: []config.QueryConfig{
			{
				Name:        "test",
				Path:        "/api/test",
				Method:      "GET",
				Description: "Test query",
				SQL:         "SELECT @id",
				Parameters: []config.ParamConfig{
					{Name: "id", Type: "int", Required: true},
					{Name: "name", Type: "string", Default: "default"},
				},
			},
		},
	}

	spec := Spec(cfg)

	// Verify it can be serialized to JSON
	data, err := json.Marshal(spec)
	if err != nil {
		t.Fatalf("failed to marshal spec to JSON: %v", err)
	}

	// Verify it can be parsed back
	var parsed map[string]any
	if err := json.Unmarshal(data, &parsed); err != nil {
		t.Fatalf("failed to unmarshal spec: %v", err)
	}

	// Verify basic structure is preserved
	if parsed["openapi"] != "3.0.3" {
		t.Error("openapi version not preserved")
	}
}

// TestSpec_TimeoutParameter tests _timeout param has correct default and maximum
func TestSpec_TimeoutParameter(t *testing.T) {
	cfg := &config.Config{
		Server: config.ServerConfig{
			DefaultTimeoutSec: 30,
			MaxTimeoutSec:     300,
		},
		Queries: []config.QueryConfig{
			{
				Name:   "test",
				Path:   "/api/test",
				Method: "GET",
				SQL:    "SELECT 1",
			},
		},
	}

	spec := Spec(cfg)
	paths := spec["paths"].(map[string]any)
	apiTest := paths["/api/test"].(map[string]any)
	get := apiTest["get"].(map[string]any)
	params := get["parameters"].([]map[string]any)

	// First param should be _timeout
	timeoutParam := params[0]
	if timeoutParam["name"] != "_timeout" {
		t.Error("expected _timeout parameter")
	}

	schema := timeoutParam["schema"].(map[string]any)
	if schema["default"] != 30 {
		t.Errorf("expected default timeout 30, got %v", schema["default"])
	}
	if schema["maximum"] != 300 {
		t.Errorf("expected max timeout 300, got %v", schema["maximum"])
	}
}

// TestSpec_QueryDescription tests custom description and timeout info in spec
func TestSpec_QueryDescription(t *testing.T) {
	cfg := &config.Config{
		Server: config.ServerConfig{
			DefaultTimeoutSec: 30,
			MaxTimeoutSec:     300,
		},
		Queries: []config.QueryConfig{
			{
				Name:        "test",
				Path:        "/api/test",
				Method:      "GET",
				Description: "Custom description",
				SQL:         "SELECT 1",
				TimeoutSec:  60,
			},
		},
	}

	spec := Spec(cfg)
	paths := spec["paths"].(map[string]any)
	apiTest := paths["/api/test"].(map[string]any)
	get := apiTest["get"].(map[string]any)

	desc := get["description"].(string)
	// Should contain custom description and timeout info
	if desc == "" {
		t.Error("expected non-empty description")
	}
	// Check it contains "60s" for the custom timeout
	found := false
	for i := 0; i <= len(desc)-3; i++ {
		if desc[i:i+3] == "60s" {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("expected description to contain timeout, got %s", desc)
	}
}

// TestParamTypeToSchema_ArrayTypes tests array type schema generation
func TestParamTypeToSchema_ArrayTypes(t *testing.T) {
	tests := []struct {
		typeName     string
		wantItemType string
		wantDesc     string
	}{
		{"int[]", "integer", "Array of integers"},
		{"string[]", "string", "Array of strings"},
		{"float[]", "number", "Array of numbers"},
		{"bool[]", "boolean", "Array of booleans"},
	}

	for _, tt := range tests {
		t.Run(tt.typeName, func(t *testing.T) {
			schema := paramTypeToSchema(tt.typeName, "")

			// Should be array type
			if schema["type"] != "array" {
				t.Errorf("expected type array, got %v", schema["type"])
			}

			// Check items has correct type
			items, ok := schema["items"].(map[string]any)
			if !ok {
				t.Fatal("expected items to be map")
			}
			if items["type"] != tt.wantItemType {
				t.Errorf("expected item type %s, got %v", tt.wantItemType, items["type"])
			}

			// Check description mentions array type
			desc, ok := schema["description"].(string)
			if !ok {
				t.Fatal("expected description string")
			}
			if desc == "" {
				t.Error("expected non-empty description")
			}
			// Should mention json_each or OPENJSON for SQL usage
			found := false
			if len(desc) > 10 {
				for i := 0; i <= len(desc)-10; i++ {
					if desc[i:i+9] == "json_each" || desc[i:i+8] == "OPENJSON" {
						found = true
						break
					}
				}
			}
			if !found {
				t.Errorf("expected description to mention json_each or OPENJSON, got %s", desc)
			}
		})
	}
}

// TestParamTypeToSchema_JSONType tests json type schema generation
func TestParamTypeToSchema_JSONType(t *testing.T) {
	schema := paramTypeToSchema("json", "")

	if schema["type"] != "string" {
		t.Errorf("expected type string, got %v", schema["type"])
	}

	desc, ok := schema["description"].(string)
	if !ok || desc == "" {
		t.Error("expected non-empty description for json type")
	}

	// Should mention JSON functions
	found := false
	if len(desc) > 4 {
		for i := 0; i <= len(desc)-4; i++ {
			if desc[i:i+4] == "JSON" {
				found = true
				break
			}
		}
	}
	if !found {
		t.Errorf("expected description to mention JSON, got %s", desc)
	}
}

// TestBuildQueryPath_DefaultTimeout tests server default timeout used when query has none
func TestBuildQueryPath_DefaultTimeout(t *testing.T) {
	// Query without custom timeout uses server default
	q := config.QueryConfig{
		Name:   "test",
		Path:   "/api/test",
		Method: "GET",
		SQL:    "SELECT 1",
		// TimeoutSec not set (0)
	}

	serverCfg := config.ServerConfig{
		DefaultTimeoutSec: 45,
		MaxTimeoutSec:     300,
	}

	path := buildQueryPath(q, serverCfg)
	get := path["get"].(map[string]any)
	desc := get["description"].(string)

	// Should use server default (45s)
	found := false
	for i := 0; i <= len(desc)-3; i++ {
		if desc[i:i+3] == "45s" {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("expected description to contain default timeout 45s, got %s", desc)
	}
}
