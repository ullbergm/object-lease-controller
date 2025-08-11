IMG ?= ghcr.io/ullbergm/object-lease-controller:v0.2.3
PLUGIN_IMG ?= ghcr.io/ullbergm/object-lease-console-plugin:v0.2.3
# Container tool to use for building and pushing images
CONTAINER_TOOL ?= docker

run: build
	./bin/lease-controller -group startpunkt.ullberg.us -kind Application -version v1alpha2 -leader-elect -leader-elect-namespace default -opt-in-label-key "object-lease-controller.ullberg.us/enabled" -opt-in-label-value true

tidy:
	go mod tidy

fmt:
	go fmt ./...

vet:
	go vet ./...

test: tidy fmt vet
	go test ./... -race -coverprofile=coverage.out

build: tidy fmt vet test
	go build -o bin/lease-controller ./cmd/main.go

docker-build: build
	docker build -t $(IMG) .

docker-push: docker-build
	docker push $(IMG)

PLATFORMS ?= linux/arm64,linux/amd64
.PHONY: docker-buildx
docker-buildx: ## Build and push docker image for the manager for cross-platform support
	- $(CONTAINER_TOOL) buildx create --name project-v3-builder
	$(CONTAINER_TOOL) buildx use project-v3-builder
	- $(CONTAINER_TOOL) buildx build --push --platform=$(PLATFORMS) --tag ${IMG} -f Dockerfile .
	- $(CONTAINER_TOOL) buildx rm project-v3-builder

.PHONY: plugin-buildx
plugin-buildx: ## Build and push docker image for the plugin for cross-platform support
	- $(CONTAINER_TOOL) buildx create --name project-v3-builder
	$(CONTAINER_TOOL) buildx use project-v3-builder
	- $(CONTAINER_TOOL) buildx build --push --platform=$(PLATFORMS) --tag ${PLUGIN_IMG} object-lease-console-plugin
	- $(CONTAINER_TOOL) buildx rm project-v3-builder

# Console plugin
plugin-build:
	docker build -t $(PLUGIN_IMG) object-lease-console-plugin

plugin-push: plugin-build
	docker push $(PLUGIN_IMG)

deploy-operator-and-plugin:
	kubectl apply -k object-lease-operator/config/default

.PHONY: run tidy fmt vet test build docker-build docker-push plugin-build plugin-push deploy-operator-and-plugin

push-everything:
	$(MAKE) docker-push
	$(MAKE) plugin-push
	cd object-lease-operator && $(MAKE) docker-push && cd ..
	cd object-lease-operator && $(MAKE) catalog-push && cd ..
