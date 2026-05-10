# Makefile for nano
# A lightweight AI-powered code generation and modification agent

.PHONY: build clean test lint fmt install help dev run deps check release docker tui-test \
         test-coverage test-coverage-html test-race benchmark \
         test-tui test-e2e test-all \
         e2e e2e-daemon e2e-client e2e-tui e2e-binary e2e-expert e2e-coverage \
        smoke smoke-tui \
        lint-check fmt-check vet check-all \
        deps-update deps-check \
        install-local uninstall \
        docker-build docker-run \
        clean-deps tools watch dev-setup quick-test gen info version \
        config-example config-check \
        run-debug run-tui run-daemon
.DEFAULT_GOAL := run

# ==================== Variables ====================
BINARY_NAME=nano
BUILD_DIR=bin
CMD_DIR=cmd/nano
PKG_LIST=$(shell go list ./...)

# Version and build info
VERSION?=$(shell git describe --tags --always --dirty 2>/dev/null || echo "dev")
BUILD_TIME=$(shell date -u '+%Y-%m-%d_%H:%M:%S')
COMMIT_HASH=$(shell git rev-parse --short HEAD 2>/dev/null || echo "unknown")

# Go build flags
LDFLAGS=-X github.com/nano-harness/nano-agent/pkg/version.Version=${VERSION} -X github.com/nano-harness/nano-agent/pkg/version.BuildTime=${BUILD_TIME} -X github.com/nano-harness/nano-agent/pkg/version.CommitHash=${COMMIT_HASH}
BUILD_FLAGS=-ldflags "${LDFLAGS}"

# Cross-compilation targets
PLATFORMS=linux/amd64 linux/arm64 darwin/amd64 darwin/arm64 windows/amd64

# ==================== Main Targets ====================

build: ## Build the application with version info
	@echo "Building $(BINARY_NAME)..."
	@mkdir -p $(BUILD_DIR)
	@go build $(BUILD_FLAGS) -o $(BUILD_DIR)/$(BINARY_NAME) ./$(CMD_DIR)
	@echo "Build completed: $(BUILD_DIR)/$(BINARY_NAME)"
	@echo "Version: ${VERSION}"
	@echo "Build time: ${BUILD_TIME}"
	@echo "Commit: ${COMMIT_HASH}"

dev: ## Quick development build without version info
	@echo "Building $(BINARY_NAME) for development..."
	@mkdir -p $(BUILD_DIR)
	@go build -o $(BUILD_DIR)/$(BINARY_NAME) ./$(CMD_DIR)
	@echo "Development build completed: $(BUILD_DIR)/$(BINARY_NAME)"

run: dev ## Build and run in TUI mode by default
	@echo "Running $(BINARY_NAME) in TUI mode..."
	@./$(BUILD_DIR)/$(BINARY_NAME) --tui

run-debug: dev ## Build and run with debug environment
	@echo "Running $(BINARY_NAME) with debug environment..."
	@NANO_VERBOSE=true ./$(BUILD_DIR)/$(BINARY_NAME)

run-tui: dev ## Build and run in TUI mode
	@echo "Running $(BINARY_NAME) in TUI mode..."
	@./$(BUILD_DIR)/$(BINARY_NAME) --tui

run-daemon: dev ## Build and run in daemon mode
	@echo "Running $(BINARY_NAME) in daemon mode..."
	@./$(BUILD_DIR)/$(BINARY_NAME) --daemon

# ==================== Testing ====================

test: ## Run unit tests (excludes e2e tests)
	@echo "Running unit tests..."
	@go test -v $(shell go list ./... | grep -v /e2e)

test-coverage: ## Run unit tests with coverage report
	@echo "Running unit tests with coverage..."
	@go test -v -cover $(shell go list ./... | grep -v /e2e)

test-coverage-html: ## Run unit tests with HTML coverage report
	@echo "Generating HTML coverage report..."
	@go test -coverprofile=coverage.out $(shell go list ./... | grep -v /e2e)
	@go tool cover -html=coverage.out -o coverage.html
	@echo "Coverage report generated: coverage.html"

test-race: ## Run unit tests with race condition detection
	@echo "Running unit tests with race detection..."
	@go test -v -race $(shell go list ./... | grep -v /e2e)

test-tui: ## Run Bubble Tea TUI component tests
	@echo "Running Bubble Tea TUI tests..."
	@go test -race ./pkg/ui/bubbletea/...

