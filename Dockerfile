# Build stage
FROM golang:1.25-alpine AS builder

WORKDIR /app

# Copy go mod files
COPY go.mod go.sum ./

# Download dependencies
RUN go mod download

# Copy source
COPY . .

# Install Templ
RUN go install github.com/a-h/templ/cmd/templ@latest

# Generate Templ components
RUN templ generate ./internal/ui

# Build binary
RUN CGO_ENABLED=0 GOOS=linux go build -ldflags="-s -w" -o ./bin/yieldi ./cmd/server

# Runtime stage
FROM alpine:latest

RUN apk --no-cache add ca-certificates

WORKDIR /app

# Copy binary from builder
COPY --from=builder /app/bin/yieldi .
COPY --from=builder /app/static ./static

EXPOSE 8080

ENTRYPOINT ["./yieldi"]
