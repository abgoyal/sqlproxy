#!/bin/bash
#
# Shared test runner for sql-proxy E2E tests
#
# This file provides:
# - Dependency checking
# - Binary building (with optional coverage)
# - Server start/stop
# - Config template processing
# - Cleanup handling
#
# Usage:
#   source "$(dirname "$0")/lib/runner.sh"
#   setup_test_env "appname"
#   start_server
#   # ... run tests ...
#   # cleanup is automatic via trap
#
# Required variables (set before calling setup_test_env):
#   APP_NAME         - Name of the app (e.g., "taskapp")
#   CONFIG_TEMPLATE  - Path to YAML config template
#
# Optional variables:
#   DB_VARS          - Associative array of DB path variables to substitute
#                      e.g., DB_VARS=([DB_PATH]="$TEMP_DIR/app.db" [DB2_PATH]="$TEMP_DIR/app2.db")

# Ensure we're running in bash (not sh)
if [ -z "$BASH_VERSION" ]; then
    echo "Error: This script requires bash. Run with 'bash' not 'sh'."
    exit 1
fi

# Ensure helpers are loaded
if [ -z "$TESTS_RUN" ]; then
    echo "Error: helpers.sh must be sourced before runner.sh"
    exit 1
fi

# ============================================================================
# CONFIGURATION
# ============================================================================

# These are set by setup_test_env
SCRIPT_DIR=""
PROJECT_ROOT=""
TEMP_DIR=""
BINARY=""
CONFIG_FILE=""
LOG_FILE=""
PID_FILE=""
PORT=""
BASE_URL=""

# Coverage settings
COVERAGE_ENABLED=false
COVERAGE_DIR=""

# ============================================================================
# ARGUMENT PARSING
# ============================================================================

parse_args() {
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
    # Note: COVERAGE_DIR is made absolute in setup_test_env after PROJECT_ROOT is set
}

# ============================================================================
# SETUP / TEARDOWN
# ============================================================================

# setup_test_env <app_name> - Initialize test environment
# Sets up temp directory, paths, and finds a free port
setup_test_env() {
    local app_name="$1"

    SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[1]}")" && pwd)"
    PROJECT_ROOT="$(cd "$SCRIPT_DIR/.." && pwd)"

    # Make coverage dir absolute if set (must be after PROJECT_ROOT is set)
    if [ "$COVERAGE_ENABLED" = true ] && [ -n "$COVERAGE_DIR" ] && [[ "$COVERAGE_DIR" != /* ]]; then
        COVERAGE_DIR="$PROJECT_ROOT/$COVERAGE_DIR"
    fi

    # Create temp directory
    TEMP_DIR=$(mktemp -d)
    BINARY="$TEMP_DIR/sql-proxy-test"
    CONFIG_FILE="$TEMP_DIR/${app_name}.yaml"
    LOG_FILE="$TEMP_DIR/server.log"
    PID_FILE="$TEMP_DIR/server.pid"

    # Find a free port using pure bash
    # Start from a random port in the ephemeral range (49152-65535)
    local base_port=$((49152 + RANDOM % 10000))
    PORT=""
    for offset in 0 1 2 3 4 5 6 7 8 9; do
        local try_port=$((base_port + offset))
        # Check if port is in use via /dev/tcp (bash built-in)
        if ! (echo >/dev/tcp/127.0.0.1/$try_port) 2>/dev/null; then
            PORT=$try_port
            break
        fi
    done
    if [ -z "$PORT" ]; then
        fail "Could not find a free port in range $base_port-$((base_port + 9))"
        exit 1
    fi
    BASE_URL="http://127.0.0.1:$PORT"

    # Set up cleanup trap
    trap cleanup EXIT
}

# cleanup - Stop server and remove temp files
cleanup() {
    if [ -f "$PID_FILE" ]; then
        local pid=$(cat "$PID_FILE")
        if kill -0 "$pid" 2>/dev/null; then
            info "Stopping server (PID: $pid)"
            kill "$pid" 2>/dev/null || true
            # Wait for graceful shutdown (important for coverage flush)
            # 10 second timeout (100 iterations * 0.1s) to allow coverage data to write
            for i in {1..100}; do
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
    if [ -n "$TEMP_DIR" ] && [ -d "$TEMP_DIR" ]; then
        rm -rf "$TEMP_DIR"
    fi
}

# ============================================================================
# DEPENDENCY CHECK
# ============================================================================

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

# ============================================================================
# BUILD
# ============================================================================

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

# ============================================================================
# CONFIG
# ============================================================================

# create_config - Process config template with variable substitution
# Substitutes ${PORT} and any variables in DB_VARS associative array
create_config() {
    info "Creating config from template..."

    # Start with PORT substitution
    local sed_args=("-e" "s|\${PORT}|$PORT|g")

    # Add DB path substitutions if DB_VARS is set
    if declare -p DB_VARS &>/dev/null 2>&1; then
        for var in "${!DB_VARS[@]}"; do
            sed_args+=("-e" "s|\${$var}|${DB_VARS[$var]}|g")
        done
    fi

    sed "${sed_args[@]}" "$CONFIG_TEMPLATE" > "$CONFIG_FILE"
}

# ============================================================================
# SERVER
# ============================================================================

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
# COMMON TESTS
# ============================================================================

# test_health - Test health endpoint (common to all apps)
test_health() {
    header "Health Check"
    GET /_/health
    expect_status 200 "Health returns 200"
    expect_json '.status' 'healthy' "Health endpoint returns healthy"
}

# test_metrics - Test metrics endpoint (common to all apps)
test_metrics() {
    header "Metrics"
    GET /_/metrics
    expect_status 200 "Metrics returns 200"
    expect_contains "sqlproxy_" "Contains sqlproxy metrics"
    expect_contains "go_" "Contains go runtime metrics"
}

# test_openapi - Test OpenAPI endpoint (common to all apps)
test_openapi() {
    header "OpenAPI Spec"
    GET /_/openapi.json
    expect_status 200 "OpenAPI returns 200"
    expect_contains "openapi" "Contains openapi field"
    expect_contains "paths" "Contains paths"
}

# ============================================================================
# HEADER PRINTING
# ============================================================================

# print_test_header <app_name> <description>
print_test_header() {
    local app_name="$1"
    local description="$2"

    echo ""
    echo "========================================"
    echo " $description"
    echo "========================================"
    echo ""

    if [ "$COVERAGE_ENABLED" = true ]; then
        info "Coverage: ENABLED"
        info "Coverage dir: $COVERAGE_DIR"
    else
        info "Coverage: disabled (use --cover to enable)"
    fi
    echo ""
}

# print_coverage_info - Print coverage information at end of run
print_coverage_info() {
    if [ "$COVERAGE_ENABLED" = true ]; then
        echo ""
        info "Coverage data: $COVERAGE_DIR"
        info "Convert: go tool covdata textfmt -i=$COVERAGE_DIR -o=coverage.out"
    fi
}
