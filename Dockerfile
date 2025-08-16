FROM golang:1.21-alpine AS builder

# Install build dependencies
RUN apk add --no-cache git ca-certificates tzdata

# Set working directory
WORKDIR /app

# Copy go mod files
COPY go.mod go.sum ./
RUN go mod download

# Copy source code
COPY . .

# Build the application
RUN CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build \
    -ldflags='-w -s -extldflags "-static"' \
    -a -installsuffix cgo \
    -o build-cache-server \
    ./cmd/cache-server

# Final stage
FROM gcr.io/distroless/static-debian11:nonroot

# Copy CA certificates and timezone data
COPY --from=builder /etc/ssl/certs/ca-certificates.crt /etc/ssl/certs/
COPY --from=builder /usr/share/zoneinfo /usr/share/zoneinfo

# Copy the binary
COPY --from=builder /app/build-cache-server /usr/local/bin/build-cache-server

# Use nonroot user
USER nonroot:nonroot

# Health check
HEALTHCHECK --interval=30s --timeout=10s --start-period=5s --retries=3 \
  CMD ["/usr/local/bin/build-cache-server", "--health-check"]

# Expose ports
EXPOSE 8080 9090

# Run the server
ENTRYPOINT ["/usr/local/bin/build-cache-server"]
