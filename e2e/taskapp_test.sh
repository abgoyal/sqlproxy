#!/bin/bash
#
# Task Management API - End-to-End Test Suite
#
# Tests the taskapp configuration which exercises:
# - All HTTP methods (GET, POST, PUT, DELETE, PATCH, HEAD, OPTIONS)
# - Public IDs for secure external identifiers
# - Step-level computed params for ID decoding
# - Path parameters, query parameters, pagination, filtering
# - Trigger-level and step-level caching
# - Rate limiting
# - Template functions (validation, formatting, IDs)
# - Variables config
# - Batch operations (blocks with iteration)
#
# Test Isolation Pattern:
#   Tests that MODIFY data (update, delete, patch) should CREATE their own
#   test data rather than modifying seed data. This prevents:
#   - Interference between tests (e.g., renaming a task breaks search tests)
#   - Order-dependent test failures
#   Public IDs solve the fragile array index problem by providing stable
#   external identifiers independent of database row order.
#   See test_delete_task and test_update_task for examples.
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

# Store public IDs for use across tests
TASK_PUBLIC_ID_1=""
TASK_PUBLIC_ID_2=""
CATEGORY_PUBLIC_ID_1=""

# ============================================================================
# TEST CASES
# ============================================================================

test_init_database() {
    header "Database Initialization"

    POST /api/init
    expect_status 201 "Init returns 201"
    expect_json '.success' 'true' "Init succeeds"
    expect_contains "TaskApp" "Response contains app name from variables"
}

test_list_tasks() {
    header "List Tasks"

    GET /api/tasks
    expect_status 200 "List returns 200"
    expect_contains "tasks" "Response contains tasks array"
    expect_contains "public_id" "Tasks have public_id field"
    expect_contains "tsk_" "Public IDs have correct prefix"

    # Capture first two task public IDs for later tests
    TASK_PUBLIC_ID_1="$(json_val '.tasks[0].public_id')"
    TASK_PUBLIC_ID_2="$(json_val '.tasks[1].public_id')"
    info "Task public IDs: $TASK_PUBLIC_ID_1, $TASK_PUBLIC_ID_2"
    info "Total tasks: $(json_val '.count')"
}

test_create_task() {
    header "Create Task"

    POST /api/tasks title="Shell Test Task" priority=2
    expect_status 201 "Create returns 201"
    expect_json '.success' 'true' "Create succeeds"
    expect_contains "tsk_" "Created task has public_id with prefix"

    local task_public_id=$(json_val '.task.public_id')
    info "Created task public_id: $task_public_id"

    GET "/api/tasks/$task_public_id"
    expect_status 200 "Created task retrievable by public_id"
    expect_contains "Shell Test Task" "Created task has correct title"
}

test_get_single_task() {
    header "Get Single Task"

    GET "/api/tasks/$TASK_PUBLIC_ID_1"
    expect_status 200 "Get returns 200"
    expect_contains "Deploy" "Task has expected title (first by priority DESC)"
    expect_contains "priority_label" "Response includes computed priority_label"
}

test_conditional_responses() {
    header "Conditional Responses"

    # Invalid public ID format
    GET /api/tasks/tsk_invalid_id
    expect_status 404 "Invalid public_id returns 404"
    expect_json '.error' 'Task not found' "Error message correct"

    GET "/api/tasks/$TASK_PUBLIC_ID_1"
    expect_status 200 "Valid public_id returns 200"
}

test_update_task() {
    header "Update Task (PUT)"

    # Create our own task to update - don't modify seed data that other tests depend on
    POST /api/tasks title="Task For Update" priority=2
    local task_public_id=$(json_val '.task.public_id')
    info "Created task $task_public_id for update test"

    PUT "/api/tasks/$task_public_id" --json '{"title":"Updated Title","description":"Updated","status":"completed","priority":3}'
    expect_status 200 "PUT returns 200"
    expect_json '.success' 'true' "Update succeeds"

    GET "/api/tasks/$task_public_id"
    expect_contains "Updated Title" "Title was updated"
}

test_patch_task() {
    header "Partial Update (PATCH)"

    # Create our own task to patch - don't modify seed data
    POST /api/tasks title="Task For Patch" priority=2
    local task_public_id=$(json_val '.task.public_id')
    info "Created task $task_public_id for patch test"

    PATCH "/api/tasks/$task_public_id" --json '{"priority":5}'
    expect_status 200 "PATCH returns 200"
    expect_json '.success' 'true' "Patch succeeds"
}

