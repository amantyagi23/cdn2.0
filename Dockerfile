# =========================
# Build stage
# =========================
FROM golang:1.25-alpine AS builder

WORKDIR /app

# Required for some Go dependencies
RUN apk add --no-cache git ca-certificates

# Cache dependencies
COPY go.mod go.sum ./
RUN go mod download

# Copy source
COPY . .

# Build binary
RUN CGO_ENABLED=0 GOOS=linux GOARCH=amd64 \
    go build -ldflags="-s -w" -o api-platform ./cmd/server


# =========================
# Runtime stage
# =========================
FROM alpine:3.22

WORKDIR /app

RUN apk add --no-cache ca-certificates tzdata

# Copy binary
COPY --from=builder /app/api-platform .

# Don't copy .env
# Environment variables are provided by docker-compose

EXPOSE 4500

CMD ["./api-platform"]