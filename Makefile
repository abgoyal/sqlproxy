# SQL Proxy Makefile

BINARY_NAME := sql-proxy
LDFLAGS := -ldflags '-s -w'

# Build output directory
BUILD_DIR := build

# Go parameters
GOCMD := go
GOBUILD := $(GOCMD) build
GOTEST := $(GOCMD) test
GOGET := $(GOCMD) get
GOMOD := $(GOCMD) mod

.PHONY: all build clean test validate run install deps tidy \
        build-linux build-windows build-darwin build-all \
        build-linux-arm64 build-darwin-arm64

# Default target
all: build

# Build for current platform
build:
	$(GOBUILD) $(LDFLAGS) -o $(BINARY_NAME) .

# Run tests
test:
	$(GOTEST) -v ./...

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
	@echo "  make test         Run tests"
	@echo "  make validate     Validate config.yaml"
	@echo "  make run          Build and run interactively"
	@echo "  make clean        Remove build artifacts"
	@echo "  make deps         Download dependencies"
	@echo "  make tidy         Tidy go.mod"
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
