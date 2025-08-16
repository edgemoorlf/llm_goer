# Build stage
FROM golang:1.21-alpine AS builder

# Install build dependencies
RUN apk add --no-cache git ca-certificates

# Set working directory
WORKDIR /app

# Copy go mod and sum files
COPY go.mod go.sum ./

# Download dependencies
RUN go mod download

# Copy source code
COPY . .

# Build the application
RUN CGO_ENABLED=1 GOOS=linux go build -a -installsuffix cgo -o proxy cmd/proxy/main.go

# Final stage
FROM alpine:latest

# Install runtime dependencies
RUN apk --no-cache add ca-certificates sqlite

# Create non-root user
RUN adduser -D -s /bin/sh proxyuser

# Set working directory
WORKDIR /app

# Copy binary from builder stage
COPY --from=builder /app/proxy .
COPY --from=builder /app/configs ./configs

# Create necessary directories
RUN mkdir -p logs data && \
    chown -R proxyuser:proxyuser /app

# Switch to non-root user
USER proxyuser

# Expose port
EXPOSE 8080

# Health check
HEALTHCHECK --interval=30s --timeout=10s --start-period=5s --retries=3 \
  CMD wget --no-verbose --tries=1 --spider http://localhost:8080/health || exit 1

# Run the application
CMD ["./proxy", "-config", "configs"]