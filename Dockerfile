# 1) Builder stage

FROM golang:1.24-alpine AS builder

RUN apk add --no-cache git

WORKDIR /app

COPY go.mod go.sum ./
RUN go mod download

COPY . .
RUN CGO_ENABLED=0 \
    GOOS=linux \
    GOARCH=amd64 \
    go build -o tokentransfer .

# 2) Runtime stage

FROM alpine:3.18

RUN apk add --no-cache ca-certificates

WORKDIR /app

COPY --from=builder /app/tokentransfer .

COPY --from=builder /app/db/migrations ./db/migrations

COPY --from=builder /app/.env.example .env

EXPOSE 8080

CMD ["./tokentransfer"]