test-e2e: ## Run real PTY TUI e2e tests
	@echo "Running TUI PTY e2e tests..."
	@go test -race -tags=e2e -timeout=5m ./e2e/tui/...

test-all: test test-tui test-e2e ## Run unit, TUI, and e2e tests

benchmark: ## Run benchmarks
	@echo "Running benchmarks..."
	@go test -bench=. -benchmem ./...

# ==================== E2E Testing ====================

e2e: ## Run all end-to-end integration tests
	@echo "Running e2e tests..."
	@go test -v -tags=e2e -timeout 10m ./e2e/...

e2e-daemon: ## Run only daemon e2e tests
	@go test -v -tags=e2e -timeout 5m -run "TestDaemon" ./e2e/...

e2e-client: ## Run only client e2e tests
	@go test -v -tags=e2e -timeout 5m -run "TestClient" ./e2e/...

e2e-tui: ## Run only TUI e2e tests
	@go test -v -tags=e2e -timeout 5m -run "TestBubbleTea" ./e2e/...

e2e-binary: ## Run only binary mode e2e tests
	@go test -v -tags=e2e -timeout 5m -run "TestBinaryMode" ./e2e/...

e2e-expert: ## Run only sub-agent / expert e2e tests
	@go test -v -tags=e2e -timeout 10m -run "TestExpert|TestForkBatch|TestParallel|TestTrigger|TestExecution|TestLoading" ./e2e/...

e2e-coverage: ## Run e2e tests with coverage
	@go test -v -tags=e2e -timeout 10m -coverprofile=e2e-coverage.out -coverpkg=./pkg/... ./e2e/...

# ==================== Smoke Testing ====================

smoke: dev ## Run PTY smoke tests (requires nano binary)
	@echo "Running PTY smoke tests..."
	@go test -v -tags=smoke -timeout 5m ./smoke/...

smoke-tui: dev ## Run only TUI smoke tests
	@echo "Running TUI smoke tests..."
	@go test -v -tags=smoke -timeout 3m -run "TestSmoke_TUI" ./smoke/...

# ==================== Code Quality ====================

lint: ## Run linter and automatically fix issues
	@echo "Running linter and fixing issues..."
	@if command -v golangci-lint >/dev/null 2>&1; then \
		golangci-lint run --fix; \
	else \
		echo "golangci-lint not installed, running go vet instead"; \
		go vet ./...; \
	fi

lint-check: ## Run linter without fixing
	@echo "Running linter check..."
	@if command -v golangci-lint >/dev/null 2>&1; then \
		golangci-lint run; \
	else \
		echo "golangci-lint not installed, running go vet instead"; \
		go vet ./...; \
	fi

fmt: ## Format code
	@echo "Formatting code..."
	@go fmt ./...
	@if command -v goimports >/dev/null 2>&1; then \
		echo "Running goimports..."; \
		goimports -w .; \
	else \
		echo "goimports not installed, skipping. Run 'go install golang.org/x/tools/cmd/goimports@latest' to install."; \
	fi

fmt-check: ## Check if code is formatted
	@echo "Checking code formatting..."
	@test -z "$$(gofmt -l .)" || (echo "Code is not formatted. Run 'make fmt' to fix." && exit 1)

vet: ## Run go vet
	@echo "Running go vet..."
	@go vet ./...

check: fmt-check vet lint test ## Run all checks (format, vet, lint, unit tests)

check-all: check e2e ## Run all checks including e2e tests

# ==================== Dependencies ====================

deps: ## Install and tidy dependencies
	@echo "Installing dependencies..."
	@go mod download
	@go mod tidy

deps-update: ## Update dependencies
	@echo "Updating dependencies..."
	@go get -u ./...
	@go mod tidy

deps-check: ## Check for outdated dependencies
	@echo "Checking for outdated dependencies..."
	@go list -u -m all

# ==================== Installation ====================

install: build ## Install the application to GOPATH/bin
	@echo "Installing $(BINARY_NAME)..."
	@cp $(BUILD_DIR)/$(BINARY_NAME) $(GOPATH)/bin/

install-local: build ## Install the application to /usr/local/bin
	@echo "Installing $(BINARY_NAME) to /usr/local/bin..."
	@sudo cp $(BUILD_DIR)/$(BINARY_NAME) /usr/local/bin/

