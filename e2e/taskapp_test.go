package e2e

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// createTaskAppConfig reads the taskapp.yaml template and substitutes port and db path
func createTaskAppConfig(t *testing.T, port int, dbPath string) string {
	t.Helper()

	// Read template
	templatePath := filepath.Join("..", "testdata", "taskapp.yaml")
	content, err := os.ReadFile(templatePath)
	if err != nil {
		t.Fatalf("failed to read taskapp.yaml: %v", err)
	}

	// Substitute variables
	config := string(content)
	config = strings.ReplaceAll(config, "${PORT}", fmt.Sprintf("%d", port))
	config = strings.ReplaceAll(config, "${DB_PATH}", dbPath)

	// Write to temp file
	configPath := filepath.Join(t.TempDir(), "taskapp-config.yaml")
	if err := os.WriteFile(configPath, []byte(config), 0644); err != nil {
		t.Fatalf("failed to write config: %v", err)
	}

	return configPath
}

// initTaskAppDB calls the /api/init endpoint to create tables and seed data
func initTaskAppDB(t *testing.T, ts *testServer) {
	t.Helper()

	resp, err := ts.post("/api/init", "")
	if err != nil {
		t.Fatalf("failed to init db: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusCreated {
		body, _ := io.ReadAll(resp.Body)
		t.Fatalf("init db failed: status=%d body=%s", resp.StatusCode, body)
	}
}

// TestTaskApp_PathParameters tests path parameter extraction for /api/tasks/{id}
func TestTaskApp_PathParameters(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping e2e test in short mode")
	}

	binaryPath := buildBinary(t)
	port, err := findFreePort()
	if err != nil {
		t.Fatalf("failed to find free port: %v", err)
	}

	dbPath := filepath.Join(t.TempDir(), "taskapp.db")
	configPath := createTaskAppConfig(t, port, dbPath)

	ts := startServer(t, binaryPath, configPath, port)
	defer ts.stop()

	initTaskAppDB(t, ts)

	t.Run("GET_with_path_param", func(t *testing.T) {
		var result map[string]any
		resp, err := ts.getJSON("/api/tasks/1", &result)
		if err != nil {
			t.Fatalf("request failed: %v", err)
		}

		if resp.StatusCode != http.StatusOK {
			t.Errorf("expected 200, got %d", resp.StatusCode)
		}

		task, ok := result["task"].(map[string]any)
		if !ok {
			t.Fatalf("expected task object in response, got %v", result)
		}

		if task["id"].(float64) != 1 {
			t.Errorf("expected task id=1, got %v", task["id"])
		}

		if task["title"] != "Review PR" {
			t.Errorf("expected title='Review PR', got %v", task["title"])
		}
	})

	t.Run("GET_nonexistent_returns_404", func(t *testing.T) {
		var result map[string]any
		resp, err := ts.getJSON("/api/tasks/999", &result)
		if err != nil {
			t.Fatalf("request failed: %v", err)
		}

		if resp.StatusCode != http.StatusNotFound {
			t.Errorf("expected 404, got %d", resp.StatusCode)
		}

		if result["error"] != "Task not found" {
			t.Errorf("expected error message, got %v", result)
		}
	})

	t.Run("DELETE_with_path_param", func(t *testing.T) {
		// First create a task to delete
		resp, err := ts.post("/api/tasks", "title=ToDelete")
		if err != nil {
			t.Fatalf("create failed: %v", err)
		}
		var createResult map[string]any
		if err := json.NewDecoder(resp.Body).Decode(&createResult); err != nil {
			resp.Body.Close()
			t.Fatalf("decode create response: %v", err)
		}
		resp.Body.Close()
		idVal, ok := createResult["id"].(float64)
		if !ok {
			t.Fatalf("expected id to be float64, got %T", createResult["id"])
		}
		taskID := int(idVal)

		// Delete it
		req, _ := http.NewRequest("DELETE", fmt.Sprintf("%s/api/tasks/%d", ts.baseURL, taskID), nil)
		client := &http.Client{}
		resp, err = client.Do(req)
		if err != nil {
			t.Fatalf("delete request failed: %v", err)
		}
		defer resp.Body.Close()

		if resp.StatusCode != http.StatusOK {
			t.Errorf("expected 200, got %d", resp.StatusCode)
		}

		var deleteResult map[string]any
		if err := json.NewDecoder(resp.Body).Decode(&deleteResult); err != nil {
			t.Fatalf("decode delete response: %v", err)
		}

		if deleteResult["success"] != true {
			t.Errorf("expected success=true, got %v", deleteResult)
		}

		deletedID, ok := deleteResult["deleted_id"].(float64)
		if !ok {
			t.Fatalf("expected deleted_id to be float64, got %T", deleteResult["deleted_id"])
		}
		if int(deletedID) != taskID {
			t.Errorf("expected deleted_id=%d, got %v", taskID, deleteResult["deleted_id"])
		}

		// Verify it's gone
		resp, _ = ts.get(fmt.Sprintf("/api/tasks/%d", taskID))
		resp.Body.Close()
		if resp.StatusCode != http.StatusNotFound {
			t.Errorf("expected 404 after delete, got %d", resp.StatusCode)
		}
	})
}

