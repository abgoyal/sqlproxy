#!/bin/bash
#
# Shared test helpers for sql-proxy E2E tests
#
# This file provides:
# - Output formatting (colors, info/success/fail/warn)
# - HTTP request wrappers (GET, POST, PUT, PATCH, DELETE, HEAD, OPTIONS)
# - Assertion helpers (expect_status, expect_json, expect_header, etc.)
#
# Usage:
#   source "$(dirname "$0")/lib/helpers.sh"
#
# Required variables (set before sourcing):
#   BASE_URL - Base URL for API requests (e.g., "http://127.0.0.1:8080")

# ============================================================================
# OUTPUT HELPERS
# ============================================================================

RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
BLUE='\033[0;34m'
NC='\033[0m'

info()    { echo -e "${BLUE}[INFO]${NC} $1"; }
success() { echo -e "${GREEN}[PASS]${NC} $1"; }
fail()    { echo -e "${RED}[FAIL]${NC} $1"; }
warn()    { echo -e "${YELLOW}[WARN]${NC} $1"; }
header()  { echo -e "\n${BLUE}=== $1 ===${NC}"; }

# Test counters
TESTS_RUN=0
TESTS_PASSED=0
TESTS_FAILED=0

# ============================================================================
# RATE LIMIT AND CACHE DETECTION
# ============================================================================
#
# These variables control expected behavior. Set them before making requests
# in tests that specifically test rate limiting or caching.
#
# EXPECT_RATE_LIMIT=true   - Don't warn on 429 responses
# EXPECT_CACHE_HIT=true    - Expect X-Cache: HIT
# EXPECT_CACHE_MISS=true   - Expect X-Cache: MISS
#
# After the request, call check_rate_limit_and_cache or let the wrapper do it.

EXPECT_RATE_LIMIT=false
EXPECT_CACHE_HIT=false
EXPECT_CACHE_MISS=false

# Track last seen values for debugging
_last_cache_status=""
_last_rate_limited=false

# _check_unexpected_behavior - Called after each request to detect issues
# This acts as a "canary" - it catches rate limiting or cache behavior
# that might indicate test pollution or ordering issues.
_check_unexpected_behavior() {
    local endpoint="$1"

    # Check for unexpected rate limiting
    if [ "$_status" = "429" ] && [ "$EXPECT_RATE_LIMIT" != "true" ]; then
        warn "UNEXPECTED RATE LIMIT at $endpoint"
        warn "  Response: $_response"
        _last_rate_limited=true
        # Don't fail the test, but make it very visible
    else
        _last_rate_limited=false
    fi

    # Extract and track cache status (|| true prevents exit on no match with set -e)
    _last_cache_status=$(echo "$_response_headers" | grep -i "^X-Cache:" | sed 's/.*: //' | tr -d '\r\n' || true)

    if [ -n "$_last_cache_status" ]; then
        # Check for unexpected cache behavior
        if [ "$EXPECT_CACHE_HIT" = "true" ] && [ "$_last_cache_status" != "HIT" ]; then
            warn "EXPECTED CACHE HIT but got $_last_cache_status at $endpoint"
        elif [ "$EXPECT_CACHE_MISS" = "true" ] && [ "$_last_cache_status" != "MISS" ]; then
            warn "EXPECTED CACHE MISS but got $_last_cache_status at $endpoint"
        fi
    fi
}

# reset_expectations - Reset rate limit and cache expectations to defaults
reset_expectations() {
    EXPECT_RATE_LIMIT=false
    EXPECT_CACHE_HIT=false
    EXPECT_CACHE_MISS=false
}

# ============================================================================
# SERVER STATE CONTROL
# ============================================================================
#
# These functions reset server-side state for test isolation.
# Use at the start of tests that need a clean slate.
#
# reset_rate_limits [pool]  - Reset rate limit buckets
# reset_cache [endpoint]    - Clear cached responses
# reset_server_state        - Reset both rate limits and cache
#

# reset_rate_limits [pool] - Reset rate limit buckets on server
# If pool is specified, only that pool is reset. Otherwise all pools.
reset_rate_limits() {
    local pool="${1:-}"
    local url="$BASE_URL/_/ratelimits/reset"
    [ -n "$pool" ] && url="$url?pool=$pool"

    local result
    result=$(curl -s -X POST "$url")
    local cleared
    cleared=$(echo "$result" | jq -r '.buckets_cleared // 0')

    if [ -n "$pool" ]; then
        info "Reset rate limits for pool '$pool' ($cleared buckets)"
    else
        info "Reset all rate limits ($cleared buckets)"
    fi
}

