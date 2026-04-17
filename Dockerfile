FROM golang:1.26-alpine AS builder

WORKDIR /app
COPY go.mod go.sum ./
RUN go mod download

COPY . .

# Build API
RUN CGO_ENABLED=0 GOOS=linux go build -ldflags="-s -w" -o /app/nms-api ./cmd/server

# Build Worker  
RUN CGO_ENABLED=0 GOOS=linux go build -ldflags="-s -w" -o /app/nms-worker ./cmd/worker

# Build TRAP-RECEIVER 
RUN CGO_ENABLED=0 GOOS=linux go build -ldflags="-s -w" -o /app/trap-receiver ./cmd/trap-receiver

FROM alpine:3.22
RUN apk --no-cache add tzdata ca-certificates net-snmp-tools su-exec && \
    addgroup -S nms && adduser -S -G nms -h /app -s /sbin/nologin nms && \
    mkdir -p /app/logs /app/static /app/templates /app/mibs && \
    chmod 755 /app/logs /app/static /app/templates /app/mibs

# Копируем ВСЕ бинарники
COPY --from=builder /app/nms-api /app/
COPY --from=builder /app/nms-worker /app/
COPY --from=builder /app/trap-receiver /app/ 

COPY config.yaml /app/
COPY static/ /app/static/
COPY templates/ /app/templates/
COPY mibs/ /app/mibs/

COPY scripts/docker-entrypoint-nms.sh /app/docker-entrypoint-nms.sh
RUN chmod 755 /app/docker-entrypoint-nms.sh

RUN chown -R nms:nms /app

WORKDIR /app
# Запуск от root только для entrypoint: chown bind-mount logs → su-exec nms (см. скрипт).
USER root
EXPOSE 8080 162/udp

ENTRYPOINT ["/app/docker-entrypoint-nms.sh"]
CMD ["./nms-api"]

HEALTHCHECK --interval=30s --timeout=3s --start-period=5s --retries=3 \
    CMD wget -q -O- http://localhost:8080/health || exit 1
