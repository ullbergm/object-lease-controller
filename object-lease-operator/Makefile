# ==============================================================================
# Project Configuration
# ==============================================================================

# Project version
VERSION ?= 0.2.6

# Image configuration
IMAGE_TAG_BASE ?= ghcr.io/ullbergm/object-lease-operator
IMG ?= ghcr.io/ullbergm/object-lease-operator-controller:v$(VERSION)

# Tool versions
OPERATOR_SDK_VERSION ?= v1.41.1
KUSTOMIZE_VERSION ?= v5.6.0
OPM_VERSION ?= v1.55.0

# Build configuration
CONTAINER_TOOL ?= docker
PLATFORMS ?= linux/arm64,linux/amd64,linux/s390x,linux/ppc64le
USE_IMAGE_DIGESTS ?= false

# Bundle configuration
ifneq ($(origin CHANNELS), undefined)
BUNDLE_CHANNELS := --channels=$(CHANNELS)
endif
ifneq ($(origin DEFAULT_CHANNEL), undefined)
BUNDLE_DEFAULT_CHANNEL := --default-channel=$(DEFAULT_CHANNEL)
endif
BUNDLE_METADATA_OPTS ?= $(BUNDLE_CHANNELS) $(BUNDLE_DEFAULT_CHANNEL)
BUNDLE_IMG ?= $(IMAGE_TAG_BASE)-bundle:v$(VERSION)
BUNDLE_GEN_FLAGS ?= -q --overwrite --version $(VERSION) $(BUNDLE_METADATA_OPTS)
ifeq ($(USE_IMAGE_DIGESTS), true)
	BUNDLE_GEN_FLAGS += --use-image-digests
endif

# Catalog configuration
PACKAGE ?= object-lease-operator
CHANNEL ?= alpha
DEFAULT_CHANNEL ?= $(CHANNEL)
BUNDLE_IMGS ?= $(BUNDLE_IMG),$(IMAGE_TAG_BASE)-bundle:v0.2.3,$(IMAGE_TAG_BASE)-bundle:v0.2.4,$(IMAGE_TAG_BASE)-bundle:v0.2.5
CATALOG_IMG ?= $(IMAGE_TAG_BASE)-catalog:latest
CATALOG_DIR ?= catalog
FBC_FILE ?= $(CATALOG_DIR)/index.yaml
CATALOG_DOCKERFILE ?= Dockerfile.catalog

# System detection
OS := $(shell uname -s | tr '[:upper:]' '[:lower:]')
ARCH := $(shell uname -m | sed 's/x86_64/amd64/' | sed 's/aarch64/arm64/')

# Local binary directory
LOCALBIN ?= $(shell pwd)/bin

# Policy configuration for catalog operations
POLICY_JSON ?= $(LOCALBIN)/policy.json

# Helper variables
COMMA := ,
BUNDLE_LIST := $(strip $(subst $(COMMA), ,$(BUNDLE_IMGS)))
BUNDLE_NAME ?= $(PACKAGE).v$(VERSION)

# ==============================================================================
# Default Target and Help
# ==============================================================================

.PHONY: all
all: docker-build

.PHONY: help
help: ## Display this help
	@awk 'BEGIN {FS = ":.*##"; printf "\nUsage:\n  make \033[36m<target>\033[0m\n"} /^[a-zA-Z_0-9-]+:.*?##/ { printf "  \033[36m%-22s\033[0m %s\n", $$1, $$2 } /^##@/ { printf "\n\033[1m%s\033[0m\n", substr($$0, 5) } ' $(MAKEFILE_LIST)

# ==============================================================================

# Build Targets
# ==============================================================================

.PHONY: run
run: helm-operator ## Run the operator against kubeconfig
	$(HELM_OPERATOR) run

.PHONY: docker-build
docker-build: ## Build the controller image
	$(CONTAINER_TOOL) build -t $(IMG) .

.PHONY: docker-push
docker-push: ## Push the controller image
	$(CONTAINER_TOOL) push $(IMG)

.PHONY: docker-buildx
docker-buildx: ## Build and push controller image for multiple platforms
	- $(CONTAINER_TOOL) buildx create --name project-v3-builder
	$(CONTAINER_TOOL) buildx use project-v3-builder
	- $(CONTAINER_TOOL) buildx build --push --platform=$(PLATFORMS) --tag $(IMG) -f Dockerfile .
	- $(CONTAINER_TOOL) buildx rm project-v3-builder

