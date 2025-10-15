# Quick Watch Makefile

.PHONY: help build test clean version major minor build-release link unlink install

# Default target
help:
	@echo "Quick Watch - Available targets:"
	@echo ""
	@echo "  build          - Build the binary"
	@echo "  test           - Run tests"
	@echo "  clean          - Clean build artifacts"
	@echo "  version        - Show current version"
	@echo "  major <num>    - Update major version"
	@echo "  minor <num>    - Update minor version"
	@echo "  build-release  - Build release binary with current date"
	@echo "  link           - Create symlink in /usr/local/bin"
	@echo "  unlink         - Remove symlink from /usr/local/bin"
	@echo "  install        - Build and link (build + link)"
	@echo ""

# Build the binary
build:
	@echo "Building quick_watch..."
	go build -v -o quick_watch .

# Run tests
test:
	@echo "Running tests..."
	go test -v ./...

# Clean build artifacts
clean:
	@echo "Cleaning build artifacts..."
	rm -f quick_watch
	rm -f quick_watch-*.sha256
	rm -f quick_watch-v*-linux-amd64

# Version management
version:
	@./scripts/version.sh current

major:
	@./scripts/version.sh major $(filter-out $@,$(MAKECMDGOALS))

minor:
	@./scripts/version.sh minor $(filter-out $@,$(MAKECMDGOALS))

build-release:
	@./scripts/version.sh build

# Development helpers
dev: build
	@echo "Built for development. Run with: ./quick_watch"

fmt:
	@echo "Formatting code..."
	go fmt ./...

vet:
	@echo "Running go vet..."
	go vet ./...

lint: fmt vet
	@echo "Linting complete"

# Install dependencies
deps:
	@echo "Downloading dependencies..."
	go mod download
	go mod verify

# Allow make to pass arguments to targets
%:
	@:
