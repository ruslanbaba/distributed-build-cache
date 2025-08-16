# Build variables
PROJECT_ID ?= your-gcp-project-id
REGION ?= us-central1
CLUSTER_NAME ?= build-cache-cluster
IMAGE_NAME = build-cache-server
IMAGE_TAG ?= $(shell git rev-parse --short HEAD)
REGISTRY = gcr.io/$(PROJECT_ID)

# Go variables
GO_VERSION = 1.21
GOOS ?= linux
GOARCH ?= amd64

# Kubernetes variables
NAMESPACE = build-cache
KUBECTL_CONTEXT ?= gke_$(PROJECT_ID)_$(REGION)_$(CLUSTER_NAME)

.PHONY: help build test docker-build docker-push deploy clean

help: ## Show this help message
	@echo "Available targets:"
	@awk 'BEGIN {FS = ":.*?## "} /^[a-zA-Z_-]+:.*?## / {printf "  %-20s %s\n", $$1, $$2}' $(MAKEFILE_LIST)

# Development targets
.PHONY: build
build: ## Build the Go binary
	CGO_ENABLED=0 GOOS=$(GOOS) GOARCH=$(GOARCH) go build \
		-ldflags="-w -s -X main.version=$(IMAGE_TAG) -X main.commit=$(shell git rev-parse HEAD) -X main.date=$(shell date -u +%Y-%m-%dT%H:%M:%SZ)" \
		-o bin/$(IMAGE_NAME) \
		./cmd/cache-server

.PHONY: test
test: ## Run all tests
	go test -v -race -coverprofile=coverage.out ./...
	go tool cover -html=coverage.out -o coverage.html

.PHONY: test-integration
test-integration: ## Run integration tests
	go test -v -tags=integration ./test/integration/...

.PHONY: lint
lint: ## Run linting
	golangci-lint run ./...

.PHONY: proto-gen
proto-gen: ## Generate protobuf code
	protoc --go_out=. --go_opt=paths=source_relative \
		--go-grpc_out=. --go-grpc_opt=paths=source_relative \
		api/proto/buildcache.proto

# Docker targets
.PHONY: docker-build
docker-build: ## Build Docker image
	docker build -t $(REGISTRY)/$(IMAGE_NAME):$(IMAGE_TAG) \
		--build-arg VERSION=$(IMAGE_TAG) \
		--build-arg COMMIT=$(shell git rev-parse HEAD) \
		--build-arg DATE=$(shell date -u +%Y-%m-%dT%H:%M:%SZ) \
		.
	docker tag $(REGISTRY)/$(IMAGE_NAME):$(IMAGE_TAG) $(REGISTRY)/$(IMAGE_NAME):latest

.PHONY: docker-push
docker-push: docker-build ## Push Docker image to registry
	docker push $(REGISTRY)/$(IMAGE_NAME):$(IMAGE_TAG)
	docker push $(REGISTRY)/$(IMAGE_NAME):latest

.PHONY: docker-scan
docker-scan: ## Scan Docker image for vulnerabilities
	docker scan $(REGISTRY)/$(IMAGE_NAME):$(IMAGE_TAG)

# Infrastructure targets
.PHONY: terraform-init
terraform-init: ## Initialize Terraform
	cd infrastructure/terraform && terraform init

.PHONY: terraform-plan
terraform-plan: ## Plan Terraform changes
	cd infrastructure/terraform && terraform plan \
		-var="project_id=$(PROJECT_ID)" \
		-var="region=$(REGION)" \
		-var="cluster_name=$(CLUSTER_NAME)"

.PHONY: terraform-apply
terraform-apply: ## Apply Terraform changes
	cd infrastructure/terraform && terraform apply \
		-var="project_id=$(PROJECT_ID)" \
		-var="region=$(REGION)" \
		-var="cluster_name=$(CLUSTER_NAME)" \
		-auto-approve

.PHONY: terraform-destroy
terraform-destroy: ## Destroy Terraform resources
	cd infrastructure/terraform && terraform destroy \
		-var="project_id=$(PROJECT_ID)" \
		-var="region=$(REGION)" \
		-var="cluster_name=$(CLUSTER_NAME)" \
		-auto-approve

