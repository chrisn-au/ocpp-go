# Redis Transport Examples

This directory contains examples demonstrating OCPP-Go with Redis transport for distributed messaging.

## Directory Structure

```
redis/
├── client/          # OCPP charge point client example
│   ├── main.go      # Client implementation
│   ├── Dockerfile   # Docker build for client
│   └── README.md    # Client-specific documentation
├── server/          # OCPP central system server example
│   ├── main.go      # Server implementation
│   ├── Dockerfile   # Docker build for server
│   └── README.md    # Server-specific documentation
└── README.md        # This file
```

## Overview

The Redis transport examples showcase distributed OCPP architecture where:

- **Client**: Acts as a charge point connecting via Redis queues
- **Server**: Acts as a central system processing messages via Redis
- **Redis**: Provides reliable message queuing and pub/sub capabilities

## Quick Start

1. **Start Redis**:
   ```bash
   docker run -d -p 6379:6379 redis:alpine
   ```

2. **Build Examples**:
   ```bash
   # From project root
   go build -o ./example/transport/redis/server/redis-server ./example/transport/redis/server
   go build -o ./example/transport/redis/client/redis-client ./example/transport/redis/client
   ```

3. **Run Server**:
   ```bash
   cd ./example/transport/redis/server
   ./redis-server
   ```

4. **Run Client** (in another terminal):
   ```bash
   cd ./example/transport/redis/client
   ./redis-client
   ```

## Key Features

- **Distributed Architecture**: Server and client can run on different machines
- **Reliable Messaging**: Redis provides persistent queues and message durability
- **Scalable**: Multiple servers can process messages from the same Redis instance
- **Connection Independence**: No direct WebSocket connections required

## Environment Variables

Both examples support these environment variables:

- `REDIS_ADDR`: Redis server address (default: `localhost:6379`)
- `REDIS_PASSWORD`: Redis password (optional)
- `CLIENT_ID`: Client identifier for the charge point (client only)

## Docker Compose

See the parent `docker-compose.yml` for a complete setup with Redis, server, and client.