package sqlutil

import "testing"

// TestStripLiterals verifies comment and string removal for safe keyword detection
func TestStripLiterals(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  string
	}{
		{"plain SQL", "SELECT * FROM users", "SELECT * FROM users"},
		{"single-line comment", "SELECT 1 -- this is a comment", "SELECT 1  "},
		{"multi-line comment", "SELECT /* comment */ 1", "SELECT   1"},
		{"single-quoted string", "SELECT * FROM t WHERE name = 'INSERT INTO'", "SELECT * FROM t WHERE name =  "},
		{"escaped quote", "SELECT 'it''s fine'", "SELECT  "},
		{"double-quoted identifier", `SELECT "DELETE" FROM t`, "SELECT   FROM t"},
		{"bracket identifier", "SELECT [OUTPUT INSERTED] FROM t", "SELECT   FROM t"},
		{"string with RETURNING", "INSERT INTO t (msg) VALUES ('returning item')", "INSERT INTO t (msg) VALUES ( )"},
		{"comment with keyword", "-- INSERT INTO evil\nSELECT 1", " \nSELECT 1"},
		{"mixed", "INSERT INTO t /* comment */ VALUES ('test') -- done", "INSERT INTO t   VALUES ( )  "},
		{"backtick identifier", "SELECT `DELETE` FROM t", "SELECT   FROM t"},
		{"escaped backtick", "SELECT `a``b` FROM t", "SELECT   FROM t"},
		{"backtick table", "SELECT * FROM `orders`", "SELECT * FROM  "},
		{"unterminated string", "SELECT 1 FROM t WHERE x = 'a", "SELECT 1 FROM t WHERE x =  "},
		{"unterminated block comment", "SELECT 1 /* unterminated", "SELECT 1  "},
		{"unterminated backtick", "SELECT `abc", "SELECT  "},
		{"unterminated bracket", "SELECT [abc", "SELECT  "},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := stripLiterals(tt.input)
			if got != tt.want {
				t.Errorf("stripLiterals(%q)\n  got:  %q\n  want: %q", tt.input, got, tt.want)
			}
		})
	}
}

// TestIsWriteQuery verifies statement type detection with literal-aware parsing
func TestIsWriteQuery(t *testing.T) {
	tests := []struct {
		name string
		sql  string
		want bool
	}{
		// Basic statements
		{"select", "SELECT * FROM users", false},
		{"insert", "INSERT INTO users (name) VALUES ('x')", true},
		{"update", "UPDATE users SET name = 'x'", true},
		{"delete", "DELETE FROM users WHERE id = 1", true},
		{"create table", "CREATE TABLE t (id INT)", true},
		{"drop table", "DROP TABLE t", true},
		{"alter table", "ALTER TABLE t ADD col INT", true},
		{"truncate", "TRUNCATE TABLE t", true},
		{"merge", "MERGE INTO t USING s ON t.id = s.id WHEN MATCHED THEN UPDATE SET t.x = s.x", true},
		{"empty", "", false},

		// CTE handling
		{"cte with select", "WITH cte AS (SELECT 1) SELECT * FROM cte", false},
		{"cte with insert", "WITH cte AS (SELECT 1) INSERT INTO t SELECT * FROM cte", true},
		{"cte with delete", "WITH cte AS (SELECT id FROM t) DELETE FROM t WHERE id IN (SELECT id FROM cte)", true},

		// Keywords inside strings (should NOT match)
		{"insert in string", "SELECT * FROM t WHERE msg = 'INSERT INTO evil'", false},
		{"delete in string", "SELECT * FROM t WHERE name = 'DELETE ME'", false},
		{"update in comment", "SELECT 1 -- UPDATE users SET x = 1", false},
		{"create in multi-comment", "SELECT /* CREATE TABLE t */ 1", false},

		// Whitespace and case
		{"leading whitespace", "  INSERT INTO t VALUES (1)", true},
		{"lowercase", "insert into t values (1)", true},
		{"mixed case", "Insert Into t Values (1)", true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := IsWriteQuery(tt.sql)
			if got != tt.want {
				t.Errorf("IsWriteQuery(%q) = %v, want %v", tt.sql, got, tt.want)
			}
		})
	}
}

