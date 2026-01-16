#!/bin/bash
#
# Task Management API - End-to-End Test Suite
#
# Tests the taskapp configuration which exercises:
# - All HTTP methods (GET, POST, PUT, DELETE, PATCH, HEAD, OPTIONS)
# - Path parameters, query parameters, pagination, filtering
# - Trigger-level and step-level caching
# - Rate limiting
# - Template functions
# - Batch operations (blocks with iteration)
#
# Usage:
#   ./e2e/taskapp_test.sh           # Run tests without coverage
#   ./e2e/taskapp_test.sh --cover   # Run with coverage

set -euo pipefail

# ============================================================================
# SETUP
# ============================================================================

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"

# Source shared libraries
source "$SCRIPT_DIR/lib/helpers.sh"
source "$SCRIPT_DIR/lib/runner.sh"

# App configuration
APP_NAME="taskapp"

# Parse command line arguments
parse_args "$@"

# Set up test environment
setup_test_env "$APP_NAME"

# Config template (uses PROJECT_ROOT from setup_test_env)
CONFIG_TEMPLATE="$PROJECT_ROOT/testdata/taskapp.yaml"

# Database path substitution
declare -A DB_VARS
DB_VARS[DB_PATH]="$TEMP_DIR/taskapp.db"

# ============================================================================
# TEST CASES
# ============================================================================

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
        fail "Rate limit not triggered after 15 requests"
        TESTS_FAILED=$((TESTS_FAILED + 1))
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

# ============================================================================
# MAIN
# ============================================================================

main() {
    print_test_header "$APP_NAME" "Task Management API - E2E Test Suite"

    check_dependencies
    build_binary
    create_config
    start_server

    # Run all tests
    test_health
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
    print_summary
    print_coverage_info
    exit_with_result
}

main
