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

// RedisClientTransport implements the transport.ClientTransport interface for Redis-based communication.
type RedisClientTransport struct {
	*baseTransport
	config               *transport.RedisConfig
	clientID             string
	correlationManager   *CorrelationManager
	messageHandler       func(data []byte) error
	disconnectionHandler func(err error)
	reconnectionHandler  func()
	errorHandler         func(err error)
	responseSubscription *redis.PubSub
	responseConsumer     context.CancelFunc
	endpoint             string
	mu                   sync.RWMutex
}

// NewRedisClientTransport creates a new Redis client transport.
func NewRedisClientTransport() *RedisClientTransport {
	return &RedisClientTransport{}
}

// Start establishes a connection to the Redis server and begins message handling.
func (rct *RedisClientTransport) Start(ctx context.Context, endpoint string, config transport.Config) error {
	redisConfig, ok := config.(*transport.RedisConfig)
	if !ok {
		return fmt.Errorf("invalid config type for Redis transport")
	}

	if err := redisConfig.Validate(); err != nil {
		return fmt.Errorf("invalid Redis config: %w", err)
	}

	rct.config = redisConfig
	rct.endpoint = endpoint

	// Extract client ID from endpoint
	if endpoint == "" {
		return fmt.Errorf("endpoint (client ID) cannot be empty")
	}
	rct.clientID = endpoint

	// Create Redis client
	client := newRedisClient(redisConfig.Addr, redisConfig.Password, redisConfig.DB, redisConfig.ChannelPrefix)

	// Test connection
	if err := client.Ping(ctx).Err(); err != nil {
		return fmt.Errorf("failed to connect to Redis: %w", err)
	}

	rct.baseTransport = newBaseTransport(client)
	rct.correlationManager = NewCorrelationManager(client, redisConfig.ClientTimeout)

	// Start base transport
	rct.baseTransport.start()

	// Start response subscription
	if err := rct.startResponseSubscription(); err != nil {
		return fmt.Errorf("failed to start response subscription: %w", err)
	}

	// Simulate connection event
	if err := rct.publishConnectionEvent("connect"); err != nil {
		return fmt.Errorf("failed to publish connection event: %w", err)
	}

	return nil
}

// Stop closes the connection and shuts down the client transport.
func (rct *RedisClientTransport) Stop(ctx context.Context) error {
	rct.mu.Lock()
	defer rct.mu.Unlock()

	if !rct.isRunning() {
		return nil
	}

	// Simulate disconnection event
	rct.publishConnectionEvent("disconnect")

	// Stop response consumer
	if rct.responseConsumer != nil {
		rct.responseConsumer()
	}

	// Stop response subscription
	if rct.responseSubscription != nil {
		rct.responseSubscription.Close()
	}

	// Close correlation manager
	if rct.correlationManager != nil {
		rct.correlationManager.Close()
	}

	// Stop base transport
	return rct.baseTransport.stop()
}

// IsConnected returns true if the client is currently connected.
func (rct *RedisClientTransport) IsConnected() bool {
	return rct.baseTransport.isRunning()
}

// Write sends data to the connected server.
func (rct *RedisClientTransport) Write(data []byte) error {
	if !rct.isRunning() {
		return transport.ErrNotConnected
	}

	// Parse message to create envelope
	var envelope MessageEnvelope
	if err := json.Unmarshal(data, &envelope); err != nil {
		// If parsing fails, create a basic envelope
		envelope = MessageEnvelope{
			ChargePointID: rct.clientID,
			Timestamp:     time.Now(),
			Payload:       json.RawMessage(data),
		}
	} else {
		// Update envelope with client info
		envelope.ChargePointID = rct.clientID
		envelope.Timestamp = time.Now()
	}

	// Re-marshal with updated envelope
	envelopeData, err := json.Marshal(envelope)
	if err != nil {
		return fmt.Errorf("failed to marshal message envelope: %w", err)
	}

	// Push to requests queue
	queueName := rct.client.keys.Requests
	if err := rct.client.pushToQueue(rct.ctx, queueName, envelopeData); err != nil {
		return fmt.Errorf("failed to send message to queue %s: %w", queueName, err)
	}

	return nil
}

// SetMessageHandler sets the handler for incoming messages from the server.
func (rct *RedisClientTransport) SetMessageHandler(handler func(data []byte) error) {
	rct.mu.Lock()
	defer rct.mu.Unlock()
	rct.messageHandler = handler
}