test_head_requests() {
    header "HEAD Requests"

    HEAD /api/tasks
    expect_status 200 "HEAD /api/tasks returns 200"
    expect_header_exists "X-Total-Count" "X-Total-Count header present"

    HEAD "/api/tasks/$TASK_PUBLIC_ID_1"
    expect_status 200 "HEAD /api/tasks/{public_id} returns 200"
    expect_header "X-Exists" "true" "X-Exists header is true"

    HEAD /api/tasks/invalid_public_id
    expect_status 404 "HEAD missing returns 404"
    expect_header "X-Exists" "false" "X-Exists header is false"
}

test_options_requests() {
    header "OPTIONS Requests"

    OPTIONS /api/tasks
    expect_status 200 "OPTIONS /api/tasks returns 200"
    expect_header_contains "Allow" "GET" "Allow header contains GET"
    expect_header_contains "Allow" "POST" "Allow header contains POST"

    OPTIONS "/api/tasks/$TASK_PUBLIC_ID_1"
    expect_status 200 "OPTIONS /api/tasks/{public_id} returns 200"
}

test_delete_task() {
    header "Delete Task"

    # Reset rate limits - previous tests exhaust the create_limit bucket
    reset_rate_limits "create_limit"

    POST /api/tasks title="To Delete" priority=1
    local task_public_id=$(json_val '.task.public_id')
    info "Created task $task_public_id for deletion"

    DELETE "/api/tasks/$task_public_id"
    expect_status 200 "Delete returns 200"

    GET "/api/tasks/$task_public_id"
    expect_status 404 "Deleted task returns 404"
}

test_trigger_caching() {
    header "Trigger-Level Caching"

    # Reset rate limits before creating test task
    reset_rate_limits "create_limit"

    # Create a fresh task to test caching - avoids pollution from other tests
    POST /api/tasks title="Cache Test Task" priority=1
    local task_public_id=$(json_val '.task.public_id')
    info "Created task $task_public_id for cache test"

    EXPECT_CACHE_MISS=true
    GET "/api/tasks/$task_public_id"
    expect_header "X-Cache" "MISS" "First request is cache MISS"

    EXPECT_CACHE_MISS=false
    EXPECT_CACHE_HIT=true
    GET "/api/tasks/$task_public_id"
    expect_header "X-Cache" "HIT" "Second request is cache HIT"

    reset_expectations
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

    # Reset rate limits to start with a full bucket
    reset_rate_limits "create_limit"

    info "Sending rapid requests to trigger rate limit..."
    EXPECT_RATE_LIMIT=true  # We expect 429s in this test
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
        fail "Rate limit not triggered after 10 requests"
        TESTS_FAILED=$((TESTS_FAILED + 1))
    fi

    reset_expectations
    reset_rate_limits "create_limit"  # Clean up for subsequent tests
}

test_batch_create() {
    header "Batch Create"

    POST /api/tasks/batch --json '{"tasks":[{"title":"Batch1"},{"title":"Batch2"},{"title":"Batch3"}]}'
    expect_status 201 "Batch create returns 201"
    expect_json '.created' '3' "Created 3 tasks"
}

test_batch_delete() {
    header "Batch Delete"

    # Reset rate limits before creating test tasks
    reset_rate_limits "create_limit"

    local ids=()
    for i in 1 2 3; do
        POST /api/tasks title="BatchDel$i" priority=1
        ids+=($(json_val '.task.public_id'))
    done
    info "Created tasks for deletion: ${ids[*]}"

    DELETE /api/tasks/batch --json "{\"ids\":[\"${ids[0]}\",\"${ids[1]}\",\"${ids[2]}\"]}"
    expect_status 200 "Batch delete returns 200"
    expect_json '.deleted' '3' "Deleted 3 tasks"
}

test_search() {
    header "Search"

    GET "/api/search/tasks?q=Review"
    expect_status 200 "Search returns 200"
    expect_contains "results" "Response contains results"
    expect_contains "description_preview" "Results include truncated description"

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
    expect_contains "completion_rate" "Response contains completion_rate"
    expect_contains "recent_tasks" "Response contains recent_tasks with public IDs"
}

