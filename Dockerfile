FROM golang:1.25-alpine AS builder

WORKDIR /app
COPY go.mod go.sum ./
RUN go mod download

# ✅ Копируем ВСЕТКИЙ код проекта
COPY . .

# Build API
RUN CGO_ENABLED=0 GOOS=linux go build -ldflags="-s -w" \
    -o /app/nms-api ./cmd/server

# Build Worker  
RUN CGO_ENABLED=0 GOOS=linux go build -ldflags="-s -w" \
    -o /app/nms-worker ./cmd/worker

FROM alpine:latest
RUN apk --no-cache add tzdata ca-certificates && \
    mkdir -p /app/logs /app/static /app/templates /app/mibs && \
    chmod 755 /app/logs /app/static /app/templates /app/mibs

# ✅ Копируем готовые бинарники + static/templates
COPY --from=builder /app/nms-api /app/nms-api
COPY --from=builder /app/nms-worker /app/nms-worker
COPY config.yaml /app/config.yaml
COPY static/ /app/static/
COPY templates/ /app/templates/
COPY mibs/ /app/mibs/

# ✅ DEBUG: покажи файлы
RUN ls -la /app/static/ && echo "--- STATIC OK ---" && \
    ls -la /app/templates/ && echo "--- TEMPLATES OK ---"

WORKDIR /app
EXPOSE 8080

# Healthcheck: проверяем доступность HTTP эндпоинта.
HEALTHCHECK --interval=30s --timeout=3s --start-period=5s --retries=3 \
    CMD wget -q -O- http://localhost:8080/health || exit 1