uninstall: ## Uninstall the application
	@echo "Uninstalling $(BINARY_NAME)..."
	@rm -f $(GOPATH)/bin/$(BINARY_NAME)
	@sudo rm -f /usr/local/bin/$(BINARY_NAME)

# ==================== Release ====================

release: clean test lint ## Build release versions for all platforms
	@echo "Building release versions..."
	@mkdir -p $(BUILD_DIR)/release
	@$(foreach platform,$(PLATFORMS), \
		echo "Building for $(platform)..."; \
		GOOS=$(word 1,$(subst /, ,$(platform))) GOARCH=$(word 2,$(subst /, ,$(platform))) \
		go build $(BUILD_FLAGS) -o $(BUILD_DIR)/release/$(BINARY_NAME)-$(platform) ./$(CMD_DIR); \
	)
	@echo "Release builds completed in $(BUILD_DIR)/release/"

# ==================== Docker ====================

docker-build: ## Build Docker image
	@echo "Building Docker image..."
	@docker build -t $(BINARY_NAME):$(VERSION) .

docker-run: docker-build ## Run Docker container
	@echo "Running Docker container..."
	@docker run -it --rm $(BINARY_NAME):$(VERSION)

# ==================== Maintenance ====================

clean: ## Clean build artifacts
	@echo "Cleaning..."
	@rm -rf $(BUILD_DIR)
	@rm -f coverage.out coverage.html
	@go clean

clean-deps: ## Clean dependency cache
	@echo "Cleaning dependency cache..."
	@go clean -modcache

# ==================== Development Tools ====================

tools: ## Install development tools
	@echo "Installing development tools..."
	@go install github.com/golangci/golangci-lint/cmd/golangci-lint@latest
	@go install github.com/goreleaser/goreleaser@latest

watch: ## Watch for file changes and rebuild (requires entr)
	@echo "Watching for changes (requires 'entr' to be installed)..."
	@find . -name "*.go" | entr -r make run

dev-setup: deps tools config-example ## Complete development environment setup
	@echo "Development environment setup complete!"
	@echo "Next steps:"
	@echo "1. Edit .nano.yaml with your API keys"
	@echo "2. Run 'make run' to start the application"

quick-test: dev ## Quick test run with a simple prompt
	@echo "Testing $(BINARY_NAME) with a simple prompt..."
	@echo "What is 2+2?" | ./$(BUILD_DIR)/$(BINARY_NAME)

# ==================== Code Generation ====================

gen: ## Generate code (if you have go:generate directives)
	@echo "Generating code..."
	@go generate ./...

# ==================== Information ====================

info: ## Show project information
	@echo "Project: nano"
	@echo "Version: $(VERSION)"
	@echo "Build Time: $(BUILD_TIME)"
	@echo "Commit: $(COMMIT_HASH)"
	@echo "Go Version: $(shell go version)"
	@echo "Build Dir: $(BUILD_DIR)"
	@echo "Binary: $(BINARY_NAME)"

version: ## Show version information
	@echo "$(VERSION)"

# ==================== Configuration ====================

config-example: ## Copy example configuration
	@echo "Copying example configuration..."
	@cp .nano.yaml.example .nano.yaml
	@echo "Configuration copied to .nano.yaml - please edit it with your settings"

config-check: ## Check configuration syntax
	@echo "Checking configuration syntax..."
	@if [ -f .nano.yaml ]; then \
		echo "Configuration file exists: .nano.yaml"; \
	else \
		echo "Configuration file not found. Run 'make config-example' to create one."; \
	fi

# ==================== Help ====================

help: ## Show this help message
	@echo "nano Build System"
	@echo "=========================="
	@echo ""
	@echo "Usage: make [target]"
	@echo ""
	@echo "Targets:"
	@awk 'BEGIN {FS = ":.*?## "} /^[a-zA-Z_-]+:.*?## / {printf "  %-20s %s\n", $$1, $$2}' $(MAKEFILE_LIST)
	@echo ""
	@echo "Examples:"
	@echo "  make build           # Build the application"
	@echo "  make dev             # Quick development build"
	@echo "  make run             # Build and run"
	@echo "  make run-tui         # Run in TUI mode"
	@echo "  make run-debug       # Run with debug environment"
	@echo "  make test            # Run tests"
	@echo "  make check           # Run all code quality checks"
	@echo "  make dev-setup       # Complete development setup"
	@echo "  make release         # Build for all platforms"
