package websocket

import (
	"context"
	"sync"

	"github.com/lorenzodonini/ocpp-go/transport"
	"github.com/lorenzodonini/ocpp-go/ws"
)

// ServerAdapter wraps a ws.Server to implement transport.Transport interface.
// It provides a bridge between the transport abstraction and the existing ws package.
type ServerAdapter struct {
	wsServer          ws.Server
	isRunning         bool
	connectedChannels map[string]*ChannelAdapter
	channelMutex      sync.RWMutex
	errorChannel      chan error

	// Transport handlers
	messageHandler      transport.MessageHandler
	connectionHandler   transport.ConnectionHandler
	disconnectionHandler transport.DisconnectionHandler
	errorHandler        transport.ErrorHandler
}

// NewServerAdapter creates a new ServerAdapter wrapping the given ws.Server.
func NewServerAdapter(wsServer ws.Server) *ServerAdapter {
	adapter := &ServerAdapter{
		wsServer:          wsServer,
		connectedChannels: make(map[string]*ChannelAdapter),
		errorChannel:      make(chan error, 100), // Buffered to prevent blocking
	}

	// Set up ws.Server callbacks to bridge to transport handlers
	adapter.setupWSCallbacks()

	return adapter
}

// setupWSCallbacks configures the ws.Server callbacks to bridge to transport handlers.
func (s *ServerAdapter) setupWSCallbacks() {
	// Set message handler
	s.wsServer.SetMessageHandler(func(wsChannel ws.Channel, data []byte) error {
		if s.messageHandler != nil {
			return s.messageHandler(wsChannel.ID(), data)
		}
		return nil
	})

	// Set new client handler
	s.wsServer.SetNewClientHandler(func(wsChannel ws.Channel) {
		// Store the channel adapter
		s.channelMutex.Lock()
		channelAdapter := NewChannelAdapter(wsChannel, context.Background())
		s.connectedChannels[wsChannel.ID()] = channelAdapter
		s.channelMutex.Unlock()

		if s.connectionHandler != nil {
			s.connectionHandler(wsChannel.ID())
		}
	})

	// Set disconnected client handler
	s.wsServer.SetDisconnectedClientHandler(func(wsChannel ws.Channel) {
		clientID := wsChannel.ID()

		// Remove from our registry
		s.channelMutex.Lock()
		delete(s.connectedChannels, clientID)
		s.channelMutex.Unlock()

		if s.disconnectionHandler != nil {
			s.disconnectionHandler(clientID, nil) // ws doesn't provide error details in disconnect callback
		}
	})
}

// Start initializes and starts the transport with the given configuration.
func (s *ServerAdapter) Start(ctx context.Context, config transport.Config) error {
	wsConfig, ok := config.(*transport.WebSocketConfig)
	if !ok {
		return transport.ErrInvalidConfig
	}

	if err := wsConfig.Validate(); err != nil {
		return err
	}

	// Apply configuration to ws.Server
	if wsConfig.TLSConfig != nil {
		// Note: ws.Server TLS configuration would need to be applied here
		// This might require extending the ws.Server interface or using server options
	}

	// Start the ws.Server in a goroutine since ws.Start is blocking
	go func() {
		s.isRunning = true
		// Forward errors from ws.Server to our error channel
		if errChan := s.wsServer.Errors(); errChan != nil {
			go func() {
				for err := range errChan {
					select {
					case s.errorChannel <- err:
					default:
						// Error channel is full, drop the error to prevent blocking
					}
					if s.errorHandler != nil {
						s.errorHandler("", err) // No specific client ID for server errors
					}
				}
			}()
		}

		s.wsServer.Start(wsConfig.Port, wsConfig.ListenPath)
		s.isRunning = false
	}()

	return nil
}

// Stop gracefully shuts down the transport, closing all connections.
func (s *ServerAdapter) Stop(ctx context.Context) error {
	if !s.isRunning {
		return nil
	}

	s.wsServer.Stop()
	s.isRunning = false

	// Clear all connected channels
	s.channelMutex.Lock()
	s.connectedChannels = make(map[string]*ChannelAdapter)
	s.channelMutex.Unlock()

	// Close error channel
	close(s.errorChannel)

	return nil
}

// IsRunning returns true if the transport is currently running and accepting connections.
func (s *ServerAdapter) IsRunning() bool {
	return s.isRunning
}

// Write sends data to a specific client identified by clientID.
func (s *ServerAdapter) Write(clientID string, data []byte) error {
	if !s.isRunning {
		return transport.ErrNotStarted
	}

	return s.wsServer.Write(clientID, data)
}

// WriteToAll broadcasts data to all connected clients.
func (s *ServerAdapter) WriteToAll(data []byte) map[string]error {
	errors := make(map[string]error)

	if !s.isRunning {
		// Return error for all connected clients
		s.channelMutex.RLock()
		for clientID := range s.connectedChannels {
			errors[clientID] = transport.ErrNotStarted
		}
		s.channelMutex.RUnlock()
		return errors
	}

	s.channelMutex.RLock()
	connectedIDs := make([]string, 0, len(s.connectedChannels))
	for clientID := range s.connectedChannels {
		connectedIDs = append(connectedIDs, clientID)
	}
	s.channelMutex.RUnlock()

	// Send to each connected client
	for _, clientID := range connectedIDs {
		if err := s.wsServer.Write(clientID, data); err != nil {
			errors[clientID] = err
		}
	}

	return errors
}

// GetConnectedClients returns a slice of currently connected client IDs.
func (s *ServerAdapter) GetConnectedClients() []string {
	s.channelMutex.RLock()
	defer s.channelMutex.RUnlock()

	clients := make([]string, 0, len(s.connectedChannels))
	for clientID := range s.connectedChannels {
		clients = append(clients, clientID)
	}
	return clients
}

// IsClientConnected checks if a specific client is currently connected.
func (s *ServerAdapter) IsClientConnected(clientID string) bool {
	s.channelMutex.RLock()
	defer s.channelMutex.RUnlock()

	_, exists := s.connectedChannels[clientID]
	return exists
}

// SetMessageHandler sets the handler for incoming messages.
func (s *ServerAdapter) SetMessageHandler(handler transport.MessageHandler) {
	s.messageHandler = handler
}

// SetConnectionHandler sets the handler for new client connections.
func (s *ServerAdapter) SetConnectionHandler(handler transport.ConnectionHandler) {
	s.connectionHandler = handler
}

// SetDisconnectionHandler sets the handler for client disconnections.
func (s *ServerAdapter) SetDisconnectionHandler(handler transport.DisconnectionHandler) {
	s.disconnectionHandler = handler
}

// SetErrorHandler sets the handler for transport-level errors.
func (s *ServerAdapter) SetErrorHandler(handler transport.ErrorHandler) {
	s.errorHandler = handler
}

// Errors returns a channel that receives transport-level errors.
func (s *ServerAdapter) Errors() <-chan error {
	return s.errorChannel
}

// GetChannel retrieves a channel adapter for the given client ID.
// This provides access to the underlying ws.Channel if needed.
func (s *ServerAdapter) GetChannel(clientID string) (*ChannelAdapter, bool) {
	s.channelMutex.RLock()
	defer s.channelMutex.RUnlock()

	channel, exists := s.connectedChannels[clientID]
	return channel, exists
}