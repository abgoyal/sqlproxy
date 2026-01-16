# SQL Proxy Makefile

BINARY_NAME := sql-proxy

# Version info from git
GIT_TAG := $(shell git describe --tags --abbrev=0 2>/dev/null || echo "v0.0.0")
GIT_COMMIT := $(shell git rev-parse --short HEAD 2>/dev/null || echo "unknown")
GIT_DIRTY := $(shell git diff --quiet 2>/dev/null || echo "-dirty")
VERSION := $(GIT_TAG)-$(GIT_COMMIT)$(GIT_DIRTY)
BUILD_TIME := $(shell date -u '+%Y-%m-%dT%H:%M:%SZ')

# Build flags with version and build time injection
LDFLAGS := -ldflags '-s -w -X main.Version=$(VERSION) -X main.BuildTime=$(BUILD_TIME)'

# Build output directory
BUILD_DIR := build

# Coverage output directory
COVERAGE_DIR := coverage

# Go parameters
GOCMD := go
GOBUILD := $(GOCMD) build
GOTEST := $(GOCMD) test
GOGET := $(GOCMD) get
GOMOD := $(GOCMD) mod

# Test packages
PKG_CONFIG := ./internal/config/...
PKG_DB := ./internal/db/...
PKG_VALIDATE := ./internal/validate/...
PKG_SERVER := ./internal/server/...
PKG_LOGGING := ./internal/logging/...
PKG_METRICS := ./internal/metrics/...
PKG_OPENAPI := ./internal/openapi/...
PKG_SERVICE := ./internal/service/...
PKG_CACHE := ./internal/cache/...
PKG_TMPL := ./internal/tmpl/...
PKG_RATELIMIT := ./internal/ratelimit/...
PKG_WORKFLOW := ./internal/workflow/...
PKG_TYPES := ./internal/types/...

.PHONY: all build clean test validate run install deps tidy version \
        build-linux build-windows build-darwin build-all \
        build-linux-arm64 build-darwin-arm64 \
        test-config test-db test-validate \
        test-server test-logging test-metrics test-openapi \
        test-cache test-tmpl test-ratelimit test-workflow test-types \
        test-unit test-integration test-e2e test-bench \
        test-cover test-cover-report test-cover-packages test-clean test-docs ci

# Default target
all: build

# Show version info
version:
	@echo "Version: $(VERSION)"
	@echo "  Tag:    $(GIT_TAG)"
	@echo "  Commit: $(GIT_COMMIT)"
	@echo "  Dirty:  $(if $(GIT_DIRTY),yes,no)"
	@echo "  Built:  $(BUILD_TIME)"

# Build for current platform
build:
	@echo "Building $(BINARY_NAME) $(VERSION)"
	$(GOBUILD) $(LDFLAGS) -o $(BINARY_NAME) .

# ============================================================================
# Testing targets
# ============================================================================

# Run all tests
test:
	$(GOTEST) -v ./...

# Run all tests (short output)
test-short:
	$(GOTEST) ./...

# Run tests for individual packages
test-config:
	$(GOTEST) -v $(PKG_CONFIG)

test-db:
	$(GOTEST) -v $(PKG_DB)

test-validate:
	$(GOTEST) -v $(PKG_VALIDATE)

test-server:
	$(GOTEST) -v $(PKG_SERVER)

test-logging:
	$(GOTEST) -v $(PKG_LOGGING)

test-metrics:
	$(GOTEST) -v $(PKG_METRICS)

test-openapi:
	$(GOTEST) -v $(PKG_OPENAPI)

test-cache:
	$(GOTEST) -v $(PKG_CACHE)

test-tmpl:
	$(GOTEST) -v $(PKG_TMPL)

test-ratelimit:
	$(GOTEST) -v $(PKG_RATELIMIT)

test-workflow:
	$(GOTEST) -v $(PKG_WORKFLOW)

test-types:
	$(GOTEST) -v $(PKG_TYPES)

# Run unit tests only (exclude benchmarks and e2e)
test-unit:
	$(GOTEST) -v -run "^Test" ./internal/...

# Run integration tests (httptest-based, in-process)
test-integration:
	$(GOTEST) -v -run "Integration" ./internal/...

# Run end-to-end tests (shell script, human-friendly output)
test-e2e:
	./e2e/taskapp_test.sh

# Run benchmarks
test-bench:
	$(GOTEST) -bench=. -benchmem ./internal/...

# Run benchmarks with short time (quick check)
test-bench-short:
	$(GOTEST) -bench=. -benchtime=100ms ./...

