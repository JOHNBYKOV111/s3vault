# cmd/redis-sentinel-check/Dockerfile
#
# Multi-stage сборка утилиты для chaos-теста Sentinel failover.
# Собирается в отдельный бинарник, не тянет за собой s3vault.
#
# Контекст сборки — корень проекта (context: ../.. в docker-compose.sentinel.yml),
# потому что нужны go.mod, go.sum и весь module.

# syntax=docker/dockerfile:1

# ---- Build stage ----
FROM golang:1.27-alpine AS builder

WORKDIR /src

# Кешируем зависимости отдельно от кода
COPY go.mod go.sum ./
RUN go mod download

# Копируем только то, что нужно для сборки клиента:
# сам пакет и его зависимости (internal не нужен — клиент самодостаточен).
COPY cmd/redis-sentinel-check/ ./cmd/redis-sentinel-check/

RUN CGO_ENABLED=0 GOOS=linux go build -trimpath -ldflags="-s -w" \
    -o /out/redis-sentinel-check ./cmd/redis-sentinel-check

# ---- Runtime stage ----
FROM alpine:3.20

RUN apk add --no-cache ca-certificates tzdata && \
    addgroup -S app && adduser -S -G app app

WORKDIR /app

COPY --from=builder /out/redis-sentinel-check /app/redis-sentinel-check

USER app

ENTRYPOINT ["/app/redis-sentinel-check"]