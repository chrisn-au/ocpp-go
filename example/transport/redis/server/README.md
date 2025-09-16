# Redis Server Example

This example demonstrates how to use the OCPP-Go library with Redis transport as an OCPP central system server.

## Building

```bash
# From the project root
go build -o ./example/transport/redis/server/redis-server ./example/transport/redis/server

# Or from this directory
go build -o redis-server .
```

## Running

```bash
# Set environment variables (optional)
export REDIS_ADDR=localhost:6379
export REDIS_PASSWORD=your-password

# Run the server
./redis-server
```

## Docker

```bash
# Build
docker build -t ocpp-redis-server .

# Run
docker run --rm \
  -e REDIS_ADDR=redis:6379 \
  ocpp-redis-server
```

## Features

- Listens for OCPP messages via Redis transport
- Handles boot notifications, heartbeats, and status notifications
- Responds to charge point requests
- Demonstrates distributed OCPP server architecture
- Shows server-to-client communication patterns