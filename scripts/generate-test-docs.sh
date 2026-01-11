#!/bin/bash
# Generate test documentation from test source files
# This script extracts test function names and their comments to create documentation

set -e

OUTPUT_FILE="TESTS.md"
PROJECT_ROOT="$(cd "$(dirname "$0")/.." && pwd)"

cd "$PROJECT_ROOT"

echo "Generating test documentation..."

cat > "$OUTPUT_FILE" << 'HEADER'
# Test Documentation

This document is auto-generated from test source files. Run `make test-docs` to regenerate.

## Coverage Summary

Run `make test-cover` for current coverage statistics.

HEADER

# Function to extract and format test info from a Go test file
extract_tests() {
    local file="$1"
    local pkg="$2"

    # Extract test functions and any preceding comment
    awk '
    /^\/\/ Test/ { comment = substr($0, 4); next }
    /^\/\/ Benchmark/ { comment = substr($0, 4); next }
    /^func Test[A-Za-z0-9_]+\(/ {
        match($0, /func (Test[A-Za-z0-9_]+)/, arr)
        name = arr[1]
        if (comment != "") {
            print "- **" name "**: " comment
            comment = ""
        } else {
            # Generate description from test name
            desc = name
            gsub(/^Test/, "", desc)
            gsub(/_/, " ", desc)
            print "- **" name "**: " desc
        }
    }
    /^func Benchmark[A-Za-z0-9_]+\(/ {
        match($0, /func (Benchmark[A-Za-z0-9_]+)/, arr)
        name = arr[1]
        if (comment != "") {
            print "- **" name "**: " comment
            comment = ""
        } else {
            desc = name
            gsub(/^Benchmark/, "", desc)
            gsub(/_/, " ", desc)
            print "- **" name "**: Benchmark " desc
        }
    }
    ' "$file"
}

# Process each package
process_package() {
    local pkg_path="$1"
    local pkg_name="$2"
    local test_files=$(find "$pkg_path" -name "*_test.go" 2>/dev/null | sort)

    if [ -z "$test_files" ]; then
        return
    fi

    echo "" >> "$OUTPUT_FILE"
    echo "---" >> "$OUTPUT_FILE"
    echo "" >> "$OUTPUT_FILE"
    echo "## $pkg_name" >> "$OUTPUT_FILE"
    echo "" >> "$OUTPUT_FILE"
    echo "**Package**: \`$pkg_path\`" >> "$OUTPUT_FILE"
    echo "" >> "$OUTPUT_FILE"

    for file in $test_files; do
        local basename=$(basename "$file")
        echo "### $basename" >> "$OUTPUT_FILE"
        echo "" >> "$OUTPUT_FILE"
        extract_tests "$file" "$pkg_name" >> "$OUTPUT_FILE"
        echo "" >> "$OUTPUT_FILE"
    done
}

# Process all test packages
process_package "internal/config" "Config"
process_package "internal/db" "Database"
process_package "internal/handler" "Handler"
process_package "internal/scheduler" "Scheduler"
process_package "internal/validate" "Validation"
process_package "internal/server" "Server"
process_package "internal/logging" "Logging"
process_package "internal/metrics" "Metrics"
process_package "internal/openapi" "OpenAPI"
process_package "internal/service" "Service"
process_package "internal/webhook" "Webhook"
process_package "internal/cache" "Cache"
process_package "internal/tmpl" "Template Engine"
process_package "internal/ratelimit" "Rate Limiting"
process_package "e2e" "End-to-End"

# Add footer
cat >> "$OUTPUT_FILE" << 'FOOTER'

---

## Running Tests

```bash
# Run all tests
make test

# Run by test type
make test-unit         # Unit tests (internal packages)
make test-integration  # Integration tests (httptest-based)
make test-e2e          # End-to-end tests (starts actual binary)

# Run by package
make test-db
make test-handler
make test-tmpl
make test-ratelimit
# etc.

# Run with coverage
make test-cover
make test-cover-html

# Run benchmarks
make test-bench
```

## Test Organization

| Type | Location | Description |
|------|----------|-------------|
| Unit tests | `internal/*/` | Test individual functions and methods |
| Integration tests | `internal/server/` | Test component interactions via `httptest` |
| End-to-end tests | `e2e/` | Start binary, make real HTTP requests |
| Benchmarks | `internal/*/benchmark_test.go` | Performance tests |

All unit and integration tests use SQLite in-memory databases to avoid external dependencies.
FOOTER

echo "Generated: $OUTPUT_FILE"
