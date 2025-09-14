// Package transport provides abstractions for different transport layers in OCPP implementations.
// It defines interfaces that can be implemented by various transport mechanisms such as WebSocket,
// Redis, or other messaging systems to enable flexible OCPP communication.
package transport

import (
	"context"
	"crypto/tls"
	"time"
)

// TransportType represents the type of transport implementation.
type TransportType int

const (
	// WebSocketTransport indicates a WebSocket-based transport.
	WebSocketTransport TransportType = iota
	// RedisTransport indicates a Redis-based transport.
	RedisTransport
)

// String returns the string representation of the transport type.
func (t TransportType) String() string {
	switch t {
	case WebSocketTransport:
		return "websocket"
	case RedisTransport:
		return "redis"
	default:
		return "unknown"
	}
}

// Config represents the configuration interface for transport implementations.
// Each transport type should implement this interface with their specific configuration.
type Config interface {
	// GetType returns the transport type this configuration is for.
	GetType() TransportType
	// Validate checks if the configuration is valid and returns an error if not.
	Validate() error
}

// MessageHandler is a function that processes incoming messages.
// It receives the client ID and the message data, and returns an error if processing fails.
type MessageHandler func(clientID string, data []byte) error

// ConnectionHandler is a function that handles connection events.
// It receives the client ID of the connecting client.
type ConnectionHandler func(clientID string)

// DisconnectionHandler is a function that handles disconnection events.
// It receives the client ID and the error that caused the disconnection (if any).
type DisconnectionHandler func(clientID string, err error)

// ErrorHandler is a function that handles transport-level errors.
// It receives the client ID (if available) and the error that occurred.
type ErrorHandler func(clientID string, err error)

// Transport defines the interface for server-side transport implementations.
// This interface abstracts the underlying communication mechanism and provides
// a unified API for OCPP message exchange in a server context where multiple
// clients can connect.
type Transport interface {
	// Start initializes and starts the transport with the given configuration.
	// It should begin listening for connections and be ready to handle messages.
	Start(ctx context.Context, config Config) error

	// Stop gracefully shuts down the transport, closing all connections.
	// It should ensure all pending operations are completed or cancelled.
	Stop(ctx context.Context) error

	// IsRunning returns true if the transport is currently running and accepting connections.
	IsRunning() bool

	// Write sends data to a specific client identified by clientID.
	// Returns an error if the client is not connected or if sending fails.
	Write(clientID string, data []byte) error

	// WriteToAll broadcasts data to all connected clients.
	// Returns a map of clientID to error for any failed sends.
	WriteToAll(data []byte) map[string]error

	// GetConnectedClients returns a slice of currently connected client IDs.
	GetConnectedClients() []string

	// IsClientConnected checks if a specific client is currently connected.
	IsClientConnected(clientID string) bool

	// SetMessageHandler sets the handler for incoming messages.
	// The handler will be called for each message received from any client.
	SetMessageHandler(handler MessageHandler)

	// SetConnectionHandler sets the handler for new client connections.
	SetConnectionHandler(handler ConnectionHandler)

	// SetDisconnectionHandler sets the handler for client disconnections.
	SetDisconnectionHandler(handler DisconnectionHandler)

	// SetErrorHandler sets the handler for transport-level errors.
	SetErrorHandler(handler ErrorHandler)

	// Errors returns a channel that receives transport-level errors.
	// This provides an alternative to SetErrorHandler for error handling.
	Errors() <-chan error
}