// SetDisconnectionHandler sets the handler for disconnection events.
func (rct *RedisClientTransport) SetDisconnectionHandler(handler func(err error)) {
	rct.mu.Lock()
	defer rct.mu.Unlock()
	rct.disconnectionHandler = handler
}

// SetReconnectionHandler sets the handler for successful reconnection events.
func (rct *RedisClientTransport) SetReconnectionHandler(handler func()) {
	rct.mu.Lock()
	defer rct.mu.Unlock()
	rct.reconnectionHandler = handler
}

// SetErrorHandler sets the handler for transport-level errors.
func (rct *RedisClientTransport) SetErrorHandler(handler func(err error)) {
	rct.mu.Lock()
	defer rct.mu.Unlock()
	rct.errorHandler = handler
}

// Errors returns a channel that receives transport-level errors.
func (rct *RedisClientTransport) Errors() <-chan error {
	return rct.baseTransport.errors()
}

// SendRequest sends a request and waits for a response with correlation.
func (rct *RedisClientTransport) SendRequest(ctx context.Context, data []byte, timeout time.Duration) ([]byte, error) {
	if !rct.isRunning() {
		return nil, transport.ErrNotConnected
	}

	// Parse message to extract message ID
	var envelope MessageEnvelope
	if err := json.Unmarshal(data, &envelope); err != nil {
		return nil, fmt.Errorf("failed to parse message for correlation: %w", err)
	}

	if envelope.MessageID == "" {
		return nil, fmt.Errorf("message ID is required for correlated requests")
	}

	// Track request for correlation
	responseCh, errorCh, err := rct.correlationManager.TrackRequest(ctx, envelope.MessageID, rct.clientID, timeout)
	if err != nil {
		return nil, fmt.Errorf("failed to track request: %w", err)
	}

	// Send the message
	if err := rct.Write(data); err != nil {
		rct.correlationManager.FailRequest(envelope.MessageID, err)
		return nil, err
	}

	// Wait for response or error
	select {
	case response := <-responseCh:
		return response, nil
	case err := <-errorCh:
		return nil, err
	case <-ctx.Done():
		rct.correlationManager.FailRequest(envelope.MessageID, ctx.Err())
		return nil, ctx.Err()
	}
}

// startResponseSubscription starts listening for responses from the server.
func (rct *RedisClientTransport) startResponseSubscription() error {
	// Subscribe to client-specific response queue
	responseQueue := rct.client.keys.Responses + rct.clientID

	ctx, cancel := context.WithCancel(rct.ctx)
	rct.responseConsumer = cancel

	rct.wg.Add(1)
	go func() {
		defer rct.wg.Done()

		for {
			select {
			case <-ctx.Done():
				return
			default:
				rct.consumeResponse(ctx, responseQueue)
			}
		}
	}()

	return nil
}

// publishConnectionEvent publishes a connection event to the control channel.
func (rct *RedisClientTransport) publishConnectionEvent(event string) error {
	controlChannel := rct.client.keys.Control + event

	connectionEvent := ConnectionEvent{
		Event:         event,
		ChargePointID: rct.clientID,
		WSServerID:    "redis-client",
		Timestamp:     time.Now(),
		RemoteAddr:    "redis://client",
	}

	return rct.client.publishMessage(rct.ctx, controlChannel, connectionEvent)
}

// consumeResponse consumes a response from the Redis queue.
func (rct *RedisClientTransport) consumeResponse(ctx context.Context, responseQueue string) {
	// Block for up to 1 second waiting for a message
	message, err := rct.client.popFromQueue(ctx, responseQueue, 1*time.Second)
	if err != nil {
		if err != redis.Nil {
			rct.reportError(fmt.Errorf("failed to consume response: %w", err))
		}
		return
	}

	// Try to handle as correlated response first
	var envelope MessageEnvelope
	if err := json.Unmarshal([]byte(message), &envelope); err == nil && envelope.MessageID != "" {
		// Check if this is a correlated response
		if rct.correlationManager.CompleteRequest(envelope.MessageID, []byte(message)) == nil {
			return // Successfully handled as correlated response
		}
	}

	// Handle as regular message
	rct.mu.RLock()
	handler := rct.messageHandler
	rct.mu.RUnlock()

	if handler != nil {
		if err := handler([]byte(message)); err != nil {
			rct.reportError(fmt.Errorf("message handler error: %w", err))
		}
	}
}