# =============================================================================
# Configuration Variables
# =============================================================================

# Image configurations
IMG ?= ghcr.io/ullbergm/object-lease-controller:v1.0.0
PLUGIN_IMG ?= ghcr.io/ullbergm/object-lease-console-plugin:v1.0.0

# Build configurations
CONTAINER_TOOL ?= docker
PLATFORMS ?= linux/arm64,linux/amd64

# Application configurations
BINARY_NAME = lease-controller
BUILD_DIR = bin

# =============================================================================
# Development Targets
# =============================================================================

.PHONY: help
help: ## Display this help message
	@echo "Available targets:"
	@grep -E '^[a-zA-Z_-]+:.*?## .*$$' $(MAKEFILE_LIST) | sort | awk 'BEGIN {FS = ":.*?## "}; {printf "  %-20s %s\n", $$1, $$2}'


.PHONY: tidy
tidy: ## Clean up Go modules
	go mod tidy

.PHONY: fmt
fmt: ## Format Go code
	go fmt ./...

.PHONY: vet
vet: ## Vet Go code
	go vet ./...

.PHONY: test
test: tidy fmt vet ## Run tests with coverage
	go test ./... -race -coverprofile=coverage.out

.PHONY: build
build: tidy fmt vet test ## Build the binary
	go build -o $(BUILD_DIR)/$(BINARY_NAME) ./cmd/main.go

.PHONY: run
run: build ## Run the application locally
	./$(BUILD_DIR)/$(BINARY_NAME) \
		-group startpunkt.ullberg.us \
		-kind Application \
		-version v1alpha2 \
		-leader-elect \
		-leader-elect-namespace default \
		-opt-in-label-key "object-lease-controller.ullberg.io/enabled" \
		-opt-in-label-value true

# =============================================================================
# Docker Targets - Main Controller
# =============================================================================

.PHONY: docker-build
docker-build: build ## Build Docker image for main controller
	$(CONTAINER_TOOL) build -t $(IMG) .

.PHONY: docker-push
docker-push: docker-build ## Build and push Docker image for main controller
	$(CONTAINER_TOOL) push $(IMG)

.PHONY: docker-buildx
docker-buildx: ## Build and push multi-platform Docker image for main controller
	- $(CONTAINER_TOOL) buildx create --name project-v3-builder
	$(CONTAINER_TOOL) buildx use project-v3-builder
	- $(CONTAINER_TOOL) buildx build --push --platform=$(PLATFORMS) --tag $(IMG) -f Dockerfile .
	- $(CONTAINER_TOOL) buildx rm project-v3-builder

# =============================================================================
# Docker Targets - Console Plugin
# =============================================================================

.PHONY: plugin-build
plugin-build: ## Build Docker image for console plugin
	$(CONTAINER_TOOL) build -t $(PLUGIN_IMG) object-lease-console-plugin

.PHONY: plugin-push
plugin-push: plugin-build ## Build and push Docker image for console plugin
	$(CONTAINER_TOOL) push $(PLUGIN_IMG)

.PHONY: plugin-buildx
plugin-buildx: ## Build and push multi-platform Docker image for console plugin
	- $(CONTAINER_TOOL) buildx create --name project-v3-builder
	$(CONTAINER_TOOL) buildx use project-v3-builder
	- $(CONTAINER_TOOL) buildx build --push --platform=$(PLATFORMS) --tag $(PLUGIN_IMG) object-lease-console-plugin
	- $(CONTAINER_TOOL) buildx rm project-v3-builder

# =============================================================================
# Deployment Targets
# =============================================================================

.PHONY: deploy-operator-and-plugin
deploy-operator-and-plugin: ## Deploy operator and plugin to Kubernetes
	kubectl apply -k object-lease-operator/config/default

# =============================================================================
# Convenience Targets
# =============================================================================

.PHONY: push-all
push-all: ## Push all images and operator bundles
	$(MAKE) docker-push
	$(MAKE) plugin-push
	cd object-lease-operator && $(MAKE) docker-build docker-push bundle-push catalog-push && cd ..
