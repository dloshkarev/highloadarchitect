FROM golang:1.27.1-alpine AS builder

WORKDIR /app

COPY go.mod go.sum ./
RUN go mod download

COPY . .

RUN CGO_ENABLED=0 GOOS=linux go build -o /app/highloadarchitect ./cmd/app

FROM alpine:3.22

RUN adduser -D -g '' appuser

COPY --from=builder /app/highloadarchitect /app/highloadarchitect
COPY --from=builder /app/config /app/config

ENV CONFIG_PATH=/app/config/config.yaml

USER appuser

ENTRYPOINT ["/app/highloadarchitect"]
