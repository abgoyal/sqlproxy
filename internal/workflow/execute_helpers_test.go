package workflow

import (
	"testing"
)

func TestExtractSQLParams(t *testing.T) {
	t.Run("extract from trigger.params", func(t *testing.T) {
		sql := "SELECT * FROM users WHERE status = @status AND id = @id"
		data := map[string]any{
			"trigger": map[string]any{
				"params": map[string]any{
					"status": "active",
					"id":     42,
				},
			},
		}

		params := extractSQLParams(sql, data)

		if params["status"] != "active" {
			t.Errorf("status = %v, want active", params["status"])
		}
		if params["id"] != 42 {
			t.Errorf("id = %v, want 42", params["id"])
		}
	})

	t.Run("extract from direct data", func(t *testing.T) {
		sql := "SELECT * FROM items WHERE item_id = @item_id"
		data := map[string]any{
			"item_id": 123,
		}

		params := extractSQLParams(sql, data)

		if params["item_id"] != 123 {
			t.Errorf("item_id = %v, want 123", params["item_id"])
		}
	})

	t.Run("step params take highest precedence", func(t *testing.T) {
		sql := "SELECT * FROM users WHERE id = @id"
		data := map[string]any{
			"id": 999,
			"params": map[string]any{
				"id": 1,
			},
			"trigger": map[string]any{
				"params": map[string]any{
					"id": 42,
				},
			},
		}

		params := extractSQLParams(sql, data)

		if params["id"] != 1 {
			t.Errorf("id = %v, want 1 (from step params)", params["id"])
		}
	})

	t.Run("trigger.params takes precedence over direct data", func(t *testing.T) {
		sql := "SELECT * FROM users WHERE id = @id"
		data := map[string]any{
			"id": 999,
			"trigger": map[string]any{
				"params": map[string]any{
					"id": 42,
				},
			},
		}

		params := extractSQLParams(sql, data)

		if params["id"] != 42 {
			t.Errorf("id = %v, want 42 (from trigger.params)", params["id"])
		}
	})

	t.Run("no params found", func(t *testing.T) {
		sql := "SELECT * FROM users WHERE status = @status"
		data := map[string]any{}

		params := extractSQLParams(sql, data)

		if _, ok := params["status"]; ok {
			t.Errorf("status should not be in params")
		}
	})

	t.Run("extract from nested map values (iteration)", func(t *testing.T) {
		sql := "INSERT INTO items (title) VALUES (@title)"
		data := map[string]any{
			"task": map[string]any{
				"title": "Buy milk",
				"id":    5,
			},
		}

		params := extractSQLParams(sql, data)

		if params["title"] != "Buy milk" {
			t.Errorf("title = %v, want 'Buy milk'", params["title"])
		}
	})
}

func TestNormalizeJSONResponse(t *testing.T) {
	t.Run("array of maps", func(t *testing.T) {
		input := []any{
			map[string]any{"id": 1, "name": "Alice"},
			map[string]any{"id": 2, "name": "Bob"},
		}
		result := normalizeJSONResponse(input)

		if len(result) != 2 {
			t.Errorf("len(result) = %d, want 2", len(result))
		}
		if result[0]["name"] != "Alice" {
			t.Errorf("result[0][name] = %v, want Alice", result[0]["name"])
		}
	})

	t.Run("array of non-maps", func(t *testing.T) {
		input := []any{1, 2, 3}
		result := normalizeJSONResponse(input)

		if len(result) != 3 {
			t.Errorf("len(result) = %d, want 3", len(result))
		}
		if result[0]["value"] != 1 {
			t.Errorf("result[0][value] = %v, want 1", result[0]["value"])
		}
	})

	t.Run("single map", func(t *testing.T) {
		input := map[string]any{"id": 1, "name": "Alice"}
		result := normalizeJSONResponse(input)

		if len(result) != 1 {
			t.Errorf("len(result) = %d, want 1", len(result))
		}
		if result[0]["name"] != "Alice" {
			t.Errorf("result[0][name] = %v, want Alice", result[0]["name"])
		}
	})

	t.Run("scalar value", func(t *testing.T) {
		input := "hello"
		result := normalizeJSONResponse(input)

		if len(result) != 1 {
			t.Errorf("len(result) = %d, want 1", len(result))
		}
		if result[0]["value"] != "hello" {
			t.Errorf("result[0][value] = %v, want hello", result[0]["value"])
		}
	})
}
