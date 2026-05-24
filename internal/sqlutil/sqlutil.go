package sqlutil

import (
	"regexp"
	"strings"
)

// ParamRegex matches @param style named parameters in SQL queries.
var ParamRegex = regexp.MustCompile(`@(\w+)`)

// stripLiterals removes string literals, comments, and quoted identifiers from SQL,
// replacing them with spaces. This allows keyword detection without false matches
// on content inside strings or comments.
func stripLiterals(sql string) string {
	var buf strings.Builder
	buf.Grow(len(sql))
	i := 0

	for i < len(sql) {
		ch := sql[i]

		// Single-line comment: -- to end of line
		if ch == '-' && i+1 < len(sql) && sql[i+1] == '-' {
			buf.WriteByte(' ')
			i += 2
			for i < len(sql) && sql[i] != '\n' {
				i++
			}
			continue
		}

		// Multi-line comment: /* ... */
		if ch == '/' && i+1 < len(sql) && sql[i+1] == '*' {
			buf.WriteByte(' ')
			i += 2
			for i+1 < len(sql) {
				if sql[i] == '*' && sql[i+1] == '/' {
					i += 2
					break
				}
				i++
			}
			continue
		}

		// Single-quoted string literal: '...' with '' escape
		if ch == '\'' {
			buf.WriteByte(' ')
			i++
			for i < len(sql) {
				if sql[i] == '\'' {
					i++
					if i < len(sql) && sql[i] == '\'' {
						i++ // escaped quote ''
					} else {
						break
					}
				} else {
					i++
				}
			}
			continue
		}

		// Double-quoted identifier: "..."
		if ch == '"' {
			buf.WriteByte(' ')
			i++
			for i < len(sql) {
				if sql[i] == '"' {
					i++
					if i < len(sql) && sql[i] == '"' {
						i++ // escaped ""
					} else {
						break
					}
				} else {
					i++
				}
			}
			continue
		}

		// Bracket-quoted identifier: [...] (T-SQL)
		if ch == '[' {
			buf.WriteByte(' ')
			i++
			for i < len(sql) && sql[i] != ']' {
				i++
			}
			if i < len(sql) {
				i++ // skip ]
			}
			continue
		}

		buf.WriteByte(ch)
		i++
	}

	return buf.String()
}

// writeKeywords are SQL keywords that indicate a write operation.
var writeKeywords = []string{"INSERT", "UPDATE", "DELETE", "CREATE", "DROP", "ALTER", "TRUNCATE", "MERGE"}

// IsWriteQuery returns true if the SQL is a write operation (INSERT, UPDATE, DELETE, etc.).
// Uses literal-aware parsing to avoid false matches on keywords inside strings or comments.
func IsWriteQuery(sql string) bool {
	stripped := stripLiterals(sql)
	upper := strings.ToUpper(strings.TrimSpace(stripped))
	if upper == "" {
		return false
	}

	// Handle CTEs: WITH ... AS (...) INSERT/UPDATE/DELETE
	if strings.HasPrefix(upper, "WITH ") {
		for _, kw := range writeKeywords {
			if strings.Contains(upper, kw+" ") || strings.HasSuffix(upper, kw) {
				return true
			}
		}
		return false
	}

	fields := strings.Fields(upper)
	if len(fields) == 0 {
		return false
	}

	for _, kw := range writeKeywords {
		if fields[0] == kw {
			return true
		}
	}
	return false
}

// HasReturningClause returns true if a write query contains OUTPUT INSERTED,
// OUTPUT DELETED (SQL Server), or RETURNING (PostgreSQL/SQLite 3.35+).
// These indicate the write statement returns rows that should be captured.
func HasReturningClause(sql string) bool {
	stripped := stripLiterals(sql)
	upper := strings.ToUpper(stripped)
	if strings.Contains(upper, "OUTPUT INSERTED") ||
		strings.Contains(upper, "OUTPUT DELETED") {
		return true
	}
	// Check for RETURNING as a standalone word (not part of an identifier)
	idx := strings.Index(upper, "RETURNING")
	if idx < 0 {
		return false
	}
	// Verify word boundary before
	if idx > 0 && isIdentChar(stripped[idx-1]) {
		return false
	}
	// Verify word boundary after
	end := idx + len("RETURNING")
	if end < len(stripped) && isIdentChar(stripped[end]) {
		return false
	}
	return true
}

func isIdentChar(c byte) bool {
	return (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') || (c >= '0' && c <= '9') || c == '_'
}
