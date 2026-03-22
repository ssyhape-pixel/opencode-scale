# Build stage
FROM golang:1.25-alpine AS builder

RUN apk add --no-cache git ca-certificates

WORKDIR /src

# Cache dependencies
COPY go.mod go.sum ./
RUN go mod download

# Build all binaries
COPY . .
RUN CGO_ENABLED=0 GOOS=linux go build -ldflags="-s -w" -o /out/router         ./cmd/router
RUN CGO_ENABLED=0 GOOS=linux go build -ldflags="-s -w" -o /out/controller     ./cmd/controller
RUN CGO_ENABLED=0 GOOS=linux go build -ldflags="-s -w" -o /out/worker         ./cmd/worker
RUN CGO_ENABLED=0 GOOS=linux go build -ldflags="-s -w" -o /out/mock-opencode  ./cmd/mock-opencode
RUN CGO_ENABLED=0 GOOS=linux go build -ldflags="-s -w" -o /out/mock-llm-api  ./cmd/mock-llm-api

# Router image
FROM gcr.io/distroless/static-debian12 AS router
COPY --from=builder /out/router /router
ENTRYPOINT ["/router"]

# Controller image
FROM gcr.io/distroless/static-debian12 AS controller
COPY --from=builder /out/controller /controller
ENTRYPOINT ["/controller"]

# Worker image
FROM gcr.io/distroless/static-debian12 AS worker
COPY --from=builder /out/worker /worker
ENTRYPOINT ["/worker"]

# Mock OpenCode server image
FROM gcr.io/distroless/static-debian12 AS mock-opencode
COPY --from=builder /out/mock-opencode /mock-opencode
ENTRYPOINT ["/mock-opencode"]

# Mock LLM API image (rate limiting simulator)
FROM gcr.io/distroless/static-debian12 AS mock-llm-api
COPY --from=builder /out/mock-llm-api /mock-llm-api
ENTRYPOINT ["/mock-llm-api"]

# Default: all binaries
FROM gcr.io/distroless/static-debian12
COPY --from=builder /out/router /router
COPY --from=builder /out/controller /controller
COPY --from=builder /out/worker /worker
COPY --from=builder /out/mock-opencode /mock-opencode
COPY --from=builder /out/mock-llm-api /mock-llm-api
