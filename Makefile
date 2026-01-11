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
PKG_HANDLER := ./internal/handler/...
PKG_SCHEDULER := ./internal/scheduler/...
PKG_VALIDATE := ./internal/validate/...
PKG_SERVER := ./internal/server/...
PKG_LOGGING := ./internal/logging/...
PKG_METRICS := ./internal/metrics/...
PKG_OPENAPI := ./internal/openapi/...
PKG_SERVICE := ./internal/service/...
PKG_WEBHOOK := ./internal/webhook/...
PKG_CACHE := ./internal/cache/...

.PHONY: all build clean test validate run install deps tidy version \
        build-linux build-windows build-darwin build-all \
        build-linux-arm64 build-darwin-arm64 \
        test-config test-db test-handler test-scheduler test-validate \
        test-server test-logging test-metrics test-openapi test-webhook \
        test-unit test-integration test-e2e test-bench \
        test-cover test-cover-html test-cover-report test-docs

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

test-handler:
	$(GOTEST) -v $(PKG_HANDLER)

test-scheduler:
	$(GOTEST) -v $(PKG_SCHEDULER)

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

test-webhook:
	$(GOTEST) -v $(PKG_WEBHOOK)

test-cache:
	$(GOTEST) -v $(PKG_CACHE)

# Run unit tests only (exclude benchmarks and e2e)
test-unit:
	$(GOTEST) -v -run "^Test" ./internal/...

# Run integration tests (httptest-based, in-process)
test-integration:
	$(GOTEST) -v -run "Integration" ./internal/...

# Run end-to-end tests (starts actual binary)
test-e2e:
	$(GOTEST) -v ./e2e/...

# Run benchmarks
test-bench:
	$(GOTEST) -bench=. -benchmem ./internal/...

# Run benchmarks with short time (quick check)
test-bench-short:
	$(GOTEST) -bench=. -benchtime=100ms ./...

# ============================================================================
# Coverage targets
# ============================================================================

# Run tests with coverage summary
test-cover:
	@$(GOTEST) -cover ./internal/... 2>&1 | grep -v "internal/testutil"

# Generate coverage report (text) - excludes testutil
test-cover-report:
	@mkdir -p $(COVERAGE_DIR)
	$(GOTEST) -coverprofile=$(COVERAGE_DIR)/coverage.out ./internal/...
	@grep -v "internal/testutil" $(COVERAGE_DIR)/coverage.out > $(COVERAGE_DIR)/coverage.tmp && mv $(COVERAGE_DIR)/coverage.tmp $(COVERAGE_DIR)/coverage.out
	$(GOCMD) tool cover -func=$(COVERAGE_DIR)/coverage.out
	@echo ""
	@echo "Coverage report saved to $(COVERAGE_DIR)/coverage.out"

# Generate HTML coverage report - excludes testutil
test-cover-html:
	@mkdir -p $(COVERAGE_DIR)
	$(GOTEST) -coverprofile=$(COVERAGE_DIR)/coverage.out ./internal/...
	@grep -v "internal/testutil" $(COVERAGE_DIR)/coverage.out > $(COVERAGE_DIR)/coverage.tmp && mv $(COVERAGE_DIR)/coverage.tmp $(COVERAGE_DIR)/coverage.out
	$(GOCMD) tool cover -html=$(COVERAGE_DIR)/coverage.out -o $(COVERAGE_DIR)/coverage.html
	@echo "HTML coverage report: $(COVERAGE_DIR)/coverage.html"

# Generate per-package coverage reports
test-cover-packages:
	@mkdir -p $(COVERAGE_DIR)
	@echo "Generating per-package coverage..."
	@$(GOTEST) -coverprofile=$(COVERAGE_DIR)/config.out $(PKG_CONFIG)
	@$(GOTEST) -coverprofile=$(COVERAGE_DIR)/db.out $(PKG_DB)
	@$(GOTEST) -coverprofile=$(COVERAGE_DIR)/handler.out $(PKG_HANDLER)
	@$(GOTEST) -coverprofile=$(COVERAGE_DIR)/scheduler.out $(PKG_SCHEDULER)
	@$(GOTEST) -coverprofile=$(COVERAGE_DIR)/validate.out $(PKG_VALIDATE)
	@$(GOTEST) -coverprofile=$(COVERAGE_DIR)/server.out $(PKG_SERVER)
	@$(GOTEST) -coverprofile=$(COVERAGE_DIR)/logging.out $(PKG_LOGGING)
	@$(GOTEST) -coverprofile=$(COVERAGE_DIR)/metrics.out $(PKG_METRICS)
	@$(GOTEST) -coverprofile=$(COVERAGE_DIR)/openapi.out $(PKG_OPENAPI)
	@echo ""
	@echo "Per-package coverage reports saved to $(COVERAGE_DIR)/"

# Generate test documentation
test-docs:
	@./scripts/generate-test-docs.sh

# ============================================================================
# CI/CD targets
# ============================================================================

# Run full CI check (test + coverage threshold)
ci: test-cover-report
	@echo "Checking coverage thresholds..."
	@./scripts/check-coverage.sh $(COVERAGE_DIR)/coverage.out 70

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
	@echo "  make test-handler    Run handler package tests"
	@echo "  make test-scheduler  Run scheduler package tests"
	@echo "  make test-validate   Run validate package tests"
	@echo "  make test-server     Run server package tests"
	@echo "  make test-logging    Run logging package tests"
	@echo "  make test-metrics    Run metrics package tests"
	@echo "  make test-openapi    Run openapi package tests"
	@echo "  make test-webhook    Run webhook package tests"
	@echo "  make test-cache      Run cache package tests"
	@echo ""
	@echo "Testing by type:"
	@echo "  make test-unit        Run unit tests (internal packages)"
	@echo "  make test-integration Run integration tests (httptest-based)"
	@echo "  make test-e2e         Run end-to-end tests (starts binary)"
	@echo ""
	@echo "Benchmarks:"
	@echo "  make test-bench        Run all benchmarks"
	@echo "  make test-bench-short  Run benchmarks (quick)"
	@echo ""
	@echo "Coverage:"
	@echo "  make test-cover          Summary coverage for all packages"
	@echo "  make test-cover-report   Detailed coverage report (text)"
	@echo "  make test-cover-html     HTML coverage report"
	@echo "  make test-cover-packages Per-package coverage files"
	@echo ""
	@echo "Documentation:"
	@echo "  make test-docs    Generate test documentation"
	@echo ""
	@echo "CI/CD:"
	@echo "  make ci           Run tests with coverage threshold check"
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
