.PHONY: build docker-build docker-push test lint clean help

help: ## Show help
	@grep -E '^[a-zA-Z_-]+:.*?## ' Makefile | sort | awk 'BEGIN {FS = ":.*?## "}; {printf "\033[36m%-30s\033[0m %s\n", $$1, $$2}'

build: ## Build the pruner binary
	GOOS=linux GOARCH=amd64 go build -o bin/pruner ./cmd/pruner

test: ## Run tests
	go test -v -race -coverprofile=coverage.out ./...

test-coverage: test ## Generate test coverage report
	go tool cover -html=coverage.out -o coverage.html

lint: ## Run linting
	golangci-lint run ./...

fmt: ## Format code
	go fmt ./...
	go mod tidy

docker-build: ## Build Docker image
	docker build -t $(IMG) .

docker-push: ## Push Docker image
	docker push $(IMG)

clean: ## Clean build artifacts
	rm -rf bin/
	rm -f coverage.out coverage.html

.DEFAULT_GOAL := help
