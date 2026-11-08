# syntax=docker/dockerfile:1

# ---- Build stage ----
# Версия Go должна быть >= версии из go.mod (1.27.3).
# Тег 1.27-alpine указывает на последнюю 1.27.x.
FROM golang:1.27-alpine AS builder

WORKDIR /src

# Кешируем зависимости отдельно от кода
COPY go.mod go.sum ./
RUN go mod download

# Копируем исходники и собираем статический бинарник
COPY . .
RUN CGO_ENABLED=0 GOOS=linux go build -trimpath -ldflags="-s -w" -o /out/s3vault ./cmd/server

# ---- Runtime stage ----
FROM alpine:3.20

RUN apk add --no-cache ca-certificates tzdata && \
    addgroup -S app && adduser -S -G app app

WORKDIR /app

COPY --from=builder /out/s3vault /app/s3vault

# Директории для blob-хранилища (ReplicaStore: ./data и ./data-replica)
RUN mkdir -p /app/data /app/data-replica && chown -R app:app /app

USER app

EXPOSE 9000

ENTRYPOINT ["/app/s3vault"]