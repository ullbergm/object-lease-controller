IMG ?= quay.io/ullbergm/object-lease-controller:latest
PLUGIN_IMG ?= ghcr.io/ullbergm/object-lease-console-plugin:latest

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

docker-push:
	docker push $(IMG)

# Console plugin
plugin-build:
	docker build -t $(PLUGIN_IMG) console-plugin/object-lease-console-plugin

plugin-push: plugin-build
	docker push $(PLUGIN_IMG)

deploy-operator-and-plugin:
	kubectl apply -k object-lease-operator/config/default

.PHONY: run tidy fmt vet test build docker-build docker-push plugin-build plugin-push deploy-operator-and-plugin
