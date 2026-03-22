.PHONY: all build test lint clean docker-build docker-push deploy-dev deploy-prod fmt vet \
       build-mock-opencode build-mock-llm-api compose-up compose-down compose-logs \
       test-e2e compose-ratelimit bench compose-archival

VERSION ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo "dev")
REGISTRY ?= ghcr.io/opencode-scale
IMAGE_NAME ?= opencode-scale
IMAGE_TAG ?= $(VERSION)

GO := go
GOFLAGS := -ldflags="-s -w -X main.version=$(VERSION)"

all: fmt vet lint test build

## Build

build:
	$(GO) build $(GOFLAGS) -o bin/router         ./cmd/router
	$(GO) build $(GOFLAGS) -o bin/controller      ./cmd/controller
	$(GO) build $(GOFLAGS) -o bin/worker          ./cmd/worker
	$(GO) build $(GOFLAGS) -o bin/mock-opencode   ./cmd/mock-opencode
	$(GO) build $(GOFLAGS) -o bin/mock-llm-api    ./cmd/mock-llm-api

build-router:
	$(GO) build $(GOFLAGS) -o bin/router ./cmd/router

build-controller:
	$(GO) build $(GOFLAGS) -o bin/controller ./cmd/controller

build-worker:
	$(GO) build $(GOFLAGS) -o bin/worker ./cmd/worker

build-mock-opencode:
	$(GO) build $(GOFLAGS) -o bin/mock-opencode ./cmd/mock-opencode

build-mock-llm-api:
	$(GO) build $(GOFLAGS) -o bin/mock-llm-api ./cmd/mock-llm-api

## Test

test:
	$(GO) test -race -coverprofile=coverage.out ./...

test-short:
	$(GO) test -short ./...

test-e2e:
	bash hack/setup-local.sh
	$(GO) test -tags e2e -v -count=1 -timeout=5m ./test/e2e/
	docker compose down -v

coverage: test
	$(GO) tool cover -html=coverage.out -o coverage.html

## Quality

fmt:
	$(GO) fmt ./...

vet:
	$(GO) vet ./...

lint:
	@which golangci-lint > /dev/null 2>&1 || (echo "Install golangci-lint: https://golangci-lint.run/welcome/install/" && exit 1)
	golangci-lint run ./...

## Docker

docker-build:
	docker build -t $(REGISTRY)/$(IMAGE_NAME):$(IMAGE_TAG) .
	docker build --target router         -t $(REGISTRY)/$(IMAGE_NAME)-router:$(IMAGE_TAG) .
	docker build --target controller     -t $(REGISTRY)/$(IMAGE_NAME)-controller:$(IMAGE_TAG) .
	docker build --target worker         -t $(REGISTRY)/$(IMAGE_NAME)-worker:$(IMAGE_TAG) .
	docker build --target mock-opencode  -t $(REGISTRY)/$(IMAGE_NAME)-mock-opencode:$(IMAGE_TAG) .
	docker build --target mock-llm-api   -t $(REGISTRY)/$(IMAGE_NAME)-mock-llm-api:$(IMAGE_TAG) .

docker-push: docker-build
	docker push $(REGISTRY)/$(IMAGE_NAME):$(IMAGE_TAG)
	docker push $(REGISTRY)/$(IMAGE_NAME)-router:$(IMAGE_TAG)
	docker push $(REGISTRY)/$(IMAGE_NAME)-controller:$(IMAGE_TAG)
	docker push $(REGISTRY)/$(IMAGE_NAME)-worker:$(IMAGE_TAG)
	docker push $(REGISTRY)/$(IMAGE_NAME)-mock-opencode:$(IMAGE_TAG)
	docker push $(REGISTRY)/$(IMAGE_NAME)-mock-llm-api:$(IMAGE_TAG)

## Docker Compose (local development)

compose-up:
	docker compose up --build -d
	@echo "Waiting for services to be ready..."
	bash hack/setup-local.sh --wait-only

compose-down:
	docker compose down -v

compose-logs:
	docker compose logs -f

## Rate-limit testing (with LiteLLM + mock-llm-api)
compose-ratelimit:
	docker compose -f docker-compose.yaml -f docker-compose.ratelimit.yaml up --build -d
	@echo "Waiting for services..."
	bash hack/setup-local.sh --wait-only
	@echo ""
	@echo "Rate-limit env is up. Endpoints:"
	@echo "  Router:            http://localhost:8080"
	@echo "  LiteLLM:           http://localhost:4000"
	@echo "  Mock LLM API:      http://localhost:4099"
	@echo "  Rate limit debug:  http://localhost:4099/debug/rate-limits"
	@echo ""
	@echo "Run stress test:     make bench"

## Archival (S3/MinIO)
compose-archival:
	ARCHIVAL_ENABLED=true docker compose --profile archival up --build -d
	@echo "Waiting for Temporal..."
	@sleep 20
	docker compose exec -T temporal sh -c 'tctl --address $$(hostname -i):7233 --namespace default \
		namespace update \
		--retention 72h \
		--history_archival_state enabled \
		--visibility_archival_state enabled'
	@echo ""
	@echo "Archival environment ready!"
	@echo "  MinIO Console: http://localhost:9001 (minioadmin/minioadmin)"

## Stress test
bench:
	bash hack/bench.sh

## Deploy

deploy-dev:
	kubectl apply -k deploy/overlays/dev

deploy-prod:
	kubectl apply -k deploy/overlays/prod

## Local development

setup-local:
	bash hack/setup-local.sh

seed:
	bash hack/seed-data.sh

## Clean

clean:
	rm -rf bin/ coverage.out coverage.html

## Dependencies

deps:
	$(GO) mod download
	$(GO) mod tidy