// TestTaskApp_AllHTTPMethods tests all 7 HTTP methods on /api/tasks
func TestTaskApp_AllHTTPMethods(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping e2e test in short mode")
	}

	binaryPath := buildBinary(t)
	port, err := findFreePort()
	if err != nil {
		t.Fatalf("failed to find free port: %v", err)
	}

	dbPath := filepath.Join(t.TempDir(), "taskapp.db")
	configPath := createTaskAppConfig(t, port, dbPath)

	ts := startServer(t, binaryPath, configPath, port)
	defer ts.stop()

	initTaskAppDB(t, ts)

	client := &http.Client{}

	t.Run("GET", func(t *testing.T) {
		resp, err := ts.get("/api/tasks")
		if err != nil {
			t.Fatalf("GET failed: %v", err)
		}
		defer resp.Body.Close()

		if resp.StatusCode != http.StatusOK {
			t.Errorf("expected 200, got %d", resp.StatusCode)
		}
	})

	t.Run("POST", func(t *testing.T) {
		resp, err := ts.post("/api/tasks", "title=TestTask")
		if err != nil {
			t.Fatalf("POST failed: %v", err)
		}
		defer resp.Body.Close()

		if resp.StatusCode != http.StatusCreated {
			t.Errorf("expected 201, got %d", resp.StatusCode)
		}
	})

	t.Run("PUT", func(t *testing.T) {
		req, _ := http.NewRequest("PUT", ts.baseURL+"/api/tasks/1", strings.NewReader("title=Updated&description=Desc&status=done&priority=5"))
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		resp, err := client.Do(req)
		if err != nil {
			t.Fatalf("PUT failed: %v", err)
		}
		defer resp.Body.Close()

		if resp.StatusCode != http.StatusOK {
			body, _ := io.ReadAll(resp.Body)
			t.Errorf("expected 200, got %d: %s", resp.StatusCode, body)
		}
	})

	t.Run("PATCH", func(t *testing.T) {
		req, _ := http.NewRequest("PATCH", ts.baseURL+"/api/tasks/1", strings.NewReader("title=PartialUpdate"))
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		resp, err := client.Do(req)
		if err != nil {
			t.Fatalf("PATCH failed: %v", err)
		}
		defer resp.Body.Close()

		if resp.StatusCode != http.StatusOK {
			body, _ := io.ReadAll(resp.Body)
			t.Errorf("expected 200, got %d: %s", resp.StatusCode, body)
		}
	})

	t.Run("DELETE", func(t *testing.T) {
		// Create a task to delete
		resp, err := ts.post("/api/tasks", "title=ToDelete")
		if err != nil {
			t.Fatalf("create failed: %v", err)
		}
		var result map[string]any
		if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
			resp.Body.Close()
			t.Fatalf("decode create response: %v", err)
		}
		resp.Body.Close()
		idVal, ok := result["id"].(float64)
		if !ok {
			t.Fatalf("expected id to be float64, got %T", result["id"])
		}
		taskID := int(idVal)

		req, _ := http.NewRequest("DELETE", fmt.Sprintf("%s/api/tasks/%d", ts.baseURL, taskID), nil)
		resp, err = client.Do(req)
		if err != nil {
			t.Fatalf("DELETE failed: %v", err)
		}
		defer resp.Body.Close()

		if resp.StatusCode != http.StatusOK {
			t.Errorf("expected 200, got %d", resp.StatusCode)
		}
	})

	t.Run("HEAD", func(t *testing.T) {
		req, _ := http.NewRequest("HEAD", ts.baseURL+"/api/tasks", nil)
		resp, err := client.Do(req)
		if err != nil {
			t.Fatalf("HEAD failed: %v", err)
		}
		defer resp.Body.Close()

		if resp.StatusCode != http.StatusOK {
			t.Errorf("expected 200, got %d", resp.StatusCode)
		}

		totalCount := resp.Header.Get("X-Total-Count")
		if totalCount == "" {
			t.Error("expected X-Total-Count header")
		}
	})

	t.Run("OPTIONS", func(t *testing.T) {
		req, _ := http.NewRequest("OPTIONS", ts.baseURL+"/api/tasks", nil)
		resp, err := client.Do(req)
		if err != nil {
			t.Fatalf("OPTIONS failed: %v", err)
		}
		defer resp.Body.Close()

		if resp.StatusCode != http.StatusOK {
			t.Errorf("expected 200, got %d", resp.StatusCode)
		}

		allow := resp.Header.Get("Allow")
		if !strings.Contains(allow, "GET") || !strings.Contains(allow, "POST") {
			t.Errorf("expected Allow header with GET and POST, got %s", allow)
		}
	})
}

