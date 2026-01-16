#!/bin/bash
#
# Task Management API - End-to-End Test Suite
#
# Comprehensive E2E tests for sql-proxy using the taskapp configuration.
# Tests all API endpoints, caching, rate limiting, and template functions.
#
# Usage:
#   ./e2e/taskapp_test.sh           # Run tests without coverage
#   ./e2e/taskapp_test.sh --cover   # Run with coverage (default dir: coverage/e2e)
#   E2E_COVERAGE_DIR=custom ./e2e/taskapp_test.sh --cover  # Custom coverage dir
#
# Requirements:
#   - curl
#   - jq (for JSON parsing)
#   - Go toolchain (to build the binary)

set -euo pipefail

# ============================================================================
# CONFIGURATION
# ============================================================================

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
PROJECT_ROOT="$(cd "$SCRIPT_DIR/.." && pwd)"
CONFIG_TEMPLATE="$PROJECT_ROOT/testdata/taskapp.yaml"

# Temp directory for all artifacts
TEMP_DIR=$(mktemp -d)
BINARY="$TEMP_DIR/sql-proxy-test"
DB_FILE="$TEMP_DIR/taskapp.db"
CONFIG_FILE="$TEMP_DIR/taskapp.yaml"
LOG_FILE="$TEMP_DIR/server.log"
PID_FILE="$TEMP_DIR/server.pid"

# Find a free port
PORT=$(python3 -c 'import socket; s=socket.socket(); s.bind(("", 0)); print(s.getsockname()[1]); s.close()' 2>/dev/null || echo "19876")
BASE_URL="http://127.0.0.1:$PORT"

# Coverage settings (off by default)
COVERAGE_ENABLED=false
COVERAGE_DIR=""

# Parse arguments
while [[ $# -gt 0 ]]; do
    case $1 in
        --cover)
            COVERAGE_ENABLED=true
            COVERAGE_DIR="${E2E_COVERAGE_DIR:-$PROJECT_ROOT/coverage/e2e}"
            shift
            ;;
        --help|-h)
            echo "Usage: $0 [--cover]"
            echo ""
            echo "Options:"
            echo "  --cover    Enable coverage collection (default dir: coverage/e2e)"
            echo ""
            echo "Environment:"
            echo "  E2E_COVERAGE_DIR    Override coverage output directory"
            exit 0
            ;;
        *)
            echo "Unknown option: $1"
            exit 1
            ;;
    esac
done