test_categories() {
    header "Categories"

    GET /api/categories
    expect_status 200 "List categories returns 200"
    expect_contains "categories" "Response contains categories"
    expect_contains "cat_" "Categories have public_id with prefix"

    # Capture first category public ID
    CATEGORY_PUBLIC_ID_1=$(json_val '.categories[0].public_id')
    info "Category public_id: $CATEGORY_PUBLIC_ID_1"

    GET "/api/categories/$CATEGORY_PUBLIC_ID_1"
    expect_status 200 "Get category returns 200"
    expect_contains "Finance" "Category has expected name"

    GET /api/categories/invalid_cat_id
    expect_status 404 "Missing category returns 404"
}

test_complete_task() {
    header "Complete Task"

    # Reset rate limits - batch_delete exhausts the create_limit bucket
    reset_rate_limits "create_limit"

    # Create our own task to complete - don't modify seed data
    POST /api/tasks title="Task For Complete" priority=1
    local task_public_id=$(json_val '.task.public_id')
    info "Created task $task_public_id for complete test"

    POST "/api/tasks/$task_public_id/complete"
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
    local avg=$(json_val '.priority.average')
    TESTS_RUN=$((TESTS_RUN + 1))
    if [ "$avg" != "null" ]; then
        success "default() function works (avg=$avg)"
        TESTS_PASSED=$((TESTS_PASSED + 1))
    else
        fail "default() function should provide value"
        TESTS_FAILED=$((TESTS_FAILED + 1))
    fi
}

test_validation_functions() {
    header "Validation Functions"

    POST /api/tasks/validate --json '{"title":"Test Task","due_date":"2024-12-31","priority":5,"email":"test@example.com"}'
    expect_status 200 "Validate returns 200"
    expect_json '.valid' 'true' "Valid input returns valid=true"
    expect_json '.validation.email.valid_format' 'true' "isEmail validates correctly"
    expect_json '.validation.due_date.matches_format' 'true' "matches validates date format"

    # Test with invalid email
    POST /api/tasks/validate --json '{"title":"Test","email":"not-an-email"}'
    expect_json '.validation.email.valid_format' 'false' "isEmail rejects invalid email"
}