# Generate benchmark report (saved to coverage directory)
test-bench-report:
	@mkdir -p $(COVERAGE_DIR)
	@echo "Running benchmarks and saving report..."
	$(GOTEST) -bench=. -benchmem -count=5 ./internal/tmpl/... > $(COVERAGE_DIR)/benchmark_tmpl.txt 2>&1
	$(GOTEST) -bench=. -benchmem -count=5 ./internal/ratelimit/... > $(COVERAGE_DIR)/benchmark_ratelimit.txt 2>&1
	$(GOTEST) -bench=. -benchmem -count=5 ./internal/cache/... > $(COVERAGE_DIR)/benchmark_cache.txt 2>&1
	$(GOTEST) -bench=. -benchmem -count=5 ./internal/metrics/... > $(COVERAGE_DIR)/benchmark_metrics.txt 2>&1
	$(GOTEST) -bench=. -benchmem -count=5 ./internal/workflow/... > $(COVERAGE_DIR)/benchmark_workflow.txt 2>&1
	@echo ""
	@echo "=== Benchmark Summary ==="
	@echo "Reports saved to $(COVERAGE_DIR)/benchmark_*.txt"
	@echo ""
	@echo "Template Engine (critical path):"
	@grep "BenchmarkEngine_CacheKey_Simple" $(COVERAGE_DIR)/benchmark_tmpl.txt | tail -1 || true
	@grep "BenchmarkEngine_RateLimit_Composite" $(COVERAGE_DIR)/benchmark_tmpl.txt | tail -1 || true
	@grep "BenchmarkEngine_Webhook_SimpleJSON" $(COVERAGE_DIR)/benchmark_tmpl.txt | tail -1 || true
	@echo ""
	@echo "Rate Limiting:"
	@grep "BenchmarkAllow-" $(COVERAGE_DIR)/benchmark_ratelimit.txt | tail -1 || true
	@echo ""
	@echo "Cache:"
	@grep "BenchmarkCache_GetSet" $(COVERAGE_DIR)/benchmark_cache.txt | tail -1 || true

# Compare benchmarks (requires benchstat: go install golang.org/x/perf/cmd/benchstat@latest)
test-bench-compare:
	@if ! command -v benchstat &> /dev/null; then \
		echo "benchstat not found. Install with: go install golang.org/x/perf/cmd/benchstat@latest"; \
		exit 1; \
	fi
	@mkdir -p $(COVERAGE_DIR)
	@echo "Running benchmarks for comparison (old)..."
	$(GOTEST) -bench=. -benchmem -count=5 ./internal/tmpl/... > $(COVERAGE_DIR)/bench_old.txt 2>&1
	@echo "Make your changes, then run: make test-bench-compare-new"

test-bench-compare-new:
	@if ! command -v benchstat &> /dev/null; then \
		echo "benchstat not found. Install with: go install golang.org/x/perf/cmd/benchstat@latest"; \
		exit 1; \
	fi
	@echo "Running benchmarks for comparison (new)..."
	$(GOTEST) -bench=. -benchmem -count=5 ./internal/tmpl/... > $(COVERAGE_DIR)/bench_new.txt 2>&1
	@echo ""
	@echo "=== Benchmark Comparison ==="
	benchstat $(COVERAGE_DIR)/bench_old.txt $(COVERAGE_DIR)/bench_new.txt

# ============================================================================
# Coverage targets
# ============================================================================

# Run all tests with coverage (unit + e2e) and show summary
# Unit tests output text format (-coverprofile), e2e outputs binary format (GOCOVERDIR).
# These are fundamentally different Go coverage mechanisms with no built-in merger,
# so we convert e2e to text and merge using a script.
#
# E2E tests use the shell script (e2e/taskapp_test.sh --cover) which:
# - Builds the binary with -cover flag
# - Runs the server with GOCOVERDIR to collect coverage
# - Tests all API endpoints comprehensively
test-cover:
	@mkdir -p $(COVERAGE_DIR)
	@rm -rf $(COVERAGE_DIR)/e2e
	@echo "=== Running unit tests with coverage ==="
	@$(GOTEST) -coverprofile=$(COVERAGE_DIR)/unit.out ./internal/...
	@grep -v "internal/testutil" $(COVERAGE_DIR)/unit.out > $(COVERAGE_DIR)/unit.tmp && mv $(COVERAGE_DIR)/unit.tmp $(COVERAGE_DIR)/unit.out
	@echo ""
	@echo "=== Running e2e tests with coverage ==="
	@./e2e/taskapp_test.sh --cover
	@echo ""
	@echo "=== Merging coverage data ==="
	@if [ -d "$(COVERAGE_DIR)/e2e" ] && [ "$$(ls -A $(COVERAGE_DIR)/e2e 2>/dev/null)" ]; then \
		$(GOCMD) tool covdata textfmt -i=$(COVERAGE_DIR)/e2e -o=$(COVERAGE_DIR)/e2e.out; \
		./scripts/merge-coverage.sh $(COVERAGE_DIR)/unit.out $(COVERAGE_DIR)/e2e.out > $(COVERAGE_DIR)/coverage.out; \
	else \
		cp $(COVERAGE_DIR)/unit.out $(COVERAGE_DIR)/coverage.out; \
		echo "Warning: No e2e coverage data found, using unit coverage only"; \
	fi
	@echo ""
	@$(GOCMD) tool cover -func=$(COVERAGE_DIR)/coverage.out | tail -1
	@echo "Full report: $(COVERAGE_DIR)/coverage.out"

