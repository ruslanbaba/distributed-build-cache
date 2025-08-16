FROM golang:1.22 as build

WORKDIR /app

# Copy go mod files
COPY go.mod go.sum ./
RUN go mod download

# Copy source code
COPY . .

# Build the binary
RUN CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -o /pruner ./cmd/pruner

# Final stage
FROM gcr.io/distroless/static:nonroot

COPY --from=build /pruner /pruner

# Use nonroot user
USER 65532:65532

EXPOSE 9090

ENTRYPOINT ["/pruner"]
