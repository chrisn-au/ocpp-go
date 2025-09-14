// Package redis provides Redis-based transport implementations for OCPP communication.
//
// This package implements the transport interfaces defined in the transport package,
// enabling OCPP communication through Redis queues and pub/sub channels. It's designed
// to work with distributed WebSocket servers and provides logical session management
// without requiring direct WebSocket connections.
//
// # Architecture
//
// The Redis transport consists of several key components:
//
//   - RedisServerTransport: Server-side transport that consumes from Redis queues
//   - RedisClientTransport: Client-side transport that publishes to Redis queues
//   - SessionManager: Manages logical sessions for charge points
//   - CorrelationManager: Handles request/response correlation using message IDs
//   - Channel: Represents logical communication channels
//
// # Integration with WebSocket Servers
//
// This transport is designed to integrate with WebSocket servers that:
//   - Publish incoming OCPP messages to "ocpp:requests" queue
//   - Consume responses from "ocpp:responses:{chargePointID}" queues
//   - Publish connection events to "ocpp:control:connect" and "ocpp:control:disconnect"
//
// # Message Format
//
// Messages use the MessageEnvelope format for compatibility with WebSocket servers:
//
//	type MessageEnvelope struct {
//	    ChargePointID string      `json:"charge_point_id"`
//	    MessageType   int         `json:"message_type"`
//	    MessageID     string      `json:"message_id"`
//	    Action        string      `json:"action,omitempty"`
//	    Payload       interface{} `json:"payload"`
//	    Timestamp     time.Time   `json:"timestamp"`
//	    WSServerID    string      `json:"ws_server_id"`
//	}
//
// # Redis Key Patterns
//
// The transport uses the following Redis key patterns (with default "ocpp" prefix):
//
//   - ocpp:requests - Queue for incoming OCPP requests
//   - ocpp:responses:{chargePointID} - Queue for responses to specific charge points
//   - ocpp:control:connect - Channel for connection events
//   - ocpp:control:disconnect - Channel for disconnection events
//   - ocpp:session:{chargePointID} - Hash for session metadata
//   - ocpp:active - Set of active charge point IDs
//   - ocpp:correlation:{messageID} - Correlation data with TTL
//
// # Usage Example
//
// Server-side usage:
//
//	config := &transport.RedisConfig{
//	    Addr: "localhost:6379",
//	    ChannelPrefix: "ocpp",
//	}
//
//	factory := redis.NewRedisFactory()
//	serverTransport, err := factory.CreateTransport(config)
//	if err != nil {
//	    log.Fatal(err)
//	}
//
//	serverTransport.SetMessageHandler(func(clientID string, data []byte) error {
//	    // Handle incoming OCPP message
//	    return nil
//	})
//
//	ctx := context.Background()
//	if err := serverTransport.Start(ctx, config); err != nil {
//	    log.Fatal(err)
//	}
//
// Client-side usage:
//
//	config := &transport.RedisConfig{
//	    Addr: "localhost:6379",
//	    ChannelPrefix: "ocpp",
//	}
//
//	factory := redis.NewRedisFactory()
//	clientTransport, err := factory.CreateClientTransport(config)
//	if err != nil {
//	    log.Fatal(err)
//	}
//
//	clientTransport.SetMessageHandler(func(data []byte) error {
//	    // Handle incoming OCPP response
//	    return nil
//	})
//
//	ctx := context.Background()
//	chargePointID := "CP001"
//	if err := clientTransport.Start(ctx, chargePointID, config); err != nil {
//	    log.Fatal(err)
//	}
//
// # Session Management
//
// The transport maintains logical sessions that persist across WebSocket reconnections.
// Sessions include metadata such as:
//   - Charge point ID
//   - Remote address from WebSocket connection
//   - WebSocket server ID
//   - Connection timestamps
//   - Last activity time
//
// # Message Correlation
//
// The transport supports request/response correlation using OCPP message IDs:
//   - Outgoing CALL messages are tracked with timeouts
//   - Incoming CALLRESULT/CALLERROR messages are matched to pending requests
//   - Correlation data is stored in Redis with TTL for automatic cleanup
//
// # Error Handling
//
// The transport provides comprehensive error handling:
//   - Transport-level errors are reported through error channels
//   - Failed message deliveries are logged and reported
//   - Connection issues trigger appropriate handler callbacks
//   - Correlation timeouts are handled gracefully
//
// # Concurrency Safety
//
// All transport components are designed to be thread-safe and can handle
// concurrent operations safely. Internal state is protected by appropriate
// synchronization mechanisms.
package redis