# ==============================================================================

# Deployment Targets
# ==============================================================================

.PHONY: install
install: kustomize ## Install CRDs into the cluster
	$(KUSTOMIZE) build config/crd | kubectl apply -f -

.PHONY: uninstall
uninstall: kustomize ## Uninstall CRDs from the cluster
	$(KUSTOMIZE) build config/crd | kubectl delete -f -

.PHONY: deploy
deploy: kustomize ## Deploy controller to the cluster
	cd config/manager && $(KUSTOMIZE) edit set image controller=$(IMG)
	$(KUSTOMIZE) build config/default | kubectl apply -f -

.PHONY: undeploy
undeploy: kustomize ## Undeploy controller from the cluster
	$(KUSTOMIZE) build config/default | kubectl delete -f -

# ==============================================================================

# Tool Installation Targets
# ==============================================================================

.PHONY: kustomize
KUSTOMIZE = $(LOCALBIN)/kustomize
kustomize: ## Download kustomize locally if needed
ifeq (,$(wildcard $(KUSTOMIZE)))
ifeq (,$(shell which kustomize 2>/dev/null))
	@{ set -e ;\
	mkdir -p $(dir $(KUSTOMIZE)) ;\
	curl -sSLo - https://github.com/kubernetes-sigs/kustomize/releases/download/kustomize/$(KUSTOMIZE_VERSION)/kustomize_$(KUSTOMIZE_VERSION)_$(OS)_$(ARCH).tar.gz | tar xzf - -C $(LOCALBIN) ;}
else
KUSTOMIZE = $(shell which kustomize)
endif
endif

.PHONY: helm-operator
HELM_OPERATOR = $(LOCALBIN)/helm-operator
helm-operator: ## Download helm-operator locally if needed
ifeq (,$(wildcard $(HELM_OPERATOR)))
ifeq (,$(shell which helm-operator 2>/dev/null))
	@{ set -e ;\
	mkdir -p $(dir $(HELM_OPERATOR)) ;\
	curl -sSLo $(HELM_OPERATOR) https://github.com/operator-framework/operator-sdk/releases/download/$(OPERATOR_SDK_VERSION)/helm-operator_$(OS)_$(ARCH) ;\
	chmod +x $(HELM_OPERATOR) ;}
else
HELM_OPERATOR = $(shell which helm-operator)
endif
endif

.PHONY: operator-sdk
OPERATOR_SDK = $(LOCALBIN)/operator-sdk
operator-sdk: ## Download operator-sdk locally if needed
ifeq (,$(wildcard $(OPERATOR_SDK)))
ifeq (, $(shell which operator-sdk 2>/dev/null))
	@{ set -e ;\
	mkdir -p $(dir $(OPERATOR_SDK)) ;\
	curl -sSLo $(OPERATOR_SDK) https://github.com/operator-framework/operator-sdk/releases/download/$(OPERATOR_SDK_VERSION)/operator-sdk_$(OS)_$(ARCH) ;\
	chmod +x $(OPERATOR_SDK) ;}
else
OPERATOR_SDK = $(shell which operator-sdk)
endif
endif

.PHONY: opm
OPM = $(LOCALBIN)/opm
opm: ## Download opm locally if needed
ifeq (,$(wildcard $(OPM)))
ifeq (,$(shell which opm 2>/dev/null))
	@{ set -e ;\
	mkdir -p $(dir $(OPM)) ;\
	curl -sSLo $(OPM) https://github.com/operator-framework/operator-registry/releases/download/$(OPM_VERSION)/$(OS)-$(ARCH)-opm ;\
	chmod +x $(OPM) ;}
else
OPM = $(shell which opm)
endif
endif

# ==============================================================================

# Bundle Targets
# ==============================================================================

.PHONY: bundle
bundle: kustomize operator-sdk ## Generate and validate bundle manifests
	$(OPERATOR_SDK) generate kustomize manifests -q
	cd config/manager && $(KUSTOMIZE) edit set image controller=$(IMG)
	$(KUSTOMIZE) build config/manifests | $(OPERATOR_SDK) generate bundle $(BUNDLE_GEN_FLAGS)
	$(OPERATOR_SDK) bundle validate ./bundle

.PHONY: bundle-build
bundle-build: bundle ## Build the bundle image
	$(CONTAINER_TOOL) build -f bundle.Dockerfile -t $(BUNDLE_IMG) .

.PHONY: bundle-push
bundle-push: bundle-build ## Push the bundle image
	$(MAKE) docker-push IMG=$(BUNDLE_IMG)

# ==============================================================================

# File-based Catalog (FBC) Targets
# ==============================================================================

.PHONY: containers-policy
containers-policy: ## Create containers policy file for catalog operations
	@mkdir -p $(dir $(POLICY_JSON))
	@printf '{ "default": [ { "type": "insecureAcceptAnything" } ] }\n' > $(POLICY_JSON)

$(CATALOG_DOCKERFILE): ## Generate Dockerfile for catalog image
	@{ \
	  echo 'FROM quay.io/operator-framework/opm:$(OPM_VERSION)'; \
	  echo 'LABEL operators.operatorframework.io.index.configs.v1=/configs'; \
	  echo 'COPY $(CATALOG_DIR)/ /configs/'; \
	  echo 'EXPOSE 50051'; \
	  echo 'ENTRYPOINT ["/bin/opm"]'; \
	  echo 'CMD ["serve","/configs","--cache-dir=/tmp/cache","--cache-enforce-integrity=false","--port=50051"]'; \
	} > $(CATALOG_DOCKERFILE)

.PHONY: catalog-fbc
catalog-fbc: opm containers-policy ## Generate file-based catalog
	@mkdir -p $(CATALOG_DIR)
	CONTAINERS_POLICY=$(POLICY_JSON) $(OPM) render $(BUNDLE_LIST) --output=yaml > $(CATALOG_DIR)/raw.yaml
	# Extract bundle documents only
	@awk 'BEGIN{RS="---\n"; ORS=""} /schema:[[:space:]]*olm\.bundle/ {print "---\n" $$0}' \
		$(CATALOG_DIR)/raw.yaml > $(FBC_FILE)
	# Generate channel entries from rendered bundle names
	@PKG="$(PACKAGE)"; \
	VERS=$$(awk -v PKG="$$PKG" 'BEGIN{RS="---\n"} /schema:[[:space:]]*olm\.bundle/ { \
	  if (match($$0, /(^|\n)[[:space:]]*name:[[:space:]]*"?([^"\n]+)"?/, m)) { \
	    n=m[2]; if (index(n, PKG ".v")==1) print substr(n, length(PKG)+3); }}' \
	  $(FBC_FILE) | sort -V); \
	test -n "$$VERS"; \
	{ \
	  echo "---"; \
	  echo "schema: olm.package"; \
	  echo "name: $(PACKAGE)"; \
	  echo "defaultChannel: $(DEFAULT_CHANNEL)"; \
	  echo "---"; \
	  echo "schema: olm.channel"; \
	  echo "name: $(CHANNEL)"; \
	  echo "package: $(PACKAGE)"; \
	  echo "entries:"; \
	  prev=""; \
	  for v in $$VERS; do \
	    name="$(PACKAGE).v$$v"; \
	    if [ -n "$$prev" ]; then \
	      echo "  - name: $$name"; \
	      echo "    replaces: $(PACKAGE).v$$prev"; \
	    else \
	      echo "  - name: $$name"; \
	    fi; \
	    prev="$$v"; \
	  done; \
	} >> $(FBC_FILE)
	@rm -f $(CATALOG_DIR)/raw.yaml
	$(OPM) validate $(CATALOG_DIR)

.PHONY: catalog-build
catalog-build: catalog-fbc $(CATALOG_DOCKERFILE) ## Build catalog image from FBC
	$(CONTAINER_TOOL) build -f $(CATALOG_DOCKERFILE) -t $(CATALOG_IMG) .

.PHONY: catalog-push
catalog-push: catalog-build ## Push catalog image
	$(MAKE) docker-push IMG=$(CATALOG_IMG)

.PHONY: catalog-clean
catalog-clean: ## Remove generated FBC artifacts
	@rm -rf $(CATALOG_DIR) $(CATALOG_DOCKERFILE)