# Generate detailed coverage report (per-function breakdown)
test-cover-report:
	@mkdir -p $(COVERAGE_DIR)
	@$(GOTEST) -coverprofile=$(COVERAGE_DIR)/coverage.out ./internal/...
	@grep -v "internal/testutil" $(COVERAGE_DIR)/coverage.out > $(COVERAGE_DIR)/coverage.tmp && mv $(COVERAGE_DIR)/coverage.tmp $(COVERAGE_DIR)/coverage.out
	@$(GOCMD) tool cover -func=$(COVERAGE_DIR)/coverage.out

# Generate per-package coverage reports
test-cover-packages:
	@mkdir -p $(COVERAGE_DIR)
	@echo "Generating per-package coverage..."
	@$(GOTEST) -coverprofile=$(COVERAGE_DIR)/config.out $(PKG_CONFIG)
	@$(GOTEST) -coverprofile=$(COVERAGE_DIR)/db.out $(PKG_DB)
	@$(GOTEST) -coverprofile=$(COVERAGE_DIR)/validate.out $(PKG_VALIDATE)
	@$(GOTEST) -coverprofile=$(COVERAGE_DIR)/server.out $(PKG_SERVER)
	@$(GOTEST) -coverprofile=$(COVERAGE_DIR)/logging.out $(PKG_LOGGING)
	@$(GOTEST) -coverprofile=$(COVERAGE_DIR)/metrics.out $(PKG_METRICS)
	@$(GOTEST) -coverprofile=$(COVERAGE_DIR)/openapi.out $(PKG_OPENAPI)
	@$(GOTEST) -coverprofile=$(COVERAGE_DIR)/tmpl.out $(PKG_TMPL)
	@$(GOTEST) -coverprofile=$(COVERAGE_DIR)/ratelimit.out $(PKG_RATELIMIT)
	@$(GOTEST) -coverprofile=$(COVERAGE_DIR)/workflow.out $(PKG_WORKFLOW)
	@$(GOTEST) -coverprofile=$(COVERAGE_DIR)/types.out $(PKG_TYPES)
	@echo ""
	@echo "Per-package coverage reports saved to $(COVERAGE_DIR)/"

# Generate test documentation
test-docs:
	@./scripts/generate-test-docs.sh

# ============================================================================
# CI/CD targets
# ============================================================================

# Run full CI check (unit + e2e tests with coverage threshold)
ci: test-cover
	@echo "Checking coverage thresholds..."
	@./scripts/check-coverage.sh $(COVERAGE_DIR)/coverage.out 70

# Clear Go's test cache (forces fresh test execution)
test-clean:
	$(GOCMD) clean -testcache

# Validate config
validate: build
	./$(BINARY_NAME) -validate -config config.yaml

# Run interactively
run: build
	./$(BINARY_NAME) -config config.yaml

# Clean build artifacts
clean:
	rm -f $(BINARY_NAME) $(BINARY_NAME).exe
	rm -rf $(BUILD_DIR)
	rm -rf $(COVERAGE_DIR)

# Download dependencies
deps:
	$(GOGET) -v ./...

# Tidy go.mod
tidy:
	$(GOMOD) tidy

# Cross-compilation targets
build-linux:
	GOOS=linux GOARCH=amd64 $(GOBUILD) $(LDFLAGS) -o $(BUILD_DIR)/$(BINARY_NAME)-linux-amd64 .

