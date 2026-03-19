# Stage 1: Builder
FROM golang:1.24-alpine AS builder

WORKDIR /build

# Copy go mod files
COPY go.mod go.sum ./

# Copy source code
COPY . .

# Build the binary with CGO disabled for a fully static build
# Strip debug info with -s -w to reduce size
RUN CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -ldflags="-s -w" -o l402proxy ./cmd/l402proxy

# Stage 2: Runtime
FROM scratch

# Copy the binary from builder
COPY --from=builder /build/l402proxy /l402proxy

# Copy CA certificates for TLS connections
COPY --from=builder /etc/ssl/certs/ca-certificates.crt /etc/ssl/certs/

EXPOSE 8080

ENTRYPOINT ["/l402proxy"]
