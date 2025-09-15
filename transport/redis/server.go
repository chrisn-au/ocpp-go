package redis

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"
	"time"

	"github.com/lorenzodonini/ocpp-go/transport"
	"github.com/go-redis/redis/v8"
)

// RedisServerTransport implements the transport.Transport interface for Redis-based communication.
type RedisServerTransport struct {
	*baseTransport
	config              *transport.RedisConfig
	sessionManager      *SessionManager
	correlationManager  *CorrelationManager
	messageHandler      transport.MessageHandler
	connectionHandler   transport.ConnectionHandler
	disconnectionHandler transport.DisconnectionHandler
	errorHandler        transport.ErrorHandler
	controlSubscription *redis.PubSub
	requestsConsumer    context.CancelFunc
	mu                  sync.RWMutex
}

// NewRedisServerTransport creates a new Redis server transport.
func NewRedisServerTransport() *RedisServerTransport {
	return &RedisServerTransport{}
}

// Start initializes and starts the Redis server transport.
func (rst *RedisServerTransport) Start(ctx context.Context, config transport.Config) error {
	redisConfig, ok := config.(*transport.RedisConfig)
	if !ok {
		return fmt.Errorf("invalid config type for Redis transport")
	}

	if err := redisConfig.Validate(); err != nil {
		return fmt.Errorf("invalid Redis config: %w", err)
	}

	rst.config = redisConfig

	// Create Redis client
	client := newRedisClient(redisConfig.Addr, redisConfig.Password, redisConfig.DB, redisConfig.ChannelPrefix)

	// Test connection
	if err := client.Ping(ctx).Err(); err != nil {
		return fmt.Errorf("failed to connect to Redis: %w", err)
	}

	rst.baseTransport = newBaseTransport(client)
	rst.sessionManager = NewSessionManager(client)
	rst.correlationManager = NewCorrelationManager(client, redisConfig.ClientTimeout)

	// Start base transport
	rst.baseTransport.start()

	// Load existing sessions from Redis
	if err := rst.sessionManager.LoadSessionsFromRedis(ctx); err != nil {
		rst.reportError(fmt.Errorf("failed to load existing sessions: %w", err))
	}

	// Start control channel subscription
	if err := rst.startControlSubscription(); err != nil {
		return fmt.Errorf("failed to start control subscription: %w", err)
	}

	// Start requests consumer
	if err := rst.startRequestsConsumer(); err != nil {
		return fmt.Errorf("failed to start requests consumer: %w", err)
	}

	return nil
}

// Stop gracefully shuts down the Redis server transport.
func (rst *RedisServerTransport) Stop(ctx context.Context) error {
	rst.mu.Lock()
	defer rst.mu.Unlock()

	if !rst.isRunning() {
		return nil
	}

	// Stop requests consumer
	if rst.requestsConsumer != nil {
		rst.requestsConsumer()
	}

	// Stop control subscription
	if rst.controlSubscription != nil {
		rst.controlSubscription.Close()
	}

	// Close correlation manager
	if rst.correlationManager != nil {
		rst.correlationManager.Close()
	}

	// Stop base transport
	return rst.baseTransport.stop()
}

// IsRunning returns true if the transport is currently running.
func (rst *RedisServerTransport) IsRunning() bool {
	return rst.baseTransport.isRunning()
}

// Write sends data to a specific client identified by clientID.
func (rst *RedisServerTransport) Write(clientID string, data []byte) error {
	if !rst.isRunning() {
		return transport.ErrNotStarted
	}

	// Check if client is connected
	if !rst.IsClientConnected(clientID) {
		return transport.ErrClientNotFound
	}

	// Create envelope for the OCPP response
	var ocppMessage []interface{}
	if err := json.Unmarshal(data, &ocppMessage); err != nil {
		return fmt.Errorf("failed to parse OCPP message: %w", err)
	}

	// Extract message ID and type from OCPP-J message
	var messageID string
	var messageType int
	if len(ocppMessage) >= 2 {
		if msgType, ok := ocppMessage[0].(float64); ok {
			messageType = int(msgType)
		}
		if msgID, ok := ocppMessage[1].(string); ok {
			messageID = msgID
		}
	}

	envelope := MessageEnvelope{
		ChargePointID: clientID,
		MessageType:   messageType,
		MessageID:     messageID,
		Payload:       ocppMessage,
		Timestamp:     time.Now(),
		WSServerID:    "redis-ocpp-server", // Identify this as coming from the Redis OCPP server
	}

	data, err := json.Marshal(envelope)
	if err != nil {
		return fmt.Errorf("failed to marshal response envelope: %w", err)
	}

	// Publish to client-specific response queue
	responseQueue := rst.client.keys.Responses + clientID
	if err := rst.client.pushToQueue(rst.ctx, responseQueue, data); err != nil {
		return fmt.Errorf("failed to send message to client %s: %w", clientID, err)
	}

	// Update session activity
	rst.sessionManager.UpdateSessionActivity(rst.ctx, clientID)

	return nil
}

// WriteToAll broadcasts data to all connected clients.
func (rst *RedisServerTransport) WriteToAll(data []byte) map[string]error {
	errors := make(map[string]error)
	clients := rst.GetConnectedClients()

	for _, clientID := range clients {
		if err := rst.Write(clientID, data); err != nil {
			errors[clientID] = err
		}
	}

	return errors
}

// GetConnectedClients returns a slice of currently connected client IDs.
func (rst *RedisServerTransport) GetConnectedClients() []string {
	sessions := rst.sessionManager.GetAllSessions()
	var clients []string

	for id, channel := range sessions {
		if channel.IsConnected() {
			clients = append(clients, id)
		}
	}

	return clients
}

