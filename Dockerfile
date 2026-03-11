# Production Dockerfile for service-provider-ksm
#
# Security: Uses minimal base image from SAP internal registry
# Multi-stage build for smaller final image

# Build stage (can use public Go image for building)
FROM golang:1.23-alpine AS builder
WORKDIR /workspace

# Copy go mod files
COPY go.mod go.sum ./
RUN go mod download

# Copy source code
COPY . .

# Build the binary
RUN CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build \
    -a -ldflags="-s -w" \
    -o service-provider-ksm \
    ./cmd/service-provider-ksm

# Production stage
# Using SAP internal registry for security and compliance
FROM crimson-prod.common.repositories.cloud.sap/distroless/base:nonroot-amd64

# Add labels for metadata
LABEL maintainer="SAP AICore CloudOps EU Team"
LABEL description="Service Provider for kube-state-metrics on OpenMCP"
LABEL org.opencontainers.image.source="https://github.com/dholeshu/service-provider-ksm"

WORKDIR /
COPY --from=builder /workspace/service-provider-ksm /service-provider-ksm

# Use non-root user (65532 = nonroot user in distroless)
USER 65532:65532

ENTRYPOINT ["/service-provider-ksm"]
