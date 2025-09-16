# OCPP-Go Library Fork - Distributed Architecture Modifications

**Type**: library
**Language**: Go
**Purpose**: Enhanced OCPP-Go library with Redis transport and distributed state management capabilities
**Dependencies**: github.com/go-redis/redis/v8, original ocpp-go dependencies
**Status**: active

## Overview

This fork of the upstream ocpp-go library (https://github.com/lorenzodonini/ocpp-go) introduces a comprehensive transport abstraction layer and Redis-based distributed communication system. The modifications enable horizontal scaling of OCPP servers by separating WebSocket connection handling from OCPP message processing, allowing multiple server instances to share message processing workload through Redis pub/sub and queuing mechanisms.

The key enhancement transforms the originally monolithic WebSocket-centric architecture into a pluggable transport system that supports both traditional WebSocket connections and distributed Redis-based communication patterns. This enables deployment architectures where WebSocket proxy servers handle connections while dedicated OCPP processing servers handle the protocol logic.

The modifications maintain full backward compatibility with existing WebSocket-based implementations while providing new distributed capabilities for scalable OCPP infrastructure deployments.

## Architecture

```
Original Architecture:
┌─────────────────┐    ┌─────────────────┐
│   Charge Point  │◄──►│   OCPP Server   │
│   (WebSocket)   │    │  (Monolithic)   │
└─────────────────┘    └─────────────────┘

Enhanced Architecture:
┌─────────────────┐    ┌─────────────────┐    ┌─────────────────┐
│   Charge Point  │◄──►│   WS-Proxy      │◄──►│  OCPP-Server-1  │
│   (WebSocket)   │    │   (Connection)  │    │  (Processing)   │
└─────────────────┘    └─────────────────┘    └─────────────────┘
                                │                       │
                       ┌─────────────────┐             │
                       │  OCPP-Server-2  │◄────────────┘
                       │  (Processing)   │
                       └─────────────────┘
                                │                       │
                                ▼                       ▼
                       ┌─────────────────────────────────────────┐
                       │              Redis                      │
                       │  requests  responses:CP-ID  sessions   │
                       │  control   correlation      state      │
                       └─────────────────────────────────────────┘
```

### Core Components Added

1. **Transport Abstraction Layer**: `/Users/chrishome/development/home/mcp-access/csms/ocpp-go/transport/transport.go`
   - `Transport` interface for server-side implementations
   - `ClientTransport` interface for client-side implementations
   - `Config` interface for transport-specific configuration
   - Support for WebSocket and Redis transport types

2. **Redis Transport Implementation**: `/Users/chrishome/development/home/mcp-access/csms/ocpp-go/transport/redis/`
   - Complete Redis-based transport layer
   - Session management with Redis persistence
   - Request/response correlation with TTL
   - Pub/sub control channels for connection events

3. **Distributed State Management**: `/Users/chrishome/development/home/mcp-access/csms/ocpp-go/ocppj/redis_state.go`
   - Redis-backed ServerState and ClientState implementations
   - Distributed request correlation tracking
   - Persistent session state across server restarts

## Message Flow

### Redis Transport Communication Pattern

1. **Connection Events**:
   ```
   WS-Proxy → "ocpp:control:connect" → Redis → OCPP-Server
   WS-Proxy → "ocpp:control:disconnect" → Redis → OCPP-Server
   ```

2. **Request Flow**:
   ```
   Charge Point → WS-Proxy → "ocpp:requests" queue → Redis → OCPP-Server
   ```

3. **Response Flow**:
   ```
   OCPP-Server → "ocpp:responses:{chargePointID}" queue → Redis → WS-Proxy → Charge Point
   ```

4. **State Synchronization**:
   ```
   OCPP-Server → "ocpp:session:{chargePointID}" hash → Redis
   OCPP-Server → "ocpp:pending:{clientID}" hash → Redis
   OCPP-Server → "ocpp:correlation:{messageID}" key → Redis
   ```

### Message Format

All messages use the standardized MessageEnvelope format:

```json
{
  "charge_point_id": "CP-001",
  "message_type": 2,
  "message_id": "uuid-string",
  "action": "BootNotification",
  "payload": [2, "uuid", "BootNotification", {...}],
  "timestamp": "2023-09-15T12:00:00Z",
  "ws_server_id": "ws-proxy-01"
}
```

## Configuration

### Redis Transport Configuration

```go
type RedisConfig struct {
    Addr                string        // "localhost:6379"
    Password           string        // Optional Redis password
    DB                 int           // Redis database number (0)
    ChannelPrefix      string        // Key prefix ("ocpp")
    ClientTimeout      time.Duration // Operation timeout (30s)
    ConnectionPoolSize int           // Connection pool size (10)
    RetryAttempts      int           // Retry count (3)
    RetryDelay         time.Duration // Retry delay (1s)
    UseDistributedState bool         // Enable Redis state management
    StateKeyPrefix     string        // State key prefix ("ocpp")
    StateTTL           time.Duration // State TTL (30s)
}
```

### Redis Key Patterns

```
ocpp:requests                    - Queue for incoming OCPP requests
ocpp:responses:{chargePointID}   - Queue for responses to specific charge points
ocpp:control:connect            - Channel for connection events
ocpp:control:disconnect         - Channel for disconnection events
ocpp:session:{chargePointID}    - Hash for session metadata
ocpp:active                     - Set of active charge point IDs
ocpp:correlation:{messageID}    - Correlation data with TTL
ocpp:pending:{clientID}         - Hash for pending request tracking
```

## API Reference

### New Transport Interfaces

**Transport Interface** (`/Users/chrishome/development/home/mcp-access/csms/ocpp-go/transport/transport.go:59-105`):
```go
type Transport interface {
    Start(ctx context.Context, config Config) error
    Stop(ctx context.Context) error
    IsRunning() bool
    Write(clientID string, data []byte) error
    WriteToAll(data []byte) map[string]error
    GetConnectedClients() []string
    IsClientConnected(clientID string) bool
    SetMessageHandler(handler MessageHandler)
    SetConnectionHandler(handler ConnectionHandler)
    SetDisconnectionHandler(handler DisconnectionHandler)
    SetErrorHandler(handler ErrorHandler)
    Errors() <-chan error
}
```

**ClientTransport Interface** (`/Users/chrishome/development/home/mcp-access/csms/ocpp-go/transport/transport.go:110-143`):
```go
type ClientTransport interface {
    Start(ctx context.Context, endpoint string, config Config) error
    Stop(ctx context.Context) error
    IsConnected() bool
    Write(data []byte) error
    SetMessageHandler(handler func(data []byte) error)
    SetDisconnectionHandler(handler func(err error))
    SetReconnectionHandler(handler func())
    SetErrorHandler(handler func(err error))
    Errors() <-chan error
}
```

### Enhanced Server Creation

**NewServerWithTransport** (`/Users/chrishome/development/home/mcp-access/csms/ocpp-go/ocppj/server.go:49-86`):
```go
func NewServerWithTransport(
    transport transport.Transport,
    dispatcher ServerDispatcher,
    stateHandler ServerState,
    profiles ...*ocpp.Profile
) *Server
```

### Redis State Management

**RedisServerState** (`/Users/chrishome/development/home/mcp-access/csms/ocpp-go/ocppj/redis_state.go:14-33`):
```go
type RedisServerState struct {
    client     *redis.Client
    keyPrefix  string
    defaultTTL time.Duration
}

func NewRedisServerState(client *redis.Client, keyPrefix string, ttl time.Duration) ServerState
```

## Development

### Key Modified Files

1. **Core Transport Layer**:
   - `/Users/chrishome/development/home/mcp-access/csms/ocpp-go/transport/transport.go` - Transport interface definitions
   - `/Users/chrishome/development/home/mcp-access/csms/ocpp-go/transport/factory.go` - Transport factory pattern
   - `/Users/chrishome/development/home/mcp-access/csms/ocpp-go/transport/errors.go` - Transport-specific errors

2. **Redis Transport Implementation** (New Package):
   - `/Users/chrishome/development/home/mcp-access/csms/ocpp-go/transport/redis/transport.go` - Base Redis client wrapper and utilities
   - `/Users/chrishome/development/home/mcp-access/csms/ocpp-go/transport/redis/server.go` - Redis server transport implementation
   - `/Users/chrishome/development/home/mcp-access/csms/ocpp-go/transport/redis/client.go` - Redis client transport implementation
   - `/Users/chrishome/development/home/mcp-access/csms/ocpp-go/transport/redis/session.go` - Session management for Redis
   - `/Users/chrishome/development/home/mcp-access/csms/ocpp-go/transport/redis/correlation.go` - Request/response correlation
   - `/Users/chrishome/development/home/mcp-access/csms/ocpp-go/transport/redis/channel.go` - Logical channel abstraction
   - `/Users/chrishome/development/home/mcp-access/csms/ocpp-go/transport/redis/factory.go` - Redis transport factory
   - `/Users/chrishome/development/home/mcp-access/csms/ocpp-go/transport/redis/doc.go` - Comprehensive package documentation

3. **WebSocket Transport Adaptation** (New Package):
   - `/Users/chrishome/development/home/mcp-access/csms/ocpp-go/transport/websocket/adapter.go` - WebSocket to transport interface adapter
   - `/Users/chrishome/development/home/mcp-access/csms/ocpp-go/transport/websocket/server.go` - WebSocket server transport
   - `/Users/chrishome/development/home/mcp-access/csms/ocpp-go/transport/websocket/client.go` - WebSocket client transport
   - `/Users/chrishome/development/home/mcp-access/csms/ocpp-go/transport/websocket/channel.go` - WebSocket channel wrapper

4. **Enhanced OCPPJ Layer**:
   - `/Users/chrishome/development/home/mcp-access/csms/ocpp-go/ocppj/server.go:49-86` - NewServerWithTransport constructor
   - `/Users/chrishome/development/home/mcp-access/csms/ocpp-go/ocppj/client.go` - Enhanced client with transport support
   - `/Users/chrishome/development/home/mcp-access/csms/ocpp-go/ocppj/redis_state.go` - Redis-backed state management
   - `/Users/chrishome/development/home/mcp-access/csms/ocpp-go/ocppj/business_state.go` - Transport-compatible state interfaces

5. **Dependencies Added**:
   - `/Users/chrishome/development/home/mcp-access/csms/ocpp-go/go.mod:10` - `github.com/go-redis/redis/v8 v8.11.5`
   - `/Users/chrishome/development/home/mcp-access/csms/ocpp-go/go.mod:7` - `github.com/caarlos0/env/v11 v11.3.1`

6. **Example Implementations**:
   - `/Users/chrishome/development/home/mcp-access/csms/ocpp-go/example/transport/redis/server_example.go` - Redis server usage example
   - `/Users/chrishome/development/home/mcp-access/csms/ocpp-go/example/transport/redis/client_example.go` - Redis client usage example

### Build and Testing

Redis transport includes comprehensive test suite:
- `/Users/chrishome/development/home/mcp-access/csms/ocpp-go/transport/redis/redis_test.go` - 464 lines of integration tests
- `/Users/chrishome/development/home/mcp-access/csms/ocpp-go/transport/websocket/adapter_test.go` - WebSocket adapter tests

## Deployment

### Docker Integration

Redis transport examples include Docker configurations:
- `/Users/chrishome/development/home/mcp-access/csms/ocpp-go/example/transport/redis/Dockerfile.server`
- `/Users/chrishome/development/home/mcp-access/csms/ocpp-go/example/transport/redis/Dockerfile.client`

### Environment Configuration

```bash
# Redis connection
REDIS_ADDR=localhost:6379
REDIS_PASSWORD=optional-password
REDIS_DB=0

# Transport configuration
OCPP_CHANNEL_PREFIX=ocpp
OCPP_CLIENT_TIMEOUT=30s
OCPP_STATE_TTL=300s
```

## Modifications Summary

### Files Added (New Implementation)
- Entire `/Users/chrishome/development/home/mcp-access/csms/ocpp-go/transport/` package (18 files)
- `/Users/chrishome/development/home/mcp-access/csms/ocpp-go/ocppj/redis_state.go` (250 lines)

### Files Modified (Enhanced Existing)
- `/Users/chrishome/development/home/mcp-access/csms/ocpp-go/ocppj/server.go:49-86` - Added NewServerWithTransport constructor
- `/Users/chrishome/development/home/mcp-access/csms/ocpp-go/ocppj/client.go` - Enhanced with transport support
- `/Users/chrishome/development/home/mcp-access/csms/ocpp-go/ocppj/business_state.go` - Extended state interfaces
- `/Users/chrishome/development/home/mcp-access/csms/ocpp-go/go.mod:10,7` - Added Redis and env dependencies
- `/Users/chrishome/development/home/mcp-access/csms/ocpp-go/README.md:35-50` - Documented Redis transport

### Backward Compatibility
- All original WebSocket-based APIs remain functional
- Existing `NewServer()` constructor unchanged
- Original `ws.Server` interface preserved
- No breaking changes to public APIs

### Performance and Scalability Impact
- Enables horizontal scaling of OCPP processing servers
- Redis-based session persistence survives server restarts
- Message queuing provides reliability and load balancing
- Request correlation supports timeout and retry mechanisms
- Distributed state management eliminates single points of failure

## Purpose and Rationale

### Problem Solved
The original ocpp-go library tightly coupled WebSocket connection handling with OCPP message processing, creating scalability limitations:
- Single server handled both connections and processing
- No ability to distribute processing load
- Session state lost on server restart
- Limited horizontal scaling options

### Solution Approach
The transport abstraction pattern separates concerns:
1. **Connection Layer**: WebSocket proxies handle client connections
2. **Transport Layer**: Redis provides reliable message queuing and pub/sub
3. **Processing Layer**: OCPP servers focus on protocol logic
4. **State Layer**: Redis provides distributed session and request state

### Benefits Achieved
- **Horizontal Scaling**: Multiple OCPP processing servers share workload
- **High Availability**: Server failures don't lose connection state
- **Load Distribution**: Redis queues balance processing across servers
- **Operational Flexibility**: Separate deployment and scaling of connection vs processing tiers
- **Backward Compatibility**: Existing implementations continue working unchanged
- **Future Extensibility**: Transport pattern supports additional transport types (RabbitMQ, Kafka, etc.)

This architectural enhancement transforms ocpp-go from a single-server library into a distributed, scalable OCPP infrastructure foundation suitable for large-scale charge point management systems.