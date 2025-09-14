package websocket

import (
	"context"
	"fmt"

	"github.com/lorenzodonini/ocpp-go/transport"
	"github.com/lorenzodonini/ocpp-go/ws"
)

// ClientAdapter wraps a ws.Client to implement transport.ClientTransport interface.
// It provides a bridge between the transport abstraction and the existing ws package.
type ClientAdapter struct {
	wsClient     ws.Client
	isConnected  bool
	errorChannel chan error

	// Transport handlers
	messageHandler       func(data []byte) error
	disconnectionHandler func(err error)
	reconnectionHandler  func()
	errorHandler         func(err error)
}

// NewClientAdapter creates a new ClientAdapter wrapping the given ws.Client.
func NewClientAdapter(wsClient ws.Client) *ClientAdapter {
	adapter := &ClientAdapter{
		wsClient:     wsClient,
		errorChannel: make(chan error, 100), // Buffered to prevent blocking
	}

	// Set up ws.Client callbacks to bridge to transport handlers
	adapter.setupWSCallbacks()

	return adapter
}

// setupWSCallbacks configures the ws.Client callbacks to bridge to transport handlers.
func (c *ClientAdapter) setupWSCallbacks() {
	// Set message handler
	c.wsClient.SetMessageHandler(func(data []byte) error {
		if c.messageHandler != nil {
			return c.messageHandler(data)
		}
		return nil
	})

	// Set disconnection handler
	c.wsClient.SetDisconnectedHandler(func(err error) {
		c.isConnected = false
		if c.disconnectionHandler != nil {
			c.disconnectionHandler(err)
		}
	})

	// Set reconnection handler
	c.wsClient.SetReconnectedHandler(func() {
		c.isConnected = true
		if c.reconnectionHandler != nil {
			c.reconnectionHandler()
		}
	})

	// Forward errors from ws.Client to our error channel
	if errChan := c.wsClient.Errors(); errChan != nil {
		go func() {
			for err := range errChan {
				select {
				case c.errorChannel <- err:
				default:
					// Error channel is full, drop the error to prevent blocking
				}
				if c.errorHandler != nil {
					c.errorHandler(err)
				}
			}
		}()
	}
}

// Start establishes a connection to the server at the given endpoint.
func (c *ClientAdapter) Start(ctx context.Context, endpoint string, config transport.Config) error {
	wsConfig, ok := config.(*transport.WebSocketConfig)
	if !ok {
		return transport.ErrInvalidConfig
	}

	if err := wsConfig.Validate(); err != nil {
		return err
	}

	// Apply configuration to ws.Client
	if wsConfig.ReadTimeout > 0 || wsConfig.WriteTimeout > 0 || wsConfig.HandshakeTimeout > 0 {
		clientTimeout := ws.ClientTimeoutConfig{
			WriteWait:        wsConfig.WriteTimeout,
			HandshakeTimeout: wsConfig.HandshakeTimeout,
			PongWait:         wsConfig.ReadTimeout,
		}
		c.wsClient.SetTimeoutConfig(clientTimeout)
	}

	// Start the connection
	err := c.wsClient.Start(endpoint)
	if err != nil {
		return fmt.Errorf("failed to start WebSocket client: %w", err)
	}

	c.isConnected = true
	return nil
}

// Stop closes the connection to the server.
func (c *ClientAdapter) Stop(ctx context.Context) error {
	if !c.isConnected {
		return nil
	}

	c.wsClient.Stop()
	c.isConnected = false

	// Close error channel
	close(c.errorChannel)

	return nil
}

// IsConnected returns true if the client is currently connected to the server.
func (c *ClientAdapter) IsConnected() bool {
	// Use the ws.Client's IsConnected method for the most accurate status
	return c.wsClient.IsConnected()
}

// Write sends data to the connected server.
func (c *ClientAdapter) Write(data []byte) error {
	if !c.IsConnected() {
		return transport.ErrNotConnected
	}

	return c.wsClient.Write(data)
}

// SetMessageHandler sets the handler for incoming messages from the server.
func (c *ClientAdapter) SetMessageHandler(handler func(data []byte) error) {
	c.messageHandler = handler
}

// SetDisconnectionHandler sets the handler for disconnection events.
func (c *ClientAdapter) SetDisconnectionHandler(handler func(err error)) {
	c.disconnectionHandler = handler
}

// SetReconnectionHandler sets the handler for successful reconnection events.
func (c *ClientAdapter) SetReconnectionHandler(handler func()) {
	c.reconnectionHandler = handler
}

// SetErrorHandler sets the handler for transport-level errors.
func (c *ClientAdapter) SetErrorHandler(handler func(err error)) {
	c.errorHandler = handler
}

// Errors returns a channel that receives transport-level errors.
func (c *ClientAdapter) Errors() <-chan error {
	return c.errorChannel
}

// GetWrappedClient returns the underlying ws.Client.
// This is useful for accessing ws-specific functionality when needed.
func (c *ClientAdapter) GetWrappedClient() ws.Client {
	return c.wsClient
}