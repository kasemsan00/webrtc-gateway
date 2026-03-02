# K2 Gateway - WebRTC-SIP Gateway Dockerfile
# Multi-stage build for smaller image size

# Stage 1: Build
FROM golang:1.25.5-alpine AS builder

# Install build dependencies
RUN apk add --no-cache git

# Set working directory
WORKDIR /build

# Copy go mod files
COPY go.mod go.sum ./

# Download dependencies
RUN go mod download

# Copy source code
COPY . .

# Build the application
# CGO_ENABLED=0 for static binary
# -ldflags="-w -s" to reduce binary size
RUN CGO_ENABLED=0 GOOS=linux go build -a -installsuffix cgo -ldflags="-w -s" -o k2-gateway .

# Stage 2: Runtime
FROM alpine:latest

# Install ca-certificates for HTTPS/TLS
RUN apk --no-cache add ca-certificates

WORKDIR /app

# Copy binary from builder
COPY --from=builder /build/k2-gateway .

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
RUN addgroup -g 1000 k2 && \
    adduser -D -u 1000 -G k2 k2 && \
    chown -R k2:k2 /app

# Switch to non-root user
USER k2

# Health check (optional - check if process is running)
HEALTHCHECK --interval=30s --timeout=5s --start-period=5s --retries=3 \
    CMD pgrep k2-gateway || exit 1

# Run the application
CMD ["./k2-gateway"]