test_id_generation() {
    header "ID Generation Functions"

    GET /api/demo/ids
    expect_status 200 "Demo IDs returns 200"
    expect_contains "uuid" "Response contains uuid"
    expect_contains "nanoid" "Response contains nanoid"
    expect_contains "short_id" "Response contains short_id"

    # Verify app variables are accessible
    expect_json '.app_info.name' 'TaskApp' "Variables accessible via .vars"
    expect_json '.app_info.default_limit' '10' "DEFAULT_LIMIT variable works"

    # Verify request info
    expect_contains "client_ip" "Request info includes client_ip"
    expect_contains "request_id" "Request info includes request_id"

    # Verify string functions
    expect_json '.string_functions.upper' 'HELLO WORLD' "upper() function works"
    expect_json '.string_functions.split_join' 'a-b-c' "split()/join() functions work"
    expect_json '.string_functions.contains' 'true' "contains() function works"
    expect_json '.string_functions.hasPrefix' 'true' "hasPrefix() function works"
    expect_json '.string_functions.repeat' '*****' "repeat() function works"
    expect_json '.string_functions.substr' 'hello' "substr() function works"

    # Verify encoding functions
    expect_json '.encoding_functions.urlEncode' 'hello+world%26foo%3Dbar' "urlEncode() function works"
    expect_json '.encoding_functions.urlDecode' 'hello world&foo=bar' "urlDecode() function works"
    expect_json '.encoding_functions.base64_encode' 'c2VjcmV0IGRhdGE=' "base64Encode() function works"
    expect_json '.encoding_functions.base64_decode' 'secret data' "base64Decode() function works"
    expect_contains "sha256_hex" "sha256() function works"
    expect_contains "md5_hex" "md5() function works"
    expect_contains "hmac_sha256" "hmacSHA256() function works"

    # Verify validation functions
    expect_json '.validation_functions.is_email_valid' 'true' "isEmail() validates valid email"
    expect_json '.validation_functions.is_email_invalid' 'false' "isEmail() rejects invalid email"
    expect_json '.validation_functions.is_uuid_valid' 'true' "isUUID() validates valid UUID"
    expect_json '.validation_functions.is_uuid_invalid' 'false' "isUUID() rejects invalid UUID"
    expect_json '.validation_functions.is_url_valid' 'true' "isURL() validates valid URL"
    expect_json '.validation_functions.is_url_invalid' 'false' "isURL() rejects invalid URL"
    expect_json '.validation_functions.is_ip_v4' 'true' "isIPv4() validates IPv4"
    expect_json '.validation_functions.is_ip_v6' 'true' "isIPv6() validates IPv6"
    expect_json '.validation_functions.is_ip_any' 'true' "isIP() validates any IP"
    expect_json '.validation_functions.is_numeric_int' 'true' "isNumeric() validates integer"
    expect_json '.validation_functions.is_numeric_float' 'true' "isNumeric() validates float"
    expect_json '.validation_functions.is_numeric_invalid' 'false' "isNumeric() rejects invalid"

    # Verify IP network functions
    expect_contains "ip_network_24" "ipNetwork() extracts network"
    expect_contains "ip_prefix_16" "ipPrefix() extracts prefix"
    expect_contains "normalized_ip" "normalizeIP() normalizes IP"

    # Verify formatting functions
    expect_json '.formatting_functions.formatNumber' '1,234,567.89' "formatNumber() function works"
    expect_json '.formatting_functions.formatBytes' '1.5 MB' "formatBytes() function works"
    expect_json '.formatting_functions.zeropad' '00042' "zeropad() function works"

    # Verify datetime functions
    local unix_ts=$(json_val '.datetime_functions.unix_timestamp')
    TESTS_RUN=$((TESTS_RUN + 1))
    if [ "$unix_ts" -gt 0 ] 2>/dev/null; then
        success "unixTime() returns valid timestamp ($unix_ts)"
        TESTS_PASSED=$((TESTS_PASSED + 1))
    else
        fail "unixTime() should return positive timestamp"
        TESTS_FAILED=$((TESTS_FAILED + 1))
    fi
    # 2024-12-25 in Unix time is 1735084800
    expect_json '.datetime_functions.parsed_unix' '1735084800' "parseTime() parses date correctly"

    # Verify debug functions
    expect_json '.debug_functions.type_of_string' 'string' "typeOf() identifies string"
    expect_json '.debug_functions.type_of_number' 'int' "typeOf() identifies int"
    expect_json '.debug_functions.type_of_bool' 'bool' "typeOf() identifies bool"
}

test_array_json_helpers() {
    header "Array and JSON Helper Functions"

    GET /api/demo/helpers
    expect_status 200 "Helpers demo returns 200"

    # Array helpers
    expect_contains "first_task" "first() extracts first element"
    expect_contains "last_task" "last() extracts last element"
    expect_contains "all_titles" "pluck() extracts field from array"
    expect_json '.array_helpers.is_empty' 'false' "isEmpty() returns false for non-empty"

    local count=$(json_val '.array_helpers.count')
    TESTS_RUN=$((TESTS_RUN + 1))
    if [ "$count" -ge 1 ]; then
        success "len() counts array elements ($count)"
        TESTS_PASSED=$((TESTS_PASSED + 1))
    else
        fail "len() should return positive count"
        TESTS_FAILED=$((TESTS_FAILED + 1))
    fi

    # JSON helpers
    expect_contains "picked_fields" "pick() selects specific fields"
    expect_contains "omitted_fields" "omit() excludes specific fields"
    expect_contains "merged" "merge() combines maps"
    expect_contains "keys_of_first" "keys() extracts map keys"
    expect_contains "values_of_first" "values() extracts map values"

    # Conditional helpers
    expect_json '.conditional_helpers.ternary_true' 'yes' "ternary() returns true value"
    expect_json '.conditional_helpers.ternary_false' 'no' "ternary() returns false value"
    expect_json '.conditional_helpers.when_true' 'shows' "when() returns value when true"
    expect_json '.conditional_helpers.when_false' '' "when() returns empty when false"
    expect_json '.conditional_helpers.coalesce_value' 'provided' "coalesce() returns first non-empty"
    expect_json '.conditional_helpers.coalesce_empty' 'fallback' "coalesce() skips empty strings"
}

