# Build stage - use native platform for faster builds
FROM --platform=$BUILDPLATFORM golang:1.24-alpine AS builder

WORKDIR /app

# Allow Go to auto-download required toolchain
ENV GOTOOLCHAIN=auto

# Install build dependencies including UPX for compression
RUN apk add --no-cache git ca-certificates upx

# Copy all module files first for better caching
COPY common/go.mod common/go.sum* ./common/
COPY common/tracing/go.mod common/tracing/go.sum* ./common/tracing/
COPY discovery/go.mod discovery/go.sum* ./discovery/
COPY gateway/go.mod gateway/go.sum* ./gateway/

# Download dependencies
WORKDIR /app/gateway
RUN go mod download

# Copy source code
WORKDIR /app
COPY common/ ./common/
COPY discovery/ ./discovery/
COPY gateway/ ./gateway/

# Build arguments for cross-compilation
ARG TARGETOS
ARG TARGETARCH

# Build the application with native cross-compilation
WORKDIR /app/gateway
RUN go mod tidy
RUN CGO_ENABLED=0 GOOS=${TARGETOS} GOARCH=${TARGETARCH} go build \
    -a \
    -installsuffix cgo \
    -ldflags="-s -w -extldflags '-static'" \
    -trimpath \
    -o gateway .

# Compress binary with UPX (50-70% size reduction)
RUN upx --best --lzma gateway

# Runtime stage - Distroless for minimal size and maximum security
FROM gcr.io/distroless/static-debian12:nonroot

WORKDIR /app

# Copy binary from builder
COPY --from=builder /app/gateway/gateway .

# Copy public directory for static files
COPY --from=builder /app/gateway/public ./public

# Expose port
EXPOSE 8080

# Run as non-root user (already set by distroless:nonroot)
USER nonroot:nonroot

# Run the application
ENTRYPOINT ["./gateway"]
