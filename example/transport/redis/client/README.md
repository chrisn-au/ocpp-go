# Redis Client Example

This example demonstrates how to use the OCPP-Go library with Redis transport as a charge point client.

## Building

```bash
# From the project root
go build -o ./example/transport/redis/client/redis-client ./example/transport/redis/client

# Or from this directory
go build -o redis-client .
```

## Running

```bash
# Set environment variables (optional)
export REDIS_ADDR=localhost:6379
export REDIS_PASSWORD=your-password
export CLIENT_ID=chargepoint001

# Run the client
./redis-client
```

## Docker

```bash
# Build
docker build -t ocpp-redis-client .

# Run
docker run --rm \
  -e REDIS_ADDR=redis:6379 \
  -e CLIENT_ID=chargepoint001 \
  ocpp-redis-client
```

## Features

- Connects to OCPP server via Redis transport
- Sends boot notification, heartbeats, and status notifications
- Handles incoming server requests (GetConfiguration)
- Demonstrates Redis-based distributed messaging