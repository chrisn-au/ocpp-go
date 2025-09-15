# OCPP Transport Layer Examples

This directory contains examples demonstrating how to use the OCPP-Go library with different transport implementations through the transport abstraction layer.

## Transport Types

### WebSocket Transport
Traditional WebSocket-based OCPP communication, now using the transport interface for better abstraction and consistency.

### Redis Transport
Redis-based OCPP communication for distributed architectures where clients and servers may be on different machines or need to scale horizontally.

## Usage

### WebSocket Examples

#### Server
```bash
cd example/transport/websocket
go run server.go

# With custom port
PORT=9000 go run server.go
```

#### Client
```bash
cd example/transport/websocket
go run client.go

# With custom server URL
SERVER_URL=ws://localhost:9000/ws go run client.go
```

### Redis Examples

#### Prerequisites
Start a Redis server:
```bash
# Using Docker
docker run -d -p 6379:6379 redis:latest

# Or install Redis locally and start it
redis-server
```

#### Server
```bash
cd example/transport/redis
go run server.go

# With custom Redis configuration
REDIS_ADDR=localhost:6380 REDIS_PASSWORD=mypassword go run server.go
```

#### Client
```bash
cd example/transport/redis
go run client.go

# With custom Redis configuration
REDIS_ADDR=localhost:6380 REDIS_PASSWORD=mypassword go run client.go
```

## Key Differences from Legacy Examples

### New Transport-Based API

These examples use the new transport abstraction:

**Server:**
```go
// Create transport
factory := websocket.NewWebSocketFactory()
config := &transport.WebSocketConfig{Port: 8887, ListenPath: "/ws/{id}"}
wsTransport, _ := factory.CreateTransport(config)

// Create server with transport
server := ocppj.NewServerWithTransport(wsTransport, nil, nil, ocpp16.Profile)

// Use transport-compatible handlers
server.SetTransportRequestHandler(func(clientID string, request ocpp.Request, requestId string, action string) {
    // Handle request using clientID string instead of ws.Channel
})

// Start with context and config
ctx := context.Background()
server.StartWithTransport(ctx, config)
```

**Client:**
```go
// Create transport
factory := websocket.NewWebSocketFactory()
config := &transport.WebSocketConfig{}
wsTransport, _ := factory.CreateClientTransport(config)

// Create client with transport
client := ocppj.NewClientWithTransport("client-id", wsTransport, nil, nil, ocpp16.Profile)

// Start with context and config
ctx := context.Background()
client.StartWithTransport(ctx, "ws://localhost:8887/ws", config)
```

### Legacy WebSocket API (Deprecated)

The old API still works for backward compatibility:

```go
// Legacy server creation (deprecated)
wsServer := ws.NewServer()
server := ocppj.NewServer(wsServer, nil, nil, ocpp16.Profile)

// Legacy client creation (deprecated)
wsClient := ws.NewClient()
client := ocppj.NewClient("client-id", wsClient, nil, nil, ocpp16.Profile)
```

## Benefits of Transport Abstraction

1. **Unified API**: Same OCPPJ interface works with any transport
2. **Pluggable Transports**: Easy to switch between WebSocket, Redis, or future transports
3. **Better Testing**: Mock transports for unit testing
4. **Scalability**: Redis transport enables horizontal scaling
5. **Cloud-Native**: Better suited for containerized and distributed deployments

## Message Flow

Both WebSocket and Redis examples demonstrate the same OCPP message flow:

1. **Client Connection**: Client connects to server
2. **Boot Notification**: Client sends boot notification, server responds
3. **Status Notification**: Client sends connector status
4. **Heartbeat**: Periodic heartbeat messages
5. **Bidirectional Communication**: Server can send requests to client

## Environment Variables

### WebSocket Examples
- `PORT`: Server listen port (default: 8887)
- `SERVER_URL`: Client server URL (default: ws://localhost:8887/ws)

### Redis Examples
- `REDIS_ADDR`: Redis server address (default: localhost:6379)
- `REDIS_PASSWORD`: Redis password (optional)

## Error Handling

All examples include proper error handling and graceful shutdown using context cancellation and signal handling.

## Production Considerations

For production use, consider:

1. **Security**: Use TLS for WebSocket, Redis AUTH for Redis
2. **Monitoring**: Add metrics and health checks
3. **Logging**: Use structured logging (e.g., logrus, zap)
4. **Configuration**: Use proper configuration management
5. **Testing**: Add integration tests with real transports