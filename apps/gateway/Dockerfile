# K2 Gateway - WebRTC-SIP Gateway Dockerfile
# Multi-stage build for smaller image size

# Stage 1: Build
FROM golang:1.26.2-alpine AS builder

# Install build dependencies (opus-dev required for CGO inbound audio gain)
RUN apk add --no-cache git gcc musl-dev opus-dev pkgconf

# Set working directory
WORKDIR /build

# Copy go mod files from root context
COPY apps/gateway/go.mod apps/gateway/go.sum ./

# Download dependencies
RUN go mod download

# Install migration runner for one-shot deploy migration jobs.
RUN go install github.com/pressly/goose/v3/cmd/goose@v3.24.1

# Copy source code
COPY apps/gateway/ .

# Build the application (CGO required for libopus decode/encode)
# -ldflags="-w -s" to reduce binary size
RUN CGO_ENABLED=1 GOOS=linux go build -ldflags="-w -s" -o k2-gateway .

# Stage 2: Runtime
FROM alpine:latest

# Install ca-certificates for HTTPS/TLS and libopus runtime
RUN apk --no-cache add ca-certificates opus

WORKDIR /app

# Copy binary from builder
COPY --from=builder /build/k2-gateway .
COPY --from=builder /go/bin/goose .
COPY --from=builder /build/migrations ./migrations
COPY --from=builder /build/scripts/docker-entrypoint.sh .

# Copy .env file (optional - can be provided via volume mount)
# COPY .env .

# Build arguments for RTP port range (can be overridden at build time)
ARG RTP_PORT_MIN=10500
ARG RTP_PORT_MAX=10600

# Environment variables (can be overridden at runtime)
ENV RTP_PORT_MIN=${RTP_PORT_MIN}
ENV RTP_PORT_MAX=${RTP_PORT_MAX}

# Expose ports
# HTTP/WebSocket API
EXPOSE 8080/tcp

# SIP signaling
EXPOSE 5060/udp

# RTP media (configurable range, default 10500-10600)
# Note: EXPOSE is documentation only. Use -p flag or docker-compose ports for actual mapping.
# The actual port range is controlled by RTP_PORT_MIN and RTP_PORT_MAX environment variables.
EXPOSE ${RTP_PORT_MIN}-${RTP_PORT_MAX}/udp

# Create non-root user for security
RUN chmod +x /app/docker-entrypoint.sh && \
    addgroup -g 1000 k2 && \
    adduser -D -u 1000 -G k2 k2 && \
    chown -R k2:k2 /app

# Switch to non-root user
USER k2

# Health check (optional - check if process is running)
HEALTHCHECK --interval=30s --timeout=5s --start-period=5s --retries=3 \
    CMD pgrep k2-gateway || exit 1

# Run optional migrations, then the application
ENTRYPOINT ["./docker-entrypoint.sh"]
CMD ["./k2-gateway"]
