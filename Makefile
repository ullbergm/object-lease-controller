IMG ?= quay.io/ullbergm/object-lease-controller:latest

run: build
	./bin/lease-controller -group startpunkt.ullberg.us -kind Application -version v1alpha2 -leader-elect -leader-elect-namespace default

tidy:
	go mod tidy

fmt:
	go fmt ./...

vet:
	go vet ./...

build: tidy fmt vet
	go build -o bin/lease-controller ./cmd/main.go

docker-build: build
	docker build -t $(IMG) .

docker-push:
	docker push $(IMG)

.PHONY: build docker-build docker-push