build-linux-arm64:
	GOOS=linux GOARCH=arm64 $(GOBUILD) $(LDFLAGS) -o $(BUILD_DIR)/$(BINARY_NAME)-linux-arm64 .

build-windows:
	GOOS=windows GOARCH=amd64 $(GOBUILD) $(LDFLAGS) -o $(BUILD_DIR)/$(BINARY_NAME)-windows-amd64.exe .

build-darwin:
	GOOS=darwin GOARCH=amd64 $(GOBUILD) $(LDFLAGS) -o $(BUILD_DIR)/$(BINARY_NAME)-darwin-amd64 .

build-darwin-arm64:
	GOOS=darwin GOARCH=arm64 $(GOBUILD) $(LDFLAGS) -o $(BUILD_DIR)/$(BINARY_NAME)-darwin-arm64 .

# Build all platforms
build-all: clean
	mkdir -p $(BUILD_DIR)
	$(MAKE) build-linux
	$(MAKE) build-linux-arm64
	$(MAKE) build-windows
	$(MAKE) build-darwin
	$(MAKE) build-darwin-arm64
	@echo "Built binaries:"
	@ls -lh $(BUILD_DIR)/

# Install to /usr/local/bin (Linux/macOS)
install: build
	sudo cp $(BINARY_NAME) /usr/local/bin/
	@echo "Installed to /usr/local/bin/$(BINARY_NAME)"

# Uninstall from /usr/local/bin
uninstall:
	sudo rm -f /usr/local/bin/$(BINARY_NAME)
	@echo "Removed /usr/local/bin/$(BINARY_NAME)"

# Show help
help:
	@echo "SQL Proxy Makefile"
	@echo ""
	@echo "Usage:"
	@echo "  make              Build for current platform"
	@echo "  make build        Build for current platform"
	@echo "  make test         Run all tests (verbose)"
	@echo "  make test-short   Run all tests (summary only)"
	@echo "  make validate     Validate config.yaml"
	@echo "  make run          Build and run interactively"
	@echo "  make clean        Remove build artifacts and coverage"
	@echo "  make deps         Download dependencies"
	@echo "  make tidy         Tidy go.mod"
	@echo ""
	@echo "Testing by package:"
	@echo "  make test-config     Run config package tests"
	@echo "  make test-db         Run db package tests"
	@echo "  make test-validate   Run validate package tests"
	@echo "  make test-server     Run server package tests"
	@echo "  make test-logging    Run logging package tests"
	@echo "  make test-metrics    Run metrics package tests"
	@echo "  make test-openapi    Run openapi package tests"
	@echo "  make test-cache      Run cache package tests"
	@echo "  make test-tmpl       Run tmpl package tests"
	@echo "  make test-ratelimit  Run ratelimit package tests"
	@echo "  make test-workflow   Run workflow package tests"
	@echo "  make test-types      Run types package tests"
	@echo ""
	@echo "Testing by type:"
	@echo "  make test-unit        Run unit tests (internal packages)"
	@echo "  make test-integration Run integration tests (httptest-based)"
	@echo "  make test-e2e         Run end-to-end tests (shell script)"
	@echo ""
	@echo "Benchmarks:"
	@echo "  make test-bench            Run all benchmarks"
	@echo "  make test-bench-short      Run benchmarks (quick)"
	@echo "  make test-bench-report     Generate benchmark reports"
	@echo "  make test-bench-compare    Baseline for A/B comparison"
	@echo "  make test-bench-compare-new Compare against baseline"
	@echo ""
	@echo "Coverage:"
	@echo "  make test-cover          Run all tests (unit + e2e) with coverage"
	@echo "  make test-cover-report   Per-function coverage breakdown"
	@echo "  make test-cover-packages Per-package coverage files"
	@echo "  make test-clean          Clear Go test cache"
	@echo ""
	@echo "Documentation:"
	@echo "  make test-docs    Generate test documentation"
	@echo ""
	@echo "CI/CD:"
	@echo "  make ci           Run all tests with coverage threshold check"
	@echo ""
	@echo "Cross-compilation:"
	@echo "  make build-linux        Linux amd64"
	@echo "  make build-linux-arm64  Linux arm64"
	@echo "  make build-windows      Windows amd64"
	@echo "  make build-darwin       macOS amd64 (Intel)"
	@echo "  make build-darwin-arm64 macOS arm64 (Apple Silicon)"
	@echo "  make build-all          All platforms"
	@echo ""
	@echo "Installation (Linux/macOS):"
	@echo "  make install      Install to /usr/local/bin"
	@echo "  make uninstall    Remove from /usr/local/bin"
