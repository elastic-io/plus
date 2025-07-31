# Build stage
FROM golang:1.24 AS builder

# Set working directory
WORKDIR /build

# Install build dependencies
RUN apk add --no-cache git make

# Copy go.mod and go.sum files first for better caching
COPY go.mod go.sum ./
RUN go mod download

# Copy the source code
COPY . .

# Build the application
RUN make build-amd

# Final stage
FROM alpine:3.18

# Install runtime dependencies
RUN apk add --no-cache ca-certificates tzdata

# Set working directory
WORKDIR /app

# Copy the binary from the builder stage
COPY --from=builder /build/bin/plus-amd /app/plus

# Copy configuration files
COPY config.yaml /app/config.yaml

# Create storage directory
RUN mkdir -p /app/storage && \
    chmod -R 755 /app/storage

# Expose the application port
EXPOSE 8080

# Set environment variables
ENV TZ=UTC \
    GIN_MODE=release

# Set the entrypoint
ENTRYPOINT ["/app/plus"]
CMD ["--config", "config.yaml"]

# Add metadata
LABEL org.opencontainers.image.title="Plus" \
      org.opencontainers.image.description="Plus - A modern package repository manager" \
      org.opencontainers.image.source="https://github.com/elastic-io/plus" \
      org.opencontainers.image.vendor="elastic-io" \
      org.opencontainers.image.licenses="MIT"