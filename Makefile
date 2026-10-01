BINARY   := antcd
IMAGE    ?= ghcr.io/ynotnauk/antcd
VERSION  ?= $(shell sed -n 's/^version: *//p' chart/Chart.yaml)
CONFIG   ?= config.yaml
LDFLAGS  := -w -s

.DEFAULT_GOAL := help

.PHONY: help build run test vet fmt lint tidy docker lint-chart cluster-up cluster-down kubeconfig clean

help: ## Show this help
	@awk 'BEGIN {FS = ":.*## "} /^[a-zA-Z_-]+:.*## / {printf "  \033[36m%-14s\033[0m %s\n", $$1, $$2}' $(MAKEFILE_LIST)

build: ## Build a static binary
	CGO_ENABLED=0 go build -ldflags="$(LDFLAGS)" -o $(BINARY) ./cmd/antcd

run: ## Run AntCD with CONFIG (default config.yaml)
	go run ./cmd/antcd --config $(CONFIG)

test: ## Run unit tests
	go test ./...

vet: ## Run go vet
	go vet ./...

fmt: ## Format Go code
	gofmt -w cmd internal

lint: vet ## Check formatting and vet
	@test -z "$$(gofmt -l cmd internal)" || { gofmt -l cmd internal; echo "gofmt needed"; exit 1; }

tidy: ## Tidy go.mod
	go mod tidy

docker: ## Build the container image
	docker build -t $(IMAGE):$(VERSION) .

lint-chart: ## Lint the Helm chart
	helm lint chart

cluster-up: ## Start the local K3s cluster
	docker compose up -d

cluster-down: ## Stop the local K3s cluster
	docker compose down

kubeconfig: ## Install the local K3s kubeconfig to ~/.kube/config
	mkdir -p ~/.kube
	cp ./k3s-data/kubeconfig.yaml ~/.kube/config
	sed -i 's/127.0.0.1/localhost/g' ~/.kube/config

clean: ## Remove build artefacts
	rm -f $(BINARY)