# reset_cache [endpoint] - Clear server cache
# If endpoint is specified, only that endpoint's cache is cleared.
reset_cache() {
    local endpoint="${1:-}"
    local url="$BASE_URL/_/cache/clear"
    [ -n "$endpoint" ] && url="$url?endpoint=$endpoint"

    curl -s -X POST "$url" > /dev/null

    if [ -n "$endpoint" ]; then
        info "Cleared cache for endpoint '$endpoint'"
    else
        info "Cleared all cache"
    fi
}

# reset_server_state - Reset both rate limits and cache
# Convenience function for full test isolation
reset_server_state() {
    reset_rate_limits
    reset_cache
}

# ============================================================================
# HTTP REQUEST HELPERS
# ============================================================================

# Request/response state (global, set by each request)
_response=""
_status=""
_response_headers=""

# Temp file counter for unique filenames
_temp_counter=0

# Create a temp file in TEMP_DIR (cleaned up automatically by runner.sh cleanup)
# Falls back to mktemp if TEMP_DIR not set (standalone usage)
_mktemp() {
    if [ -n "$TEMP_DIR" ] && [ -d "$TEMP_DIR" ]; then
        _temp_counter=$((_temp_counter + 1))
        echo "$TEMP_DIR/headers_$$_${_temp_counter}"
    else
        mktemp
    fi
}

# GET <path> [params or headers...]
# Headers format: "Header-Name: value" (contains colon)
# Query params format: "key=value" (contains = but no colon)
GET() {
    local path="$1"
    shift
    local header_args=()
    local query_params=""
    for h in "$@"; do
        case "$h" in
            *:*)
                # Header (contains colon)
                header_args+=("-H" "$h")
                ;;
            *=*)
                # Query param (contains = but no colon)
                if [ -n "$query_params" ]; then
                    query_params="$query_params&$h"
                else
                    query_params="$h"
                fi
                ;;
        esac
    done
    # Append query params to path
    local full_path="$path"
    if [ -n "$query_params" ]; then
        if [[ "$path" == *"?"* ]]; then
            full_path="$path&$query_params"
        else
            full_path="$path?$query_params"
        fi
    fi
    local _headers_file=$(_mktemp)
    trap "rm -f '$_headers_file'" RETURN
    _response=$(curl -s -D "$_headers_file" "${header_args[@]}" "$BASE_URL$full_path")
    # Extract HTTP status code from response headers (handles HTTP/1.1 and HTTP/2)
    # Uses tail -1 to get final status after any redirects
    _status=$(grep "^HTTP" "$_headers_file" | tail -1 | awk '{print $2}')
    _response_headers=$(cat "$_headers_file")
    _check_unexpected_behavior "GET $full_path"
}

# POST <path> [key=value ...] or POST <path> --json '<json>' [extra_headers...]
POST() {
    local path="$1"
    shift
    _do_request POST "$path" "$@"
}

# PUT <path> [key=value ...] or PUT <path> --json '<json>' [extra_headers...]
PUT() {
    local path="$1"
    shift
    _do_request PUT "$path" "$@"
}

# PATCH <path> [key=value ...] or PATCH <path> --json '<json>' [extra_headers...]
PATCH() {
    local path="$1"
    shift
    _do_request PATCH "$path" "$@"
}

# DELETE <path> [--json '<json>'] [extra_headers...]
DELETE() {
    local path="$1"
    shift
    _do_request DELETE "$path" "$@"
}

# HEAD <path> [extra_headers...]
HEAD() {
    local path="$1"
    shift
    local header_args=()
    for h in "$@"; do
        header_args+=("-H" "$h")
    done
    local _headers_file=$(_mktemp)
    trap "rm -f '$_headers_file'" RETURN
    curl -s -I -o "$_headers_file" -w "%{http_code}" "${header_args[@]}" "$BASE_URL$path" > /dev/null
    _status=$(grep "^HTTP" "$_headers_file" | tail -1 | awk '{print $2}')
    _response=""
    _response_headers=$(cat "$_headers_file")
    _check_unexpected_behavior "HEAD $path"
}