// TestTaskApp_TriggerCaching tests X-Cache header behavior
func TestTaskApp_TriggerCaching(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping e2e test in short mode")
	}

	binaryPath := buildBinary(t)
	port, err := findFreePort()
	if err != nil {
		t.Fatalf("failed to find free port: %v", err)
	}

	dbPath := filepath.Join(t.TempDir(), "taskapp.db")
	configPath := createTaskAppConfig(t, port, dbPath)

	ts := startServer(t, binaryPath, configPath, port)
	defer ts.stop()

	initTaskAppDB(t, ts)

	// First request should be MISS
	resp, err := ts.get("/api/tasks/1")
	if err != nil {
		t.Fatalf("first request failed: %v", err)
	}
	resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}

	cacheHeader := resp.Header.Get("X-Cache")
	if cacheHeader != "MISS" {
		t.Errorf("first request: expected X-Cache=MISS, got %q", cacheHeader)
	}

	// Second request should be HIT
	resp, err = ts.get("/api/tasks/1")
	if err != nil {
		t.Fatalf("second request failed: %v", err)
	}
	resp.Body.Close()

	cacheHeader = resp.Header.Get("X-Cache")
	if cacheHeader != "HIT" {
		t.Errorf("second request: expected X-Cache=HIT, got %q", cacheHeader)
	}
}

// TestTaskApp_RateLimiting tests 429 response when rate limit exceeded
func TestTaskApp_RateLimiting(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping e2e test in short mode")
	}

	binaryPath := buildBinary(t)
	port, err := findFreePort()
	if err != nil {
		t.Fatalf("failed to find free port: %v", err)
	}

	dbPath := filepath.Join(t.TempDir(), "taskapp.db")
	configPath := createTaskAppConfig(t, port, dbPath)

	ts := startServer(t, binaryPath, configPath, port)
	defer ts.stop()

	initTaskAppDB(t, ts)

	// Rate limit is 2 req/s with burst of 3
	// Send requests rapidly until we get 429
	got429 := false
	for i := 0; i < 10; i++ {
		resp, err := ts.post("/api/tasks", fmt.Sprintf("title=Task%d", i))
		if err != nil {
			t.Fatalf("request %d failed: %v", i, err)
		}
		resp.Body.Close()

		if resp.StatusCode == http.StatusTooManyRequests {
			got429 = true
			retryAfter := resp.Header.Get("Retry-After")
			if retryAfter == "" {
				t.Error("expected Retry-After header on 429 response")
			}
			break
		}
	}

	if !got429 {
		t.Error("expected to hit rate limit after rapid requests")
	}
}