// IsClientConnected checks if a specific client is currently connected.
func (rst *RedisServerTransport) IsClientConnected(clientID string) bool {
	channel, exists := rst.sessionManager.GetSession(clientID)
	return exists && channel.IsConnected()
}

// SetMessageHandler sets the handler for incoming messages.
func (rst *RedisServerTransport) SetMessageHandler(handler transport.MessageHandler) {
	rst.mu.Lock()
	defer rst.mu.Unlock()
	rst.messageHandler = handler
}

// SetConnectionHandler sets the handler for new client connections.
func (rst *RedisServerTransport) SetConnectionHandler(handler transport.ConnectionHandler) {
	rst.mu.Lock()
	defer rst.mu.Unlock()
	rst.connectionHandler = handler
}

// SetDisconnectionHandler sets the handler for client disconnections.
func (rst *RedisServerTransport) SetDisconnectionHandler(handler transport.DisconnectionHandler) {
	rst.mu.Lock()
	defer rst.mu.Unlock()
	rst.disconnectionHandler = handler
}

// SetErrorHandler sets the handler for transport-level errors.
func (rst *RedisServerTransport) SetErrorHandler(handler transport.ErrorHandler) {
	rst.mu.Lock()
	defer rst.mu.Unlock()
	rst.errorHandler = handler
}

// Errors returns a channel that receives transport-level errors.
func (rst *RedisServerTransport) Errors() <-chan error {
	return rst.baseTransport.errors()
}

// startControlSubscription starts listening for connection control events.
func (rst *RedisServerTransport) startControlSubscription() error {
	// Subscribe to connection control channels
	connectChannel := rst.client.keys.Control + "connect"
	disconnectChannel := rst.client.keys.Control + "disconnect"

	pubsub := rst.client.Subscribe(rst.ctx, connectChannel, disconnectChannel)
	rst.controlSubscription = pubsub

	rst.wg.Add(1)
	go func() {
		defer rst.wg.Done()
		defer pubsub.Close()

		ch := pubsub.Channel()
		for {
			select {
			case <-rst.ctx.Done():
				return
			case msg := <-ch:
				if msg == nil {
					continue
				}
				rst.handleControlEvent(msg)
			}
		}
	}()

	return nil
}

// startRequestsConsumer starts consuming messages from the requests queue.
func (rst *RedisServerTransport) startRequestsConsumer() error {
	ctx, cancel := context.WithCancel(rst.ctx)
	rst.requestsConsumer = cancel

	rst.wg.Add(1)
	go func() {
		defer rst.wg.Done()

		for {
			select {
			case <-ctx.Done():
				return
			default:
				rst.consumeRequest(ctx)
			}
		}
	}()

	return nil
}

// handleControlEvent processes connection control events.
func (rst *RedisServerTransport) handleControlEvent(msg *redis.Message) {
	var event ConnectionEvent
	if err := json.Unmarshal([]byte(msg.Payload), &event); err != nil {
		rst.reportError(fmt.Errorf("failed to unmarshal control event: %w", err))
		return
	}

	switch event.Event {
	case "connect":
		rst.handleConnect(event)
	case "disconnect":
		rst.handleDisconnect(event)
	}
}

// handleConnect processes a connection event.
func (rst *RedisServerTransport) handleConnect(event ConnectionEvent) {
	// Create or update session
	channel, err := rst.sessionManager.CreateSession(rst.ctx, event.ChargePointID, event.RemoteAddr, event.WSServerID)
	if err != nil {
		rst.reportError(fmt.Errorf("failed to create session for %s: %w", event.ChargePointID, err))
		return
	}

	// Store WS server ID in channel metadata
	channel.SetMetadata("ws_server_id", event.WSServerID)

	// Call connection handler
	rst.mu.RLock()
	handler := rst.connectionHandler
	rst.mu.RUnlock()

	if handler != nil {
		handler(event.ChargePointID)
	}
}

// handleDisconnect processes a disconnection event.
func (rst *RedisServerTransport) handleDisconnect(event ConnectionEvent) {
	// Remove session
	if err := rst.sessionManager.RemoveSession(rst.ctx, event.ChargePointID); err != nil {
		rst.reportError(fmt.Errorf("failed to remove session for %s: %w", event.ChargePointID, err))
	}

	// Call disconnection handler
	rst.mu.RLock()
	handler := rst.disconnectionHandler
	rst.mu.RUnlock()

	if handler != nil {
		handler(event.ChargePointID, nil)
	}
}

// consumeRequest consumes a request from the Redis queue.
func (rst *RedisServerTransport) consumeRequest(ctx context.Context) {
	// Block for up to 1 second waiting for a message
	message, err := rst.client.popFromQueue(ctx, rst.client.keys.Requests, 1*time.Second)
	if err != nil {
		if err != redis.Nil {
			rst.reportError(fmt.Errorf("failed to consume request: %w", err))
		}
		return
	}

	var envelope MessageEnvelope
	if err := json.Unmarshal([]byte(message), &envelope); err != nil {
		rst.reportError(fmt.Errorf("failed to unmarshal request message: %w", err))
		return
	}

	// Update session activity
	rst.sessionManager.UpdateSessionActivity(ctx, envelope.ChargePointID)

	// Call message handler
	rst.mu.RLock()
	handler := rst.messageHandler
	rst.mu.RUnlock()

	if handler != nil {
		// Extract the OCPP-J payload from the envelope
		payloadBytes, err := json.Marshal(envelope.Payload)
		if err != nil {
			rst.reportError(fmt.Errorf("failed to marshal OCPP payload for client %s: %w", envelope.ChargePointID, err))
			return
		}

		if err := handler(envelope.ChargePointID, payloadBytes); err != nil {
			rst.reportError(fmt.Errorf("message handler error for client %s: %w", envelope.ChargePointID, err))
		}
	}
}