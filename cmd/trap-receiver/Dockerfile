FROM golang:1.26-alpine AS builder

WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download

# ✅ Копируем код trap-receiver
COPY cmd/trap-receiver/ ./cmd/trap-receiver/
COPY cmd/**/*.go ./cmd/
COPY internal/ ./internal/

# Строим из cmd/trap-receiver
WORKDIR /src/cmd/trap-receiver
RUN CGO_ENABLED=0 GOOS=linux go build -ldflags="-s -w" -o /app/trap-receiver .

FROM alpine:3.21
RUN apk --no-cache add tzdata ca-certificates net-snmp
WORKDIR /app
COPY --from=builder /app/trap-receiver .
RUN addgroup -S nms && adduser -S -G nms -h /app -s /sbin/nologin nms && \
    mkdir -p /app/config /app/logs /tmp && \
    chown -R nms:nms /app /tmp
COPY config/critical_oids.json /app/config/critical_oids.json
USER nms:nms
EXPOSE 162/udp
CMD ["./trap-receiver"]