// TestTaskApp_ConditionalResponses tests different response codes based on conditions
func TestTaskApp_ConditionalResponses(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping e2e test in short mode")
	}

	binaryPath := buildBinary(t)
	port, err := findFreePort()
	if err != nil {
		t.Fatalf("failed to find free port: %v", err)
	}

	dbPath := filepath.Join(t.TempDir(), "taskapp.db")
	configPath := createTaskAppConfig(t, port, dbPath)

	ts := startServer(t, binaryPath, configPath, port)
	defer ts.stop()

	initTaskAppDB(t, ts)

	t.Run("existing_task_returns_200", func(t *testing.T) {
		resp, err := ts.get("/api/tasks/1")
		if err != nil {
			t.Fatalf("request failed: %v", err)
		}
		resp.Body.Close()

		if resp.StatusCode != http.StatusOK {
			t.Errorf("expected 200, got %d", resp.StatusCode)
		}
	})

	t.Run("missing_task_returns_404", func(t *testing.T) {
		resp, err := ts.get("/api/tasks/99999")
		if err != nil {
			t.Fatalf("request failed: %v", err)
		}
		defer resp.Body.Close()

		if resp.StatusCode != http.StatusNotFound {
			t.Errorf("expected 404, got %d", resp.StatusCode)
		}

		var result map[string]any
		json.NewDecoder(resp.Body).Decode(&result)

		if result["error"] != "Task not found" {
			t.Errorf("expected error='Task not found', got %v", result["error"])
		}

		if result["id"].(float64) != 99999 {
			t.Errorf("expected id=99999, got %v", result["id"])
		}
	})

	t.Run("HEAD_existing_returns_200", func(t *testing.T) {
		client := &http.Client{}
		req, _ := http.NewRequest("HEAD", ts.baseURL+"/api/tasks/1", nil)
		resp, err := client.Do(req)
		if err != nil {
			t.Fatalf("request failed: %v", err)
		}
		resp.Body.Close()

		if resp.StatusCode != http.StatusOK {
			t.Errorf("expected 200, got %d", resp.StatusCode)
		}

		if resp.Header.Get("X-Exists") != "true" {
			t.Errorf("expected X-Exists=true, got %q", resp.Header.Get("X-Exists"))
		}
	})

	t.Run("HEAD_missing_returns_404", func(t *testing.T) {
		client := &http.Client{}
		req, _ := http.NewRequest("HEAD", ts.baseURL+"/api/tasks/99999", nil)
		resp, err := client.Do(req)
		if err != nil {
			t.Fatalf("request failed: %v", err)
		}
		resp.Body.Close()

		if resp.StatusCode != http.StatusNotFound {
			t.Errorf("expected 404, got %d", resp.StatusCode)
		}

		if resp.Header.Get("X-Exists") != "false" {
			t.Errorf("expected X-Exists=false, got %q", resp.Header.Get("X-Exists"))
		}
	})
}

// TestTaskApp_TemplateFunctions tests template functions in responses
func TestTaskApp_TemplateFunctions(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping e2e test in short mode")
	}

	binaryPath := buildBinary(t)
	port, err := findFreePort()
	if err != nil {
		t.Fatalf("failed to find free port: %v", err)
	}

	dbPath := filepath.Join(t.TempDir(), "taskapp.db")
	configPath := createTaskAppConfig(t, port, dbPath)

	ts := startServer(t, binaryPath, configPath, port)
	defer ts.stop()

	initTaskAppDB(t, ts)

	t.Run("upper_function_in_PUT_response", func(t *testing.T) {
		client := &http.Client{}
		req, _ := http.NewRequest("PUT", ts.baseURL+"/api/tasks/1",
			strings.NewReader("title=lowercase&description=test&status=done&priority=5"))
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		resp, err := client.Do(req)
		if err != nil {
			t.Fatalf("request failed: %v", err)
		}
		defer resp.Body.Close()

		var result map[string]any
		json.NewDecoder(resp.Body).Decode(&result)

		// Template uses {{.trigger.params.title | upper}}
		if result["title"] != "LOWERCASE" {
			t.Errorf("expected title='LOWERCASE', got %v", result["title"])
		}
	})

	t.Run("lower_and_trim_in_search", func(t *testing.T) {
		var result map[string]any
		resp, err := ts.getJSON("/api/search/tasks?q=+REVIEW+", &result)
		if err != nil {
			t.Fatalf("request failed: %v", err)
		}

		if resp.StatusCode != http.StatusOK {
			t.Errorf("expected 200, got %d", resp.StatusCode)
		}

		// Template uses {{.trigger.params.q | lower | trim}}
		// Input was "+REVIEW+" so output should be "review"
		if result["query"] != "review" {
			t.Errorf("expected query='review', got %v", result["query"])
		}
	})

	t.Run("default_function_in_stats", func(t *testing.T) {
		var result map[string]any
		resp, err := ts.getJSON("/api/stats", &result)
		if err != nil {
			t.Fatalf("request failed: %v", err)
		}

		if resp.StatusCode != http.StatusOK {
			t.Errorf("expected 200, got %d", resp.StatusCode)
		}

		// avg_priority uses default function
		if result["avg_priority"] == nil {
			t.Error("expected avg_priority field")
		}
	})
}