// ClientTransport defines the interface for client-side transport implementations.
// This interface abstracts the underlying communication mechanism for a single
// client connecting to a server.
type ClientTransport interface {
	// Start establishes a connection to the server at the given endpoint.
	// The endpoint format depends on the transport implementation.
	Start(ctx context.Context, endpoint string, config Config) error

	// Stop closes the connection to the server.
	// It should ensure all pending operations are completed or cancelled.
	Stop(ctx context.Context) error

	// IsConnected returns true if the client is currently connected to the server.
	IsConnected() bool

	// Write sends data to the connected server.
	// Returns an error if not connected or if sending fails.
	Write(data []byte) error

	// SetMessageHandler sets the handler for incoming messages from the server.
	SetMessageHandler(handler func(data []byte) error)

	// SetDisconnectionHandler sets the handler for disconnection events.
	// The handler receives the error that caused the disconnection (if any).
	SetDisconnectionHandler(handler func(err error))

	// SetReconnectionHandler sets the handler for successful reconnection events.
	// This is called when the client successfully reconnects after a disconnection.
	SetReconnectionHandler(handler func())

	// SetErrorHandler sets the handler for transport-level errors.
	SetErrorHandler(handler func(err error))

	// Errors returns a channel that receives transport-level errors.
	// This provides an alternative to SetErrorHandler for error handling.
	Errors() <-chan error
}

// WebSocketConfig contains configuration options for WebSocket transport.
type WebSocketConfig struct {
	// Port is the port number to listen on (server) or connect to (client).
	Port int
	// ListenPath is the HTTP path to listen on (server only).
	ListenPath string
	// TLSConfig contains TLS configuration for secure connections.
	TLSConfig *tls.Config
	// ReadTimeout is the timeout for read operations.
	ReadTimeout time.Duration
	// WriteTimeout is the timeout for write operations.
	WriteTimeout time.Duration
	// HandshakeTimeout is the timeout for the WebSocket handshake.
	HandshakeTimeout time.Duration
	// MaxMessageSize is the maximum size of a message in bytes.
	MaxMessageSize int64
	// CheckOrigin is used to check the origin of upgrade requests.
	CheckOrigin func(origin string) bool
}

// GetType returns WebSocketTransport.
func (c *WebSocketConfig) GetType() TransportType {
	return WebSocketTransport
}

// Validate checks if the WebSocket configuration is valid.
func (c *WebSocketConfig) Validate() error {
	if c.Port <= 0 || c.Port > 65535 {
		return ErrInvalidPort
	}
	if c.ListenPath == "" {
		c.ListenPath = "/"
	}
	if c.ReadTimeout <= 0 {
		c.ReadTimeout = 30 * time.Second
	}
	if c.WriteTimeout <= 0 {
		c.WriteTimeout = 10 * time.Second
	}
	if c.HandshakeTimeout <= 0 {
		c.HandshakeTimeout = 10 * time.Second
	}
	if c.MaxMessageSize <= 0 {
		c.MaxMessageSize = 32 * 1024 // 32KB default
	}
	return nil
}

// RedisConfig contains configuration options for Redis transport.
type RedisConfig struct {
	// Addr is the Redis server address in host:port format.
	Addr string
	// Password is the Redis server password (optional).
	Password string
	// DB is the Redis database number to use.
	DB int
	// ChannelPrefix is the prefix for Redis channels used for communication.
	ChannelPrefix string
	// ClientTimeout is the timeout for Redis client operations.
	ClientTimeout time.Duration
	// ConnectionPoolSize is the size of the Redis connection pool.
	ConnectionPoolSize int
	// RetryAttempts is the number of retry attempts for failed operations.
	RetryAttempts int
	// RetryDelay is the delay between retry attempts.
	RetryDelay time.Duration
}

// GetType returns RedisTransport.
func (c *RedisConfig) GetType() TransportType {
	return RedisTransport
}

// Validate checks if the Redis configuration is valid.
func (c *RedisConfig) Validate() error {
	if c.Addr == "" {
		return ErrInvalidAddress
	}
	if c.ChannelPrefix == "" {
		c.ChannelPrefix = "ocpp"
	}
	if c.ClientTimeout <= 0 {
		c.ClientTimeout = 30 * time.Second
	}
	if c.ConnectionPoolSize <= 0 {
		c.ConnectionPoolSize = 10
	}
	if c.RetryAttempts < 0 {
		c.RetryAttempts = 3
	}
	if c.RetryDelay <= 0 {
		c.RetryDelay = time.Second
	}
	return nil
}