test_condition_expr_functions() {
    header "Condition Expression Functions"

    # Test condition functions: alias chaining, divOr, len, hasPrefix, upper
    GET /api/demo/condition-functions
    expect_status 200 "Condition functions endpoint returns 200"

    # Alias chaining tests
    expect_json '.alias_chaining.has_data' 'true' "len() works in condition alias"
    expect_json '.alias_chaining.has_multiple' 'true' "len() comparison in condition alias"
    expect_json '.alias_chaining.ready' 'true' "Alias chaining (ready depends on has_data)"

    # Safe division with divOr
    local task_count=$(json_val '.safe_division.task_count')
    local half_count=$(json_val '.safe_division.half_count')
    TESTS_RUN=$((TESTS_RUN + 1))
    if [ "$half_count" -ge 1 ]; then
        success "divOr() works in condition ($task_count / 2 = $half_count)"
        TESTS_PASSED=$((TESTS_PASSED + 1))
    else
        fail "divOr() should return positive result"
        TESTS_FAILED=$((TESTS_FAILED + 1))
    fi
    expect_json '.safe_division.good_ratio' 'true' "divOr() works in condition expression"

    # String functions in conditions
    expect_contains "sample_title" "Sample title is returned"
    expect_json '.string_conditions.has_prefix_match' 'true' "hasPrefix() works in condition"
    expect_contains "upper_status" "upper() transforms string"

    # Overall result
    expect_json '.result' 'all_conditions_passed' "All condition functions work correctly"
}

test_header_cookie_functions() {
    header "Header and Cookie Functions"

    # Test with default values (no custom headers/cookies)
    GET /api/demo/request
    expect_status 200 "Request demo returns 200"
    expect_json '.headers.custom_header' 'not provided' "header() returns default for missing header"
    expect_json '.cookies.session_id' 'none' "cookie() returns default for missing cookie"
    expect_json '.headers.auth_present' 'false' "header presence check works"

    # Test with custom header
    GET /api/demo/request "X-Custom-Header: my-value"
    expect_status 200 "Request with custom header returns 200"
    expect_json '.headers.custom_header' 'my-value' "header() extracts custom header"

    # Test with cookie
    GET /api/demo/request "Cookie: session_id=abc123; theme=dark"
    expect_status 200 "Request with cookie returns 200"
    expect_json '.cookies.session_id' 'abc123' "cookie() extracts session_id"
    expect_json '.cookies.theme' 'dark' "cookie() extracts theme"
    expect_json '.cookies.has_session' 'true' "cookie presence check works"
}

test_cron_functionality() {
    header "Cron Trigger Demonstration"

    # Get initial cron status
    GET /api/cron/status
    expect_status 200 "Cron status returns 200"
    expect_json '.cron_enabled' 'true' "Cron is enabled"
    expect_json '.schedule' '* * * * *' "Cron schedule is every minute"

    # Get initial count
    local initial_count=$(json_val '.execution_count')
    info "Initial execution count: $initial_count"

    # Manually trigger the cron logic
    POST /api/cron/trigger
    expect_status 200 "Manual trigger returns 200"
    expect_json '.triggered' 'true' "Trigger confirmed"

    # Verify count increased
    local new_count=$(json_val '.execution_count')
    TESTS_RUN=$((TESTS_RUN + 1))
    if [ "$new_count" -gt "$initial_count" ]; then
        success "Execution count incremented ($initial_count -> $new_count)"
        TESTS_PASSED=$((TESTS_PASSED + 1))
    else
        fail "Execution count should have increased"
        TESTS_FAILED=$((TESTS_FAILED + 1))
    fi

    # Verify last_run is set
    expect_json '.message' 'manually triggered' "Message updated after trigger"

    # Trigger again and verify count increases
    POST /api/cron/trigger
    expect_status 200 "Second trigger returns 200"
    local final_count=$(json_val '.execution_count')
    TESTS_RUN=$((TESTS_RUN + 1))
    if [ "$final_count" -gt "$new_count" ]; then
        success "Count increases on each trigger ($new_count -> $final_count)"
        TESTS_PASSED=$((TESTS_PASSED + 1))
    else
        fail "Count should increase on each trigger"
        TESTS_FAILED=$((TESTS_FAILED + 1))
    fi
}

test_httpcall_functionality() {
    header "HTTPCall Step Demonstration"

    # Test GET request to external API
    GET /api/demo/httpcall
    expect_status 200 "HTTPCall GET returns 200"
    expect_json '.httpcall_demo' 'true' "HTTPCall demo flag is set"

    # Verify external IP was retrieved (don't check specific value)
    TESTS_RUN=$((TESTS_RUN + 1))
    local external_ip=$(json_val '.external_ip')
    if [ -n "$external_ip" ] && [ "$external_ip" != "null" ] && [ "$external_ip" != "<no value>" ]; then
        success "External IP retrieved ($external_ip)"
        TESTS_PASSED=$((TESTS_PASSED + 1))
    else
        fail "External IP should be retrieved from httpbin"
        TESTS_FAILED=$((TESTS_FAILED + 1))
    fi

    # Verify httpbin response status
    expect_json '.httpbin_response.status_code' '200' "HTTPbin returned 200"

    # Verify args were passed through (APP_NAME variable is "TaskApp")
    expect_json '.httpbin_response.args_received.demo' 'true' "Query param 'demo' passed through"
    expect_json '.httpbin_response.args_received.app' 'TaskApp' "Query param 'app' passed through"

    # Test POST request to echo endpoint
    POST /api/demo/httpcall/echo message="Hello HTTPCall"
    expect_status 200 "HTTPCall POST returns 200"
    expect_json '.echo_demo' 'true' "Echo demo flag is set"
    expect_json '.sent_message' 'Hello HTTPCall' "Sent message matches"
    expect_json '.httpbin_received.message' 'Hello HTTPCall' "HTTPbin received correct message"
    expect_json '.httpbin_status' '200' "HTTPbin POST returned 200"
}

test_public_id_round_trip() {
    header "Public ID Round Trip"

    # Reset rate limits for deterministic behavior
    reset_rate_limits "create_limit"

    # Create a task
    POST /api/tasks title="Round Trip Test" priority=1
    expect_status 201 "Create returns 201"
    local public_id=$(json_val '.task.public_id')
    info "Created task with public_id: $public_id"

    # Verify public_id format
    TESTS_RUN=$((TESTS_RUN + 1))
    if [[ "$public_id" == tsk_* ]]; then
        success "Public ID has correct prefix (tsk_)"
        TESTS_PASSED=$((TESTS_PASSED + 1))
    else
        fail "Public ID should start with tsk_"
        TESTS_FAILED=$((TESTS_FAILED + 1))
    fi

    # Retrieve by public_id
    GET "/api/tasks/$public_id"
    expect_status 200 "Retrieve by public_id works"
    expect_contains "Round Trip Test" "Task data correct"

    # Update by public_id
    PATCH "/api/tasks/$public_id" --json '{"title":"Updated Round Trip"}'
    expect_status 200 "Update by public_id works"

    # Delete by public_id
    DELETE "/api/tasks/$public_id"
    expect_status 200 "Delete by public_id works"

    # Immediately after delete, cached response is still returned (TTL=2s)
    # Note: Cache invalidation on mutation is not yet implemented
    GET "/api/tasks/$public_id"
    expect_status 200 "Deleted task still returns 200 from cache"
    expect_header "X-Cache" "HIT" "Response served from cache after delete"

    # Wait for cache TTL to expire (2 seconds + buffer)
    sleep 3

    # After cache expires, resource is properly gone
    GET "/api/tasks/$public_id"
    expect_status 404 "Deleted task returns 404 after cache expires"
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
    test_batch_create
    test_batch_delete
    test_search
    test_step_caching  # Must run before test_stats (both use /api/stats, step cache has 30s TTL)
    test_stats
    test_categories
    test_complete_task
    test_filtering
    test_pagination
    test_template_functions
    test_validation_functions
    test_id_generation
    test_array_json_helpers
    test_condition_expr_functions
    test_header_cookie_functions
    test_cron_functionality
    test_httpcall_functionality
    test_public_id_round_trip

    # Cache and rate limit tests run last with isolation
    # These tests have timing-sensitive behavior that can affect other tests
    test_trigger_caching
    test_rate_limiting

    # Infrastructure tests
    test_metrics
    test_openapi

    # Summary
    print_summary
    print_coverage_info
    exit_with_result
}

main
