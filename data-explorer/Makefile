APP_NAME := data-explorer
BIN_DIR := bin

.PHONY: help fmt lint gosec build test clean install-tools deps fix-imports

help:
	@echo "Available targets:"
	@echo "  fmt           - Format Go code with gofmt"
	@echo "  lint          - Run golangci-lint"
	@echo "  lint-fix      - Run golangci-lint with --fix to automatically fix issues"
	@echo "  gosec         - Run gosec security checks"
	@echo "  build         - Build the data-explorer binary"
	@echo "  test          - Run go test ./..."
	@echo "  clean         - Remove build artifacts"
	@echo "  deps          - Fetch Go module dependencies"
	@echo "  fix-imports   - Remove unused imports with goimports"
	@echo "  install-tools - Install linting/security tools (golangci-lint, gosec, goimports)"
	@echo "  release        - Clean and build for release"
	@echo "  docker-build   - Build the Docker image"
	@echo "  docker-run     - Run the Docker container"
	@echo "  start           - Build and run the Docker container"

fmt:
	@gofmt -w ./ 

lint:
	@golangci-lint run ./...

lint-fix:
	@golangci-lint run --fix ./...	

gosec:
	@gosec ./...

build:
	@mkdir -p $(BIN_DIR)
	@go build -o $(BIN_DIR)/$(APP_NAME) .

test:
	@go test ./... -v -timeout 10m

clean:
	@rm -rf $(BIN_DIR)

install-tools:
	@echo "Installing golangci-lint..."
	@go install github.com/golangci/golangci-lint/cmd/golangci-lint@latest
	@echo "Installing gosec..."
	@go install github.com/securego/gosec/v2/cmd/gosec@latest
	@echo "Installing goimports..."
	@go install golang.org/x/tools/cmd/goimports@latest

deps:
	@echo "Fetching Go module dependencies..."
	@go get github.com/ethereum/go-ethereum/crypto/kzg4844@v1.16.8
	@go get github.com/ethereum/go-ethereum/core/types@v1.16.8
	@go mod tidy

fix-imports:
	@echo "Running goimports to fix and remove unused imports..."
	@find . -name '*.go' -not -path "./vendor/*" -exec goimports -w {} +
	@echo "Done!"

release: clean build
	@echo "Release build completed. Binary is located at $(BIN_DIR)/$(APP_NAME)"	

docker-build:
	@docker compose build --no-cache

docker-run:
	@docker compose up -d

start: docker-build docker-run
	@echo "Data Explorer is starting up. It may take a few moments for the API to be ready."