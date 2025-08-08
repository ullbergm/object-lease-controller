IMG ?= quay.io/ullbergm/object-lease-controller:latest

run: build
	./bin/lease-controller -group startpunkt.ullberg.us -kind Application -version v1alpha2 -leader-elect -leader-elect-namespace default -opt-in-label-key "object-lease-controller.ullberg.us/enabled" -opt-in-label-value true

tidy:
	go mod tidy

fmt:
	go fmt ./...

vet:
	go vet ./...

test: tidy fmt vet
	go test ./... -timeout 30s

build: tidy fmt vet test
	go build -o bin/lease-controller ./cmd/main.go

docker-build: build
	docker build -t $(IMG) .

docker-push:
	docker push $(IMG)

.PHONY: run tidy fmt vet test build docker-build docker-push
