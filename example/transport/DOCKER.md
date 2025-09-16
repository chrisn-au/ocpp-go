# Docker Compose Transport Examples

This directory contains a complete Docker Compose stack for testing OCPP transport layer implementations.

## Stack Overview

The compose file brings up the following services on ports 47000-47999:

| Service | Port | Description |
|---------|------|-------------|
| `redis` | 47001 | Redis server for transport messaging |
| `redis-commander` | 47002 | Redis admin UI for monitoring |
| `websocket-server` | 47003 | WebSocket OCPP server for comparison |
| `ocpp-server` | - | Redis transport OCPP server |
| `ocpp-client` | - | Redis transport OCPP client |

## Quick Start

```bash
# From the transport examples directory
cd example/transport

# Start the full stack
docker-compose up -d

# Watch logs from all services
docker-compose logs -f

# Watch logs from specific service
docker-compose logs -f ocpp-server
docker-compose logs -f ocpp-client

# Stop the stack
docker-compose down

# Stop and remove volumes
docker-compose down -v
```

## Services

### Redis Transport Stack

1. **Redis Server** (`redis:47001`)
   - Message broker for OCPP transport
   - Persistent storage with volume
   - Health checks enabled

2. **OCPP Server** (`ocpp-server`)
   - Uses Redis transport for distributed processing
   - Connects to Redis on startup
   - Handles OCPP protocol messages

3. **OCPP Client** (`ocpp-client`)
   - Simulates charge point using Redis transport
   - Sends BootNotification, Heartbeat, StatusNotification
   - Client ID: `CP-001`

### Monitoring & Debugging

4. **Redis Commander** (`redis-commander:47002`)
   - Web UI for Redis monitoring
   - View keys, queues, pub/sub messages
   - Access at: http://localhost:47002

### Comparison Stack

5. **WebSocket Server** (`websocket-server:47003`)
   - Traditional WebSocket OCPP server
   - For comparing with Redis transport
   - Access at: ws://localhost:47003/ws/{id}

## Testing the Stack

### 1. Verify Services are Running
```bash
docker-compose ps
```

### 2. Check Redis Activity
Open Redis Commander at http://localhost:47002 and monitor:
- `ocpp:requests` queue
- `ocpp:responses:CP-001` queue
- `ocpp:session:CP-001` hash
- `ocpp:active` set

### 3. Monitor Message Flow
```bash
# Server logs
docker-compose logs -f ocpp-server

# Client logs
docker-compose logs -f ocpp-client

# Redis logs
docker-compose logs -f redis
```

### 4. Test WebSocket Comparison
```bash
# Test WebSocket server with wscat
npm install -g wscat
wscat -c ws://localhost:47003/ws/test-client

# Send OCPP message
["2","12345","BootNotification",{"chargePointVendor":"Test","chargePointModel":"Test"}]
```

## Environment Variables

### Redis Configuration
- `REDIS_ADDR`: Redis server address (default: redis:6379)
- `REDIS_PASSWORD`: Redis password (optional)
- `OCPP_CHANNEL_PREFIX`: Key prefix for OCPP messages (default: ocpp)
- `OCPP_CLIENT_TIMEOUT`: Client operation timeout (default: 30s)
- `OCPP_STATE_TTL`: State TTL for distributed management (default: 300s)

### Client Configuration
- `CLIENT_ID`: Charge point identifier (default: CP-001)

### WebSocket Configuration
- `PORT`: WebSocket server port (default: 8887)

## Troubleshooting

### Services Won't Start
```bash
# Check service health
docker-compose ps

# View service logs
docker-compose logs <service-name>

# Restart specific service
docker-compose restart <service-name>
```

### Redis Connection Issues
```bash
# Test Redis connectivity
docker-compose exec redis redis-cli ping

# Check Redis logs
docker-compose logs redis
```

### Build Issues
```bash
# Rebuild containers
docker-compose build --no-cache

# Force recreate
docker-compose up --force-recreate
```

## Development

### Modifying Examples
1. Edit the example code in `redis/` or `websocket/` directories
2. Rebuild containers: `docker-compose build`
3. Restart services: `docker-compose up -d`

### Adding Custom Configuration
1. Create `.env` file with custom variables
2. Mount config files as volumes in `docker-compose.yml`
3. Use environment-specific compose files: `docker-compose -f docker-compose.yml -f docker-compose.dev.yml up`

## Message Flow Verification

The stack demonstrates the complete OCPP message flow:

1. **Connection**: Client connects via Redis transport
2. **BootNotification**: Client → Server → Response
3. **StatusNotification**: Client reports connector status
4. **Heartbeat**: Periodic keep-alive messages
5. **Server Requests**: Server can send requests to clients

All messages flow through Redis queues and can be monitored in real-time through Redis Commander.