# Kubernetes targets
.PHONY: k8s-context
k8s-context: ## Set kubectl context
	kubectl config use-context $(KUBECTL_CONTEXT)

.PHONY: k8s-namespace
k8s-namespace: ## Create namespace
	kubectl create namespace $(NAMESPACE) --dry-run=client -o yaml | kubectl apply -f -

.PHONY: deploy-staging
deploy-staging: docker-push k8s-context k8s-namespace ## Deploy to staging environment
	cd k8s/overlays/staging && \
	kustomize edit set image $(REGISTRY)/$(IMAGE_NAME)=$(REGISTRY)/$(IMAGE_NAME):$(IMAGE_TAG) && \
	kustomize build . | kubectl apply -f -

.PHONY: deploy-production
deploy-production: docker-push k8s-context k8s-namespace ## Deploy to production environment
	cd k8s/overlays/production && \
	kustomize edit set image $(REGISTRY)/$(IMAGE_NAME)=$(REGISTRY)/$(IMAGE_NAME):$(IMAGE_TAG) && \
	kustomize build . | kubectl apply -f -

.PHONY: rollback
rollback: ## Rollback deployment
	kubectl rollout undo deployment/build-cache-server -n $(NAMESPACE)

.PHONY: status
status: ## Check deployment status
	kubectl get pods,svc,ing -n $(NAMESPACE)
	kubectl rollout status deployment/build-cache-server -n $(NAMESPACE)

# Monitoring targets
.PHONY: monitor
monitor: ## Open monitoring dashboard
	kubectl port-forward -n monitoring svc/grafana 3000:80 &
	kubectl port-forward -n monitoring svc/prometheus-server 9090:80 &
	echo "Grafana: http://localhost:3000"
	echo "Prometheus: http://localhost:9090"

.PHONY: logs
logs: ## Show application logs
	kubectl logs -f deployment/build-cache-server -n $(NAMESPACE)

.PHONY: metrics
metrics: ## Show metrics endpoint
	kubectl port-forward -n $(NAMESPACE) svc/build-cache-server 9090:9090 &
	echo "Metrics: http://localhost:9090/metrics"

# Security targets
.PHONY: security-scan
security-scan: ## Run security scans
	gosec ./...
	nancy sleuth

.PHONY: vulnerability-scan
vulnerability-scan: ## Scan for vulnerabilities
	go list -json -deps ./... | nancy sleuth

# Cleanup targets
.PHONY: clean
clean: ## Clean build artifacts
	rm -rf bin/
	rm -f coverage.out coverage.html
	docker system prune -f

.PHONY: clean-k8s
clean-k8s: ## Clean Kubernetes resources
	kubectl delete namespace $(NAMESPACE) --ignore-not-found=true

# Load testing
.PHONY: load-test
load-test: ## Run load tests
	cd test/load && go run main.go -target=$(shell kubectl get svc build-cache-server -n $(NAMESPACE) -o jsonpath='{.status.loadBalancer.ingress[0].ip}'):8080

# Backup and restore
.PHONY: backup
backup: ## Backup cache data
	gsutil -m cp -r gs://$(PROJECT_ID)-build-cache ./backup-$(shell date +%Y%m%d-%H%M%S)

.PHONY: restore
restore: ## Restore cache data
	@echo "Usage: make restore BACKUP_DIR=backup-20231201-120000"
	@if [ -z "$(BACKUP_DIR)" ]; then echo "Please specify BACKUP_DIR"; exit 1; fi
	gsutil -m cp -r $(BACKUP_DIR)/* gs://$(PROJECT_ID)-build-cache/

# Database migration (if using SQL for metadata)
.PHONY: migrate-up
migrate-up: ## Run database migrations
	migrate -path migrations -database "$(DATABASE_URL)" up

.PHONY: migrate-down
migrate-down: ## Rollback database migrations
	migrate -path migrations -database "$(DATABASE_URL)" down