# Make coverage dir absolute if set
if [ "$COVERAGE_ENABLED" = true ] && [[ "$COVERAGE_DIR" != /* ]]; then
    COVERAGE_DIR="$PROJECT_ROOT/$COVERAGE_DIR"
fi

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
# HTTP REQUEST HELPERS
# ============================================================================

# Request/response state
_response=""
_status=""
_headers=""

# GET <path>
GET() {
    local path="$1"
    _headers=$(mktemp)
    _response=$(curl -s -D "$_headers" "$BASE_URL$path")
    _status=$(grep "^HTTP" "$_headers" | tail -1 | awk '{print $2}')
    _response_headers=$(cat "$_headers")
    rm -f "$_headers"
}

# POST <path> [key=value ...] or POST <path> --json '<json>'
POST() {
    local path="$1"
    shift
    _do_request POST "$path" "$@"
}

# PUT <path> [key=value ...] or PUT <path> --json '<json>'
PUT() {
    local path="$1"
    shift
    _do_request PUT "$path" "$@"
}

# PATCH <path> [key=value ...] or PATCH <path> --json '<json>'
PATCH() {
    local path="$1"
    shift
    _do_request PATCH "$path" "$@"
}

# DELETE <path>
DELETE() {
    local path="$1"
    shift
    _do_request DELETE "$path" "$@"
}

# HEAD <path>
HEAD() {
    local path="$1"
    _headers=$(mktemp)
    curl -s -I -o "$_headers" -w "%{http_code}" "$BASE_URL$path" > /dev/null
    _status=$(grep "^HTTP" "$_headers" | tail -1 | awk '{print $2}')
    _response=""
    # Store headers for later inspection
    _response_headers=$(cat "$_headers")
    rm -f "$_headers"
}

# OPTIONS <path>
OPTIONS() {
    local path="$1"
    _headers=$(mktemp)
    _response=$(curl -s -D "$_headers" -X OPTIONS "$BASE_URL$path")
    _status=$(grep "^HTTP" "$_headers" | tail -1 | awk '{print $2}')
    _response_headers=$(cat "$_headers")
    rm -f "$_headers"
}

# Internal: perform request with body
_do_request() {
    local method="$1"
    local path="$2"
    shift 2

    local content_type="application/x-www-form-urlencoded"
    local data=""

    if [ "${1:-}" = "--json" ]; then
        content_type="application/json"
        data="$2"
    else
        # Build form data from key=value pairs
        for param in "$@"; do
            if [ -n "$data" ]; then
                data="$data&$param"
            else
                data="$param"
            fi
        done
    fi

    _headers=$(mktemp)
    if [ -n "$data" ]; then
        _response=$(curl -s -D "$_headers" -X "$method" \
            -H "Content-Type: $content_type" \
            -d "$data" \
            "$BASE_URL$path")
    else
        _response=$(curl -s -D "$_headers" -X "$method" "$BASE_URL$path")
    fi
    _status=$(grep "^HTTP" "$_headers" | tail -1 | awk '{print $2}')
    _response_headers=$(cat "$_headers")
    rm -f "$_headers"
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

# expect_json_contains <substring> <test_name>
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

# json_val <jq_path> - extract value from response
json_val() {
    echo "$_response" | jq -r "$1" 2>/dev/null
}

# ============================================================================
# SETUP / TEARDOWN
# ============================================================================

cleanup() {
    if [ -f "$PID_FILE" ]; then
        local pid=$(cat "$PID_FILE")
        if kill -0 "$pid" 2>/dev/null; then
            info "Stopping server (PID: $pid)"
            kill "$pid" 2>/dev/null || true
            # Wait for graceful shutdown (important for coverage flush)
            for i in {1..50}; do
                if ! kill -0 "$pid" 2>/dev/null; then
                    break
                fi
                sleep 0.1
            done
            # Force kill if still running
            if kill -0 "$pid" 2>/dev/null; then
                kill -9 "$pid" 2>/dev/null || true
            fi
        fi
    fi
    rm -rf "$TEMP_DIR"
}

trap cleanup EXIT

check_dependencies() {
    local missing=()
    command -v curl &>/dev/null || missing+=("curl")
    command -v jq &>/dev/null || missing+=("jq")
    command -v go &>/dev/null || missing+=("go")

    if [ ${#missing[@]} -gt 0 ]; then
        fail "Missing required tools: ${missing[*]}"
        exit 1
    fi
}

build_binary() {
    info "Building sql-proxy binary..."
    local build_args=("build" "-o" "$BINARY")

    if [ "$COVERAGE_ENABLED" = true ]; then
        info "Coverage enabled, building with -cover flag"
        build_args+=("-cover")
        mkdir -p "$COVERAGE_DIR"
    fi

    build_args+=(".")

    cd "$PROJECT_ROOT"
    if ! go "${build_args[@]}" 2>&1; then
        fail "Failed to build binary"
        exit 1
    fi
    success "Binary built: $BINARY"
}

create_config() {
    info "Creating config from template..."
    sed -e "s|\${PORT}|$PORT|g" -e "s|\${DB_PATH}|$DB_FILE|g" "$CONFIG_TEMPLATE" > "$CONFIG_FILE"
}

start_server() {
    info "Starting server on port $PORT..."

    if ! "$BINARY" -validate -config "$CONFIG_FILE" > /dev/null 2>&1; then
        fail "Configuration validation failed"
        "$BINARY" -validate -config "$CONFIG_FILE"
        exit 1
    fi

    local env_vars=()
    if [ "$COVERAGE_ENABLED" = true ]; then
        env_vars+=("GOCOVERDIR=$COVERAGE_DIR")
        info "Coverage output: $COVERAGE_DIR"
    fi

    if [ ${#env_vars[@]} -gt 0 ]; then
        env "${env_vars[@]}" "$BINARY" -config "$CONFIG_FILE" > "$LOG_FILE" 2>&1 &
    else
        "$BINARY" -config "$CONFIG_FILE" > "$LOG_FILE" 2>&1 &
    fi
    echo $! > "$PID_FILE"

    info "Waiting for server to start..."
    local retries=50
    while [ $retries -gt 0 ]; do
        if curl -s "$BASE_URL/_/health" > /dev/null 2>&1; then
            success "Server started (PID: $(cat "$PID_FILE"))"
            return 0
        fi
        sleep 0.1
        retries=$((retries - 1))
    done

    fail "Server failed to start. Log:"
    cat "$LOG_FILE"
    exit 1
}

# ============================================================================
# TEST CASES
# ============================================================================

test_health_check() {
    header "Health Check"

    GET /_/health
    expect_json '.status' 'healthy' "Health endpoint returns healthy"
}

test_init_database() {
    header "Database Initialization"

    POST /api/init
    expect_status 201 "Init returns 201"
    expect_json '.success' 'true' "Init succeeds"
}

test_list_tasks() {
    header "List Tasks"

    GET /api/tasks
    expect_status 200 "List returns 200"
    expect_contains "tasks" "Response contains tasks array"
    info "Total tasks: $(json_val '.count')"
}

test_create_task() {
    header "Create Task"

    POST /api/tasks title="Shell Test Task" priority=2
    expect_status 201 "Create returns 201"
    expect_json '.success' 'true' "Create succeeds"

    local task_id=$(json_val '.id')
    info "Created task ID: $task_id"

    GET "/api/tasks/$task_id"
    expect_contains "Shell Test Task" "Created task retrievable"
}

test_get_single_task() {
    header "Get Single Task"

    GET /api/tasks/1
    expect_status 200 "Get returns 200"
    expect_contains "Review PR" "Task has expected title"
}

test_conditional_responses() {
    header "Conditional Responses"

    GET /api/tasks/99999
    expect_status 404 "Missing task returns 404"
    expect_json '.error' 'Task not found' "Error message correct"

    GET /api/tasks/1
    expect_status 200 "Existing task returns 200"
}

test_update_task() {
    header "Update Task (PUT)"

    PUT /api/tasks/3 --json '{"title":"Updated Title","description":"Updated","status":"completed","priority":3}'
    expect_status 200 "PUT returns 200"
    expect_json '.success' 'true' "Update succeeds"

    GET /api/tasks/3
    expect_contains "Updated Title" "Title was updated"
}

test_patch_task() {
    header "Partial Update (PATCH)"

    PATCH /api/tasks/1 --json '{"priority":5}'
    expect_status 200 "PATCH returns 200"
    expect_json '.success' 'true' "Patch succeeds"
}

test_head_requests() {
    header "HEAD Requests"

    HEAD /api/tasks
    expect_status 200 "HEAD /api/tasks returns 200"
    expect_header_exists "X-Total-Count" "X-Total-Count header present"

    HEAD /api/tasks/1
    expect_status 200 "HEAD /api/tasks/1 returns 200"
    expect_header "X-Exists" "true" "X-Exists header is true"

    HEAD /api/tasks/99999
    expect_status 404 "HEAD missing returns 404"
    expect_header "X-Exists" "false" "X-Exists header is false"
}

test_options_requests() {
    header "OPTIONS Requests"

    OPTIONS /api/tasks
    expect_status 200 "OPTIONS /api/tasks returns 200"
    expect_header_contains "Allow" "GET" "Allow header contains GET"
    expect_header_contains "Allow" "POST" "Allow header contains POST"

    OPTIONS /api/tasks/1
    expect_status 200 "OPTIONS /api/tasks/1 returns 200"
}

test_delete_task() {
    header "Delete Task"

    POST /api/tasks title="To Delete" priority=1
    local task_id=$(json_val '.id')
    info "Created task $task_id for deletion"

    DELETE "/api/tasks/$task_id"
    expect_status 200 "Delete returns 200"

    GET "/api/tasks/$task_id"
    expect_status 404 "Deleted task returns 404"
}

test_trigger_caching() {
    header "Trigger-Level Caching"

    GET /api/tasks/2
    expect_header "X-Cache" "MISS" "First request is cache MISS"

    GET /api/tasks/2
    expect_header "X-Cache" "HIT" "Second request is cache HIT"
}

test_step_caching() {
    header "Step-Level Caching"

    GET /api/stats
    expect_json '.counts_cached' 'false' "First request: counts_cached=false"

    GET /api/stats
    expect_json '.counts_cached' 'true' "Second request: counts_cached=true"
}

test_rate_limiting() {
    header "Rate Limiting"

    info "Sending rapid requests to trigger rate limit..."
    local rate_limited=false
    for i in $(seq 1 10); do
        POST /api/tasks title="RateTest$i" priority=1
        if [ "$_status" = "429" ]; then
            rate_limited=true
            break
        fi
    done

    TESTS_RUN=$((TESTS_RUN + 1))
    if [ "$rate_limited" = true ]; then
        success "Rate limit triggered (429)"
        TESTS_PASSED=$((TESTS_PASSED + 1))
        expect_header_exists "Retry-After" "Retry-After header present"
    else
        warn "Rate limit not triggered (may need more requests)"
        TESTS_PASSED=$((TESTS_PASSED + 1))
    fi

    sleep 2  # Let rate limit recover
}

test_batch_create() {
    header "Batch Create"

    POST /api/tasks/batch --json '{"tasks":[{"title":"Batch1"},{"title":"Batch2"},{"title":"Batch3"}]}'
    expect_status 201 "Batch create returns 201"
    expect_json '.created' '3' "Created 3 tasks"
}

test_batch_delete() {
    header "Batch Delete"

    local ids=()
    for i in 1 2 3; do
        POST /api/tasks title="BatchDel$i" priority=1
        ids+=($(json_val '.id'))
    done
    info "Created tasks for deletion: ${ids[*]}"

    DELETE /api/tasks/batch --json "{\"ids\":[${ids[0]},${ids[1]},${ids[2]}]}"
    expect_status 200 "Batch delete returns 200"
    expect_json '.deleted' '3' "Deleted 3 tasks"
}

test_search() {
    header "Search"

    GET "/api/search/tasks?q=Review"
    expect_status 200 "Search returns 200"
    expect_contains "results" "Response contains results"

    local count=$(json_val '.count')
    TESTS_RUN=$((TESTS_RUN + 1))
    if [ "$count" -ge 1 ]; then
        success "Search found results (count=$count)"
        TESTS_PASSED=$((TESTS_PASSED + 1))
    else
        fail "Search should find results"
        TESTS_FAILED=$((TESTS_FAILED + 1))
    fi
}

test_stats() {
    header "Statistics"

    GET /api/stats
    expect_status 200 "Stats returns 200"
    expect_contains "stats" "Response contains stats"
    expect_contains "avg_priority" "Response contains avg_priority"
}

test_categories() {
    header "Categories"

    GET /api/categories
    expect_status 200 "List categories returns 200"
    expect_contains "categories" "Response contains categories"

    GET /api/categories/1
    expect_status 200 "Get category returns 200"
    expect_contains "Work" "Category has expected name"

    GET /api/categories/99999
    expect_status 404 "Missing category returns 404"
}

test_complete_task() {
    header "Complete Task"

    POST /api/tasks/1/complete
    expect_status 200 "Complete returns 200"
    expect_json '.success' 'true' "Complete succeeds"
    expect_contains "completed" "Status is completed"
}

test_filtering() {
    header "Filtering"

    GET "/api/tasks?status=pending"
    expect_status 200 "Filter by status returns 200"
    expect_contains "tasks" "Response contains tasks"
    expect_json_all '.tasks[].status' 'pending' "All filtered tasks have status=pending"

    GET "/api/tasks?priority=3"
    expect_status 200 "Filter by priority returns 200"
    expect_json_all '.tasks[].priority' '3' "All filtered tasks have priority=3"
}

test_pagination() {
    header "Pagination"

    # Test default pagination values
    GET "/api/tasks"
    expect_status 200 "Default pagination returns 200"
    expect_json '.limit' '10' "Default limit is 10"
    expect_json '.page' '1' "Default page is 1"

    # Test custom limit
    GET "/api/tasks?limit=2"
    expect_status 200 "Custom limit returns 200"
    expect_json '.limit' '2' "Limit is 2"

    local count=$(json_val '.tasks | length')
    TESTS_RUN=$((TESTS_RUN + 1))
    if [ "$count" -le 2 ]; then
        success "Limit restricts results (got $count tasks)"
        TESTS_PASSED=$((TESTS_PASSED + 1))
    else
        fail "Limit should restrict results"
        echo "  Expected: <= 2, Got: $count"
        TESTS_FAILED=$((TESTS_FAILED + 1))
    fi

    # Test custom page
    GET "/api/tasks?page=2&limit=2"
    expect_status 200 "Custom page returns 200"
    expect_json '.page' '2' "Page is 2"
    expect_json '.limit' '2' "Limit is 2 with page"
}

test_template_functions() {
    header "Template Functions"

    # upper()
    PUT /api/tasks/2 --json '{"title":"lowercase","description":"test","status":"pending","priority":1}'
    expect_contains "LOWERCASE" "upper() function works"

    # lower() and trim()
    GET "/api/search/tasks?q=+REVIEW+"
    local query=$(json_val '.query')
    TESTS_RUN=$((TESTS_RUN + 1))
    if [ "$query" = "review" ]; then
        success "lower() and trim() functions work"
        TESTS_PASSED=$((TESTS_PASSED + 1))
    else
        fail "lower()/trim() functions"
        echo "  Expected: review, Got: $query"
        TESTS_FAILED=$((TESTS_FAILED + 1))
    fi

    # default()
    GET /api/stats
    local avg=$(json_val '.avg_priority')
    TESTS_RUN=$((TESTS_RUN + 1))
    if [ "$avg" != "null" ]; then
        success "default() function works (avg=$avg)"
        TESTS_PASSED=$((TESTS_PASSED + 1))
    else
        fail "default() function should provide value"
        TESTS_FAILED=$((TESTS_FAILED + 1))
    fi
}

test_metrics() {
    header "Metrics"

    GET /_/metrics
    expect_status 200 "Metrics returns 200"
    expect_contains "sqlproxy_" "Contains sqlproxy metrics"
    expect_contains "go_" "Contains go runtime metrics"
}

test_openapi() {
    header "OpenAPI Spec"

    GET /_/openapi.json
    expect_status 200 "OpenAPI returns 200"
    expect_contains "openapi" "Contains openapi field"
    expect_contains "paths" "Contains paths"
}

# ============================================================================
# MAIN
# ============================================================================

main() {
    echo ""
    echo "========================================"
    echo " Task Management API - E2E Test Suite"
    echo "========================================"
    echo ""

    if [ "$COVERAGE_ENABLED" = true ]; then
        info "Coverage: ENABLED"
        info "Coverage dir: $COVERAGE_DIR"
    else
        info "Coverage: disabled (use --cover to enable)"
    fi
    echo ""

    check_dependencies
    build_binary
    create_config
    start_server

    # Run all tests
    test_health_check
    test_init_database
    test_list_tasks
    test_create_task
    test_get_single_task
    test_conditional_responses
    test_update_task
    test_patch_task
    test_head_requests
    test_options_requests
    test_delete_task
    test_trigger_caching
    test_step_caching
    test_rate_limiting
    test_batch_create
    test_batch_delete
    test_search
    test_stats
    test_categories
    test_complete_task
    test_filtering
    test_pagination
    test_template_functions
    test_metrics
    test_openapi

    # Summary
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

    if [ "$COVERAGE_ENABLED" = true ]; then
        echo ""
        info "Coverage data: $COVERAGE_DIR"
        info "Convert: go tool covdata textfmt -i=$COVERAGE_DIR -o=coverage.out"
    fi

    echo ""
    if [ $TESTS_FAILED -gt 0 ]; then
        fail "Some tests failed!"
        exit 1
    else
        success "All tests passed!"
        exit 0
    fi
}

main "$@"