// TestRequiresWriteAccess verifies write-permission detection including stored procedure calls
func TestRequiresWriteAccess(t *testing.T) {
	tests := []struct {
		name string
		sql  string
		want bool
	}{
		// Plain reads
		{"select", "SELECT * FROM users", false},
		{"empty", "", false},
		{"read only batch", "SELECT 1; SELECT 2", false},
		{"trailing semicolon on read", "SELECT * FROM users;", false},

		// Plain writes
		{"insert", "INSERT INTO users (name) VALUES ('x')", true},
		{"merge", "MERGE INTO t USING s ON t.id = s.id WHEN MATCHED THEN UPDATE SET t.x = s.x", true},
		{"lowercase", "insert into t values (1)", true},
		{"leading whitespace", "  INSERT INTO t VALUES (1)", true},

		// Stored procedures: may write, so they need write access
		{"exec", "EXEC sp_rebuild_index", true},
		{"execute", "EXECUTE sp_rebuild_index", true},
		{"call", "CALL rebuild_index()", true},

		// Batches: a write anywhere in the batch needs write access
		{"select then drop", "SELECT 1; DROP TABLE users", true},
		{"newline separated batch", "SELECT 1;\nUPDATE t SET x = 1", true},
		{"set nocount then insert", "SET NOCOUNT ON; INSERT INTO t VALUES (1)", true},
		{"explicit transaction", "BEGIN; INSERT INTO t VALUES (1); COMMIT", true},
		{"select then exec", "SELECT 1; EXEC sp_rebuild_index", true},

		// CTEs, including positions a first-token check cannot see
		{"cte with select", "WITH c AS (SELECT 1) SELECT * FROM c", false},
		{"cte with insert", "WITH c AS (SELECT 1) INSERT INTO t SELECT * FROM c", true},
		{"leading semicolon cte", ";WITH c AS (SELECT 1) INSERT INTO t SELECT * FROM c", true},
		{"cte inside batch", "SELECT 1; WITH c AS (SELECT 1) INSERT INTO t SELECT * FROM c", true},

		// T-SQL control flow wrapping a write
		{"if begin insert end", "IF EXISTS (SELECT 1 FROM t) BEGIN INSERT INTO t VALUES (1) END", true},
		{"while begin delete end", "WHILE (1=1) BEGIN DELETE FROM t END", true},
		{"if exec", "IF (1=1) EXEC sp_rebuild", true},

		// Keywords inside literals, comments and quoted identifiers are not keywords
		{"delete in string", "SELECT * FROM t WHERE name = 'DELETE ME'", false},
		{"exec in string", "SELECT * FROM t WHERE cmd = 'EXEC sp_evil'", false},
		{"create in line comment", "SELECT id FROM t -- CREATE a report", false},
		{"update in block comment", "SELECT /* UPDATE users SET x = 1 */ id FROM t", false},
		{"semicolon inside string literal", "SELECT * FROM t WHERE s = 'a; DROP TABLE users'", false},
		{"bracket quoted identifier", "SELECT [insert] FROM t", false},
		{"double quoted identifier", "SELECT \"delete\" FROM t", false},
		{"backtick quoted identifier", "SELECT `insert` FROM t", false},
		{"backtick quoted table", "SELECT `update`, `delete` FROM `create`", false},
		{"write with backtick quoted table", "INSERT INTO `t` VALUES (1)", true},
		{"unterminated block comment", "SELECT 1 /* unterminated", false},

		// Backslash-escaped quotes are MySQL-specific and not portable. The tokenizer
		// assumes no dialect, so the string ends early and the remainder is read as
		// code. That fails closed - rejecting a valid read rather than missing a
		// write - which is the safe direction for a permission check. Use ANSI ''
		// doubling, which every supported database accepts.
		{"mysql backslash escaped quote fails closed", `SELECT * FROM t WHERE s = 'a\'b, INSERT INTO evil'`, true},
		{"ansi doubled quote", `SELECT * FROM t WHERE s = 'it''s fine, INSERT INTO evil'`, false},

		// Word boundaries: keywords must be whole tokens, not substrings
		{"execute as column prefix", "SELECT executed_at FROM jobs", false},
		{"call as table prefix", "SELECT * FROM call_log", false},
		{"create as column prefix", "SELECT create_date, updated_at FROM t", false},
		{"inserted pseudo table", "SELECT inserted.id FROM audit", false},
		{"deleted pseudo table", "SELECT deleted.id FROM audit", false},
		{"keyword as substring", "SELECT truncated, altered FROM t", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := RequiresWriteAccess(tt.sql)
			if got != tt.want {
				t.Errorf("RequiresWriteAccess(%q) = %v, want %v", tt.sql, got, tt.want)
			}
		})
	}
}

