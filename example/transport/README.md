# OCPP Transport Layer Examples

This directory contains examples demonstrating how to use the OCPP-Go library with different transport implementations through the transport abstraction layer.

## Directory Structure

```
transport/
├── redis/
│   ├── client/          # Redis transport client example
│   │   ├── main.go      # Client implementation
│   │   ├── Dockerfile   # Client Docker build
│   │   └── README.md    # Client documentation
│   ├── server/          # Redis transport server example
│   │   ├── main.go      # Server implementation
│   │   ├── Dockerfile   # Server Docker build
│   │   └── README.md    # Server documentation
│   └── README.md        # Redis transport overview
├── docker-compose.yml   # Complete stack setup
├── DOCKER.md           # Docker documentation
└── README.md           # This file
```

## Transport Types

### Redis Transport
Redis-based OCPP communication for distributed architectures where clients and servers may be on different machines or need to scale horizontally. Examples are organized in separate `client/` and `server/` directories for clear separation.

This directory focuses on Redis transport examples demonstrating the transport abstraction layer capabilities.

## Usage

### Docker Compose Stack (Recommended)

The easiest way to run and test the transport examples is using Docker Compose:

```bash
cd example/transport

# Start the complete stack (Redis + Server + Client)
docker-compose up -d

# View logs from all services
docker-compose logs -f

# View logs from specific services
docker-compose logs -f ocpp-server
docker-compose logs -f ocpp-client

# Monitor Redis activity via web UI
# Open http://localhost:47002 in your browser

# Stop the stack
docker-compose down
```

**Stack includes:**
- Redis server (port 47001)
- OCPP server using Redis transport
- OCPP client using Redis transport

See [DOCKER.md](DOCKER.md) for complete documentation.

### Manual Docker Commands

If you prefer to run services individually:

```bash
# Start Redis
docker run -d -p 47001:6379 --name ocpp-redis redis:7-alpine

# Build and run server
docker build -f redis/server/Dockerfile -t ocpp-redis-server ../../..
docker run --link ocpp-redis:redis -e REDIS_ADDR=redis:6379 ocpp-redis-server

# Build and run client
docker build -f redis/client/Dockerfile -t ocpp-redis-client ../../..
docker run --link ocpp-redis:redis -e REDIS_ADDR=redis:6379 ocpp-redis-client
```

## Key Differences from Legacy Examples

### New Transport-Based API

These examples use the new transport abstraction:

**Server:**
```go
// Create transport
factory := redis.NewRedisFactory()
config := &transport.RedisConfig{
    Addr:          "localhost:6379",
    ChannelPrefix: "ocpp",
}
redisTransport, _ := factory.CreateTransport(config)

// Create server with transport
server := ocppj.NewServerWithTransport(redisTransport, nil, nil, ocpp16.Profile)

// Use transport-compatible handlers
server.SetTransportRequestHandler(func(clientID string, request ocpp.Request, requestId string, action string) {
    // Handle request using clientID string for Redis transport
})

// Start with context and config
ctx := context.Background()
server.StartWithTransport(ctx, config)
```

**Client:**
```go
// Create transport
factory := redis.NewRedisFactory()
config := &transport.RedisConfig{
    Addr:          "localhost:6379",
    ChannelPrefix: "ocpp",
}
redisTransport, _ := factory.CreateClientTransport(config)

// Create client with transport
client := ocppj.NewClientWithTransport("client-id", redisTransport, nil, nil, ocpp16.Profile)

// Start with context and config (endpoint empty for Redis)
ctx := context.Background()
client.StartWithTransport(ctx, "", config)
```

### Legacy API Comparison

The transport abstraction provides benefits over direct usage:

```go
// Direct Redis usage (more complex)
redisClient := redis.NewClient(&redis.Options{Addr: "localhost:6379"})
// Manual message envelope handling, queue management, etc.

// Transport abstraction (simplified)
factory := redis.NewRedisFactory()
config := &transport.RedisConfig{Addr: "localhost:6379"}
transport, _ := factory.CreateTransport(config)
// Automatic message handling, connection management, etc.
```

## Benefits of Transport Abstraction

1. **Unified API**: Same OCPPJ interface works with any transport
2. **Pluggable Transports**: Easy to switch between Redis and future transports
3. **Better Testing**: Mock transports for unit testing
4. **Scalability**: Redis transport enables horizontal scaling
5. **Cloud-Native**: Better suited for containerized and distributed deployments

## Key Improvements

### Redis Transport Message Handling
The Redis transport now properly handles `MessageEnvelope` structures:

- **Server**: Wraps OCPP-J messages in `MessageEnvelope` for Redis queues
- **Client**: Automatically extracts OCPP-J payload from `MessageEnvelope`
- **Compatibility**: Works seamlessly with existing OCPP message parsing
- **Client ID Handling**: Redis transport uses client ID directly (not WebSocket URLs)

### Clean Directory Structure
Examples are now organized in separate directories to eliminate IDE conflicts:

- **No conflicts**: Each example has its own `main()` function
- **Clean builds**: Individual Docker builds for client and server
- **Better organization**: Focused documentation for each component

## Message Flow

The Redis transport examples demonstrate the standard OCPP message flow:

1. **Client Connection**: Client connects to server
2. **Boot Notification**: Client sends boot notification, server responds
3. **Status Notification**: Client sends connector status
4. **Heartbeat**: Periodic heartbeat messages
5. **Bidirectional Communication**: Server can send requests to client

## Environment Variables

### Docker Compose Configuration
The docker-compose stack uses these environment variables:

#### Redis Transport
- `REDIS_ADDR`: Redis server address (default: redis:6379)
- `REDIS_PASSWORD`: Redis password (optional)
- `CLIENT_ID`: Charge point identifier (default: CP-001)
- `OCPP_CHANNEL_PREFIX`: Redis key prefix (default: ocpp)
- `OCPP_CLIENT_TIMEOUT`: Client timeout (default: 30s)
- `OCPP_STATE_TTL`: State TTL (default: 300s)

#### Ports Used
- `47001`: Redis server

## Error Handling

All examples include proper error handling and graceful shutdown using context cancellation and signal handling.

## Production Considerations

For production deployments using Docker:

1. **Security**:
   - Enable Redis AUTH with strong passwords
   - Use Docker secrets for sensitive configuration
   - Run containers with non-root users
   - Secure Redis with TLS if needed

2. **Monitoring**:
   - Health checks are included in docker-compose
   - Add Prometheus/Grafana for metrics
   - Use structured logging with log aggregation

3. **Scalability**:
   - Scale Redis transport services with `docker-compose up --scale ocpp-server=3`
   - Use Redis Cluster for high availability
   - Multiple servers can process from the same Redis instance

4. **Configuration**:
   - Use `.env` files for environment-specific settings
   - Mount configuration files as volumes
   - Use Docker Swarm or Kubernetes for orchestration

5. **Testing**:
   - Integration tests with real Redis instances
   - Load testing with multiple clients
   - Chaos engineering with container failures