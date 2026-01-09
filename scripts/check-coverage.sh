#!/bin/bash
# Check if coverage meets minimum threshold
# Usage: ./check-coverage.sh <coverage.out> <threshold>

set -e

COVERAGE_FILE="${1:-coverage/coverage.out}"
THRESHOLD="${2:-70}"

if [ ! -f "$COVERAGE_FILE" ]; then
    echo "Error: Coverage file not found: $COVERAGE_FILE"
    echo "Run 'make test-cover-report' first"
    exit 1
fi

# Extract total coverage percentage
TOTAL=$(go tool cover -func="$COVERAGE_FILE" | grep "^total:" | awk '{print $3}' | sed 's/%//')

if [ -z "$TOTAL" ]; then
    echo "Error: Could not extract coverage percentage"
    exit 1
fi

# Compare using bc for floating point
PASS=$(echo "$TOTAL >= $THRESHOLD" | bc -l)

echo "============================================"
echo "Coverage Check"
echo "============================================"
echo "Total coverage: ${TOTAL}%"
echo "Threshold:      ${THRESHOLD}%"
echo "============================================"

if [ "$PASS" -eq 1 ]; then
    echo "✓ PASS: Coverage meets threshold"
    exit 0
else
    echo "✗ FAIL: Coverage below threshold"
    exit 1
fi
