# Build stage
FROM golang:1.21-alpine AS builder

WORKDIR /app

# Copy go mod files
COPY go.mod go.sum ./
RUN go mod download

# Copy source code
COPY . .

# Build the client
RUN go build -o redis-client ./example/transport/redis/client

# Runtime stage
FROM alpine:latest

RUN apk --no-cache add ca-certificates
WORKDIR /root/

# Copy the binary from builder stage
COPY --from=builder /app/redis-client .

# Expose any ports if needed
EXPOSE 9090

# Run the client
CMD ["./redis-client"]