// TestTaskApp_BatchOperations tests block iteration for batch create/delete
func TestTaskApp_BatchOperations(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping e2e test in short mode")
	}

	binaryPath := buildBinary(t)
	port, err := findFreePort()
	if err != nil {
		t.Fatalf("failed to find free port: %v", err)
	}

	dbPath := filepath.Join(t.TempDir(), "taskapp.db")
	configPath := createTaskAppConfig(t, port, dbPath)

	ts := startServer(t, binaryPath, configPath, port)
	defer ts.stop()

	initTaskAppDB(t, ts)

	t.Run("batch_create_with_json", func(t *testing.T) {
		tasks := map[string]any{
			"tasks": []map[string]any{
				{"title": "Batch1", "priority": 1},
				{"title": "Batch2", "priority": 2},
				{"title": "Batch3", "priority": 3},
			},
		}
		jsonData, _ := json.Marshal(tasks)

		req, _ := http.NewRequest("POST", ts.baseURL+"/api/tasks/batch", bytes.NewReader(jsonData))
		req.Header.Set("Content-Type", "application/json")

		client := &http.Client{}
		resp, err := client.Do(req)
		if err != nil {
			t.Fatalf("request failed: %v", err)
		}
		defer resp.Body.Close()

		if resp.StatusCode != http.StatusCreated {
			body, _ := io.ReadAll(resp.Body)
			t.Fatalf("expected 201, got %d: %s", resp.StatusCode, body)
		}

		var result map[string]any
		if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
			t.Fatalf("decode batch create response: %v", err)
		}

		if result["success"] != true {
			t.Errorf("expected success=true, got %v", result)
		}

		created, ok := result["created"].(float64)
		if !ok {
			t.Fatalf("expected created to be float64, got %T", result["created"])
		}
		if int(created) != 3 {
			t.Errorf("expected created=3, got %d", int(created))
		}
	})

	t.Run("batch_delete_with_array", func(t *testing.T) {
		// Create tasks to delete
		var ids []int
		for i := 0; i < 3; i++ {
			resp, err := ts.post("/api/tasks", fmt.Sprintf("title=ToDelete%d", i))
			if err != nil {
				t.Fatalf("create task %d failed: %v", i, err)
			}
			var result map[string]any
			if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
				resp.Body.Close()
				t.Fatalf("decode create response: %v", err)
			}
			resp.Body.Close()
			idVal, ok := result["id"].(float64)
			if !ok {
				t.Fatalf("expected id to be float64, got %T", result["id"])
			}
			ids = append(ids, int(idVal))
		}

		// Batch delete
		idsJSON, _ := json.Marshal(ids)
		req, _ := http.NewRequest("DELETE", ts.baseURL+"/api/tasks/batch",
			bytes.NewReader([]byte(fmt.Sprintf(`{"ids":%s}`, idsJSON))))
		req.Header.Set("Content-Type", "application/json")

		client := &http.Client{}
		resp, err := client.Do(req)
		if err != nil {
			t.Fatalf("request failed: %v", err)
		}
		defer resp.Body.Close()

		if resp.StatusCode != http.StatusOK {
			body, _ := io.ReadAll(resp.Body)
			t.Fatalf("expected 200, got %d: %s", resp.StatusCode, body)
		}

		var result map[string]any
		if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
			t.Fatalf("decode batch delete response: %v", err)
		}

		deleted, ok := result["deleted"].(float64)
		if !ok {
			t.Fatalf("expected deleted to be float64, got %T", result["deleted"])
		}
		if int(deleted) != 3 {
			t.Errorf("expected deleted=3, got %d", int(deleted))
		}
	})
}