# OPTIONS <path> [extra_headers...]
OPTIONS() {
    local path="$1"
    shift
    local header_args=()
    for h in "$@"; do
        header_args+=("-H" "$h")
    done
    local _headers_file=$(_mktemp)
    trap "rm -f '$_headers_file'" RETURN
    _response=$(curl -s -D "$_headers_file" -X OPTIONS "${header_args[@]}" "$BASE_URL$path")
    _status=$(grep "^HTTP" "$_headers_file" | tail -1 | awk '{print $2}')
    _response_headers=$(cat "$_headers_file")
    _check_unexpected_behavior "OPTIONS $path"
}

# Internal: perform request with body
# _do_request <method> <path> [--json '<json>' | key=value ...] [extra_headers...]
_do_request() {
    local method="$1"
    local path="$2"
    shift 2

    local content_type="application/x-www-form-urlencoded"
    local data=""
    local extra_headers=()

    # Parse arguments
    while [ $# -gt 0 ]; do
        case "$1" in
            --json)
                content_type="application/json"
                data="$2"
                shift 2
                ;;
            *:*)
                # Extra header (contains colon)
                extra_headers+=("-H" "$1")
                shift
                ;;
            *=*)
                # Form data
                if [ -n "$data" ]; then
                    data="$data&$1"
                else
                    data="$1"
                fi
                shift
                ;;
            *)
                shift
                ;;
        esac
    done

    local _headers_file=$(mktemp)
    # Ensure temp file is cleaned up even if function is interrupted
    trap "rm -f '$_headers_file'" RETURN
    if [ -n "$data" ]; then
        _response=$(curl -s -D "$_headers_file" -X "$method" \
            -H "Content-Type: $content_type" \
            "${extra_headers[@]}" \
            -d "$data" \
            "$BASE_URL$path")
    else
        _response=$(curl -s -D "$_headers_file" -X "$method" "${extra_headers[@]}" "$BASE_URL$path")
    fi
    _status=$(grep "^HTTP" "$_headers_file" | tail -1 | awk '{print $2}')
    _response_headers=$(cat "$_headers_file")
    _check_unexpected_behavior "$method $path"
}

# ============================================================================
# ASSERTION HELPERS
# ============================================================================

# expect_status <code> <test_name>
expect_status() {
    local expected="$1"
    local test_name="$2"

    TESTS_RUN=$((TESTS_RUN + 1))
    if [ "$_status" = "$expected" ]; then
        success "$test_name"
        TESTS_PASSED=$((TESTS_PASSED + 1))
    else
        fail "$test_name"
        echo "  Expected status: $expected"
        echo "  Got: $_status"
        TESTS_FAILED=$((TESTS_FAILED + 1))
    fi
}

# expect_json <jq_path> <expected> <test_name>
expect_json() {
    local jq_path="$1"
    local expected="$2"
    local test_name="$3"

    TESTS_RUN=$((TESTS_RUN + 1))
    local actual=$(echo "$_response" | jq -r "$jq_path" 2>/dev/null)

    if [ "$actual" = "$expected" ]; then
        success "$test_name"
        TESTS_PASSED=$((TESTS_PASSED + 1))
    else
        fail "$test_name"
        echo "  Path: $jq_path"
        echo "  Expected: $expected"
        echo "  Got: $actual"
        TESTS_FAILED=$((TESTS_FAILED + 1))
    fi
}

# expect_contains <substring> <test_name>
expect_contains() {
    local expected="$1"
    local test_name="$2"

    TESTS_RUN=$((TESTS_RUN + 1))
    if echo "$_response" | grep -q "$expected"; then
        success "$test_name"
        TESTS_PASSED=$((TESTS_PASSED + 1))
    else
        fail "$test_name"
        echo "  Expected to contain: $expected"
        echo "  Got: $_response"
        TESTS_FAILED=$((TESTS_FAILED + 1))
    fi
}

# expect_header <header_name> <expected_value> <test_name>
expect_header() {
    local header="$1"
    local expected="$2"
    local test_name="$3"

    TESTS_RUN=$((TESTS_RUN + 1))
    local actual=$(echo "$_response_headers" | grep -i "^$header:" | sed 's/.*: //' | tr -d '\r\n')

    if [ "$actual" = "$expected" ]; then
        success "$test_name"
        TESTS_PASSED=$((TESTS_PASSED + 1))
    else
        fail "$test_name"
        echo "  Header: $header"
        echo "  Expected: $expected"
        echo "  Got: $actual"
        TESTS_FAILED=$((TESTS_FAILED + 1))
    fi
}

# expect_header_exists <header_name> <test_name>
expect_header_exists() {
    local header="$1"
    local test_name="$2"

    TESTS_RUN=$((TESTS_RUN + 1))
    local actual=$(echo "$_response_headers" | grep -i "^$header:" | sed 's/.*: //' | tr -d '\r\n')

    if [ -n "$actual" ]; then
        success "$test_name ($actual)"
        TESTS_PASSED=$((TESTS_PASSED + 1))
    else
        fail "$test_name"
        echo "  Header '$header' not found or empty"
        TESTS_FAILED=$((TESTS_FAILED + 1))
    fi
}

# expect_header_contains <header_name> <substring> <test_name>
expect_header_contains() {
    local header="$1"
    local substring="$2"
    local test_name="$3"

    TESTS_RUN=$((TESTS_RUN + 1))
    local actual=$(echo "$_response_headers" | grep -i "^$header:" | sed 's/.*: //' | tr -d '\r\n')

    if echo "$actual" | grep -q "$substring"; then
        success "$test_name"
        TESTS_PASSED=$((TESTS_PASSED + 1))
    else
        fail "$test_name"
        echo "  Header: $header"
        echo "  Expected to contain: $substring"
        echo "  Got: $actual"
        TESTS_FAILED=$((TESTS_FAILED + 1))
    fi
}

# expect_json_all <jq_path> <expected> <test_name>
# Verifies all values at jq_path equal expected (e.g., all tasks have status=pending)
# Note: Empty result (no matches) passes the test - use expect_json_count first if you need to verify results exist
expect_json_all() {
    local jq_path="$1"
    local expected="$2"
    local test_name="$3"

    TESTS_RUN=$((TESTS_RUN + 1))
    local values=$(echo "$_response" | jq -r "$jq_path" 2>/dev/null | sort -u)

    # Empty is OK (no results), single matching value is OK
    if [ -z "$values" ] || [ "$values" = "$expected" ]; then
        success "$test_name"
        TESTS_PASSED=$((TESTS_PASSED + 1))
    else
        fail "$test_name"
        echo "  Path: $jq_path"
        echo "  Expected all: $expected"
        echo "  Got: $values"
        TESTS_FAILED=$((TESTS_FAILED + 1))
    fi
}

# expect_json_count <jq_array_path> <min_count> <test_name>
# Verifies array has at least min_count elements
expect_json_count() {
    local jq_path="$1"
    local min_count="$2"
    local test_name="$3"

    TESTS_RUN=$((TESTS_RUN + 1))
    local actual=$(echo "$_response" | jq -r "$jq_path | length" 2>/dev/null)

    if [ "$actual" -ge "$min_count" ] 2>/dev/null; then
        success "$test_name (count=$actual)"
        TESTS_PASSED=$((TESTS_PASSED + 1))
    else
        fail "$test_name"
        echo "  Path: $jq_path"
        echo "  Expected at least: $min_count"
        echo "  Got: $actual"
        TESTS_FAILED=$((TESTS_FAILED + 1))
    fi
}

# json_val <jq_path> - extract value from response
json_val() {
    echo "$_response" | jq -r "$1" 2>/dev/null
}

# ============================================================================
# SUMMARY HELPERS
# ============================================================================

# print_summary - Print test results summary
print_summary() {
    echo ""
    echo "========================================"
    echo " Summary"
    echo "========================================"
    echo ""
    echo "Tests run:    $TESTS_RUN"
    echo -e "Tests passed: ${GREEN}$TESTS_PASSED${NC}"
    if [ $TESTS_FAILED -gt 0 ]; then
        echo -e "Tests failed: ${RED}$TESTS_FAILED${NC}"
    else
        echo "Tests failed: $TESTS_FAILED"
    fi
}

# exit_with_result - Exit with appropriate code based on test results
exit_with_result() {
    echo ""
    if [ $TESTS_FAILED -gt 0 ]; then
        fail "Some tests failed!"
        exit 1
    else
        success "All tests passed!"
        exit 0
    fi
}
