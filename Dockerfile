# Build stage
FROM golang:1.26-alpine AS builder

# Install dependencies and create non-root user
RUN apk add --no-cache git ca-certificates tzdata && \
  adduser -D -u 1001 appuser

# Set working directory
WORKDIR /app

# Copy go mod and sum files
COPY go.mod go.sum ./

# Download dependencies
RUN go mod download

# Copy source code
COPY . .

# Build the application
RUN CGO_ENABLED=0 GOOS=linux go build \
  -ldflags="-s -w -extldflags '-static'" \
  -trimpath \
  -o main cmd/service/main.go

# Final stage
FROM scratch

# Copy user info for non-root execution
COPY --from=builder /etc/passwd /etc/passwd

# Copy ca-certificates for HTTPS requests
COPY --from=builder /etc/ssl/certs/ca-certificates.crt /etc/ssl/certs/

# Copy timezone data
COPY --from=builder /usr/share/zoneinfo /usr/share/zoneinfo

# Set working directory and copy binary
WORKDIR /app

COPY --from=builder /app/main .

# Drop to non-root user
USER appuser

# Expose port
EXPOSE ${PORT:-8080}

# Run the binary
ENTRYPOINT ["/app/main"]