// TestTaskApp_StepCaching tests step-level cache behavior
func TestTaskApp_StepCaching(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping e2e test in short mode")
	}

	binaryPath := buildBinary(t)
	port, err := findFreePort()
	if err != nil {
		t.Fatalf("failed to find free port: %v", err)
	}

	dbPath := filepath.Join(t.TempDir(), "taskapp.db")
	configPath := createTaskAppConfig(t, port, dbPath)

	ts := startServer(t, binaryPath, configPath, port)
	defer ts.stop()

	initTaskAppDB(t, ts)

	// First request - cache miss
	var result1 map[string]any
	resp, err := ts.getJSON("/api/stats", &result1)
	if err != nil {
		t.Fatalf("first request failed: %v", err)
	}
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}

	// Response includes counts_cached field showing cache status
	if result1["counts_cached"] != false {
		t.Errorf("first request: expected counts_cached=false, got %v", result1["counts_cached"])
	}

	// Second request - cache hit
	var result2 map[string]any
	resp, err = ts.getJSON("/api/stats", &result2)
	if err != nil {
		t.Fatalf("second request failed: %v", err)
	}

	if result2["counts_cached"] != true {
		t.Errorf("second request: expected counts_cached=true, got %v", result2["counts_cached"])
	}
}

// TestTaskApp_Pagination tests pagination parameters
func TestTaskApp_Pagination(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping e2e test in short mode")
	}

	binaryPath := buildBinary(t)
	port, err := findFreePort()
	if err != nil {
		t.Fatalf("failed to find free port: %v", err)
	}

	dbPath := filepath.Join(t.TempDir(), "taskapp.db")
	configPath := createTaskAppConfig(t, port, dbPath)

	ts := startServer(t, binaryPath, configPath, port)
	defer ts.stop()

	initTaskAppDB(t, ts)

	t.Run("default_pagination", func(t *testing.T) {
		var result map[string]any
		resp, err := ts.getJSON("/api/tasks", &result)
		if err != nil {
			t.Fatalf("request failed: %v", err)
		}
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("expected 200, got %d", resp.StatusCode)
		}

		// Default limit is 10, page is 1
		if result["limit"].(float64) != 10 {
			t.Errorf("expected limit=10, got %v", result["limit"])
		}
		if result["page"].(float64) != 1 {
			t.Errorf("expected page=1, got %v", result["page"])
		}
	})

	t.Run("custom_pagination", func(t *testing.T) {
		var result map[string]any
		resp, err := ts.getJSON("/api/tasks?limit=2&page=2", &result)
		if err != nil {
			t.Fatalf("request failed: %v", err)
		}
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("expected 200, got %d", resp.StatusCode)
		}

		if result["limit"].(float64) != 2 {
			t.Errorf("expected limit=2, got %v", result["limit"])
		}
		if result["page"].(float64) != 2 {
			t.Errorf("expected page=2, got %v", result["page"])
		}
	})
}

// TestTaskApp_Filtering tests status and priority filters
func TestTaskApp_Filtering(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping e2e test in short mode")
	}

	binaryPath := buildBinary(t)
	port, err := findFreePort()
	if err != nil {
		t.Fatalf("failed to find free port: %v", err)
	}

	dbPath := filepath.Join(t.TempDir(), "taskapp.db")
	configPath := createTaskAppConfig(t, port, dbPath)

	ts := startServer(t, binaryPath, configPath, port)
	defer ts.stop()

	initTaskAppDB(t, ts)

	t.Run("filter_by_status", func(t *testing.T) {
		var result map[string]any
		resp, err := ts.getJSON("/api/tasks?status=pending", &result)
		if err != nil {
			t.Fatalf("request failed: %v", err)
		}
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("expected 200, got %d", resp.StatusCode)
		}

		tasks := result["tasks"].([]any)
		for _, item := range tasks {
			task := item.(map[string]any)
			if task["status"] != "pending" {
				t.Errorf("expected status=pending, got %v", task["status"])
			}
		}
	})

	t.Run("filter_by_priority", func(t *testing.T) {
		var result map[string]any
		resp, err := ts.getJSON("/api/tasks?priority=3", &result)
		if err != nil {
			t.Fatalf("request failed: %v", err)
		}
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("expected 200, got %d", resp.StatusCode)
		}

		tasks := result["tasks"].([]any)
		for _, tk := range tasks {
			task := tk.(map[string]any)
			if task["priority"].(float64) != 3 {
				t.Errorf("expected priority=3, got %v", task["priority"])
			}
		}
	})
}

// TestTaskApp_Categories tests secondary entity CRUD
func TestTaskApp_Categories(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping e2e test in short mode")
	}

	binaryPath := buildBinary(t)
	port, err := findFreePort()
	if err != nil {
		t.Fatalf("failed to find free port: %v", err)
	}

	dbPath := filepath.Join(t.TempDir(), "taskapp.db")
	configPath := createTaskAppConfig(t, port, dbPath)

	ts := startServer(t, binaryPath, configPath, port)
	defer ts.stop()

	initTaskAppDB(t, ts)

	t.Run("list_categories", func(t *testing.T) {
		var result map[string]any
		resp, err := ts.getJSON("/api/categories", &result)
		if err != nil {
			t.Fatalf("request failed: %v", err)
		}
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("expected 200, got %d", resp.StatusCode)
		}

		// Seed data has 3 categories
		if result["count"].(float64) != 3 {
			t.Errorf("expected 3 categories, got %v", result["count"])
		}
	})

	t.Run("get_single_category", func(t *testing.T) {
		var result map[string]any
		resp, err := ts.getJSON("/api/categories/1", &result)
		if err != nil {
			t.Fatalf("request failed: %v", err)
		}
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("expected 200, got %d", resp.StatusCode)
		}

		cat := result["category"].(map[string]any)
		if cat["name"] != "Work" {
			t.Errorf("expected name='Work', got %v", cat["name"])
		}
	})

	t.Run("category_not_found", func(t *testing.T) {
		var result map[string]any
		resp, err := ts.getJSON("/api/categories/999", &result)
		if err != nil {
			t.Fatalf("request failed: %v", err)
		}
		if resp.StatusCode != http.StatusNotFound {
			t.Errorf("expected 404, got %d", resp.StatusCode)
		}
	})
}

// TestTaskApp_CompleteTask tests the complete workflow with disabled audit step
func TestTaskApp_CompleteTask(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping e2e test in short mode")
	}

	binaryPath := buildBinary(t)
	port, err := findFreePort()
	if err != nil {
		t.Fatalf("failed to find free port: %v", err)
	}

	dbPath := filepath.Join(t.TempDir(), "taskapp.db")
	configPath := createTaskAppConfig(t, port, dbPath)

	ts := startServer(t, binaryPath, configPath, port)
	defer ts.stop()

	initTaskAppDB(t, ts)

	// Complete task 1 (which starts as 'pending')
	resp, err := ts.post("/api/tasks/1/complete", "")
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		t.Fatalf("expected 200, got %d: %s", resp.StatusCode, body)
	}

	var result map[string]any
	json.NewDecoder(resp.Body).Decode(&result)

	if result["success"] != true {
		t.Errorf("expected success=true, got %v", result)
	}

	if result["previous_status"] != "pending" {
		t.Errorf("expected previous_status='pending', got %v", result["previous_status"])
	}

	if result["new_status"] != "completed" {
		t.Errorf("expected new_status='completed', got %v", result["new_status"])
	}

	// Verify the task is actually completed
	var taskResult map[string]any
	ts.getJSON("/api/tasks/1", &taskResult)
	task := taskResult["task"].(map[string]any)
	if task["status"] != "completed" {
		t.Errorf("expected task status='completed', got %v", task["status"])
	}
}
