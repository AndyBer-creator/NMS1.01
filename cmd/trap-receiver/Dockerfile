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

FROM alpine:latest
RUN apk --no-cache add tzdata ca-certificates net-snmp
WORKDIR /app
COPY --from=builder /app/trap-receiver .
COPY config/ /app/config/
EXPOSE 162/udp
CMD ["./trap-receiver"]
