# Multi-stage build for optimal image size
FROM --platform=$BUILDPLATFORM golang:1.23.1-alpine AS builder

# Build arguments
ARG TARGETOS
ARG TARGETARCH
ARG VERSION=dev
ARG GIT_REV=unknown
ARG BUILD_DATE=unknown

# Install build dependencies
RUN apk add --no-cache git ca-certificates tzdata

# Set working directory
WORKDIR /src

# Copy go mod files first for better caching
COPY go.mod go.sum ./
RUN go mod download

# Copy source code
COPY . .

# Build the application
RUN CGO_ENABLED=0 GOOS=$TARGETOS GOARCH=$TARGETARCH \
    go build \
    -ldflags="-w -s -X main.version=${VERSION} -X main.commit=${GIT_REV} -X main.date=${BUILD_DATE}" \
    -a -tags=netgo \
    -o bloco-wallet-manager \
    ./cmd/blocowallet

# Final stage - minimal runtime image
FROM scratch

# Copy CA certificates for HTTPS requests
COPY --from=builder /etc/ssl/certs/ca-certificates.crt /etc/ssl/certs/

# Copy timezone data
COPY --from=builder /usr/share/zoneinfo /usr/share/zoneinfo

# Copy the binary
COPY --from=builder /src/bloco-wallet-manager /bloco-wallet-manager

# Set environment
ENV TZ=UTC

# Create a non-root user (note: this is just for metadata since we're using scratch)
USER 65534:65534

# Expose port (if needed for future features)
EXPOSE 8080

# Entry point
ENTRYPOINT ["/bloco-wallet-manager"]