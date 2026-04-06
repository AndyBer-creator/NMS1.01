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

FROM alpine:latest
RUN apk --no-cache add tzdata ca-certificates net-snmp-tools && \
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

WORKDIR /app
EXPOSE 8080 162/udp

HEALTHCHECK --interval=30s --timeout=3s --start-period=5s --retries=3 \
    CMD wget -q -O- http://localhost:8080/health || exit 1
