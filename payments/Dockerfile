# Build stage - use native platform for faster builds
FROM --platform=$BUILDPLATFORM golang:1.24-alpine AS builder

WORKDIR /app

# Use local Go toolchain only (no auto-download)
ENV GOTOOLCHAIN=auto

# Install build dependencies including UPX for compression
RUN apk add --no-cache git ca-certificates upx

# Copy all module files first for better caching
COPY common/go.mod common/go.sum* ./common/
COPY common/tracing/go.mod common/tracing/go.sum* ./common/tracing/
COPY discovery/go.mod discovery/go.sum* ./discovery/
COPY payments/go.mod payments/go.sum* ./payments/

# Download dependencies
WORKDIR /app/payments
RUN go mod download

# Copy source code
WORKDIR /app
COPY common/ ./common/
COPY discovery/ ./discovery/
COPY payments/ ./payments/

# Build arguments for cross-compilation
ARG TARGETOS
ARG TARGETARCH

# Build the application with native cross-compilation
WORKDIR /app/payments
RUN go mod tidy
RUN CGO_ENABLED=0 GOOS=${TARGETOS} GOARCH=${TARGETARCH} go build \
    -a \
    -installsuffix cgo \
    -ldflags="-s -w -extldflags '-static'" \
    -trimpath \
    -o payments .

# Compress binary with UPX (50-70% size reduction)
RUN upx --best --lzma payments

# Runtime stage - Distroless for minimal size and maximum security
FROM gcr.io/distroless/static-debian12:nonroot

WORKDIR /app

# Copy binary from builder
COPY --from=builder /app/payments/payments .

# Expose gRPC and HTTP ports
EXPOSE 9002 8082

# Run as non-root user (already set by distroless:nonroot)
USER nonroot:nonroot

# Run the application
ENTRYPOINT ["./payments"]