// TestHasReturningClause verifies OUTPUT/RETURNING detection with literal-awareness
func TestHasReturningClause(t *testing.T) {
	tests := []struct {
		name string
		sql  string
		want bool
	}{
		// SQL Server OUTPUT
		{"output inserted", "INSERT INTO t (name) OUTPUT INSERTED.* VALUES ('x')", true},
		{"output deleted", "DELETE FROM t OUTPUT DELETED.id WHERE id = 1", true},
		{"output inserted lowercase", "insert into t output inserted.* values ('x')", true},
		{"no output", "INSERT INTO t (name) VALUES ('x')", false},

		// PostgreSQL/SQLite RETURNING
		{"returning star", "INSERT INTO t (name) VALUES ('x') RETURNING *", true},
		{"returning columns", "DELETE FROM t WHERE id = 1 RETURNING id, name", true},
		{"returning lowercase", "insert into t values ('x') returning id", true},
		{"no returning", "INSERT INTO t (name) VALUES ('x')", false},

		// False positives that should NOT match
		{"returning in string", "INSERT INTO t (msg) VALUES ('returning item')", false},
		{"returning in comment", "INSERT INTO t VALUES (1) -- RETURNING *", false},
		{"returning_customers table", "SELECT * FROM returning_customers", false},
		{"not_returning column", "SELECT not_returning FROM t", false},
		{"output in string", "INSERT INTO t (msg) VALUES ('OUTPUT INSERTED')", false},
		{"output in bracket id", "SELECT [OUTPUT INSERTED] FROM t", false},

		// Word boundary checks
		{"returning prefix", "SELECT * FROM returningdata", false},
		{"returning suffix", "SELECT * FROM datareturning", false},

		// Plain selects
		{"select", "SELECT * FROM users", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := HasReturningClause(tt.sql)
			if got != tt.want {
				t.Errorf("HasReturningClause(%q) = %v, want %v", tt.sql, got, tt.want)
			}
		})
	}
}

// TestParamRegex verifies @param matching
func TestParamRegex(t *testing.T) {
	tests := []struct {
		name  string
		sql   string
		count int
	}{
		{"no params", "SELECT 1", 0},
		{"single param", "SELECT * FROM t WHERE id = @id", 1},
		{"multiple params", "SELECT * FROM t WHERE a = @a AND b = @b", 2},
		{"repeated param", "SELECT * FROM t WHERE @x IS NULL OR col = @x", 2},
		{"email in string", "SELECT * FROM t WHERE email = @email", 1},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			matches := ParamRegex.FindAllString(tt.sql, -1)
			if len(matches) != tt.count {
				t.Errorf("ParamRegex on %q found %d matches, want %d", tt.sql, len(matches), tt.count)
			}
		})
	}
}
