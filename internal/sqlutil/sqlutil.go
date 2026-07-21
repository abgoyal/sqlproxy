package sqlutil

import (
	"regexp"
	"slices"
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
			for i < len(sql) {
				if i+1 < len(sql) && sql[i] == '*' && sql[i+1] == '/' {
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

		// Backtick-quoted identifier: `...` with `` escape (MySQL, SQLite)
		if ch == '`' {
			buf.WriteByte(' ')
			i++
			for i < len(sql) {
				if sql[i] == '`' {
					i++
					if i < len(sql) && sql[i] == '`' {
						i++ // escaped ``
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

	return slices.Contains(writeKeywords, fields[0])
}

// procedureKeywords invoke stored procedures or user code, which may write.
var procedureKeywords = []string{"EXEC", "EXECUTE", "CALL"}

// mutatingKeywords are every keyword that can modify the database. Permission
// checking is a superset of execution routing: routing asks whether a statement
// returns rows and therefore excludes procedure calls, which may do either.
var mutatingKeywords = slices.Concat(writeKeywords, procedureKeywords)

// RequiresWriteAccess reports whether the SQL could modify the database.
//
// This is a different question from IsWriteQuery, which classifies a statement for
// execution routing. Routing must know whether rows come back; permission must know
// whether anything can change. A procedure call needs write access but may still
// return rows, so the two questions must not share an answer.
//
// The check is deliberately conservative, and answers "is this provably a read"
// rather than "where is the write". Locating the operative keyword by position is
// not reliable across CTEs, T-SQL control flow, and multi-statement batches, and
// every construct missed silently grants write access to a read-only connection.
// So a mutating keyword appearing anywhere outside a string literal, comment, or
// quoted identifier requires write access. All of these words are reserved in SQL,
// so a genuine read-only statement has no legitimate reason to contain one.
func RequiresWriteAccess(sql string) bool {
	upper := strings.ToUpper(stripLiterals(sql))

	for i := 0; i < len(upper); {
		if !isIdentChar(upper[i]) {
			i++
			continue
		}
		end := i
		for end < len(upper) && isIdentChar(upper[end]) {
			end++
		}
		if slices.Contains(mutatingKeywords, upper[i:end]) {
			return true
		}
		i = end
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
