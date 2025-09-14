package websocket

import (
	"github.com/lorenzodonini/ocpp-go/transport"
	"github.com/lorenzodonini/ocpp-go/ws"
)

// WebSocketFactory implements transport.Factory interface for WebSocket transport.
// It creates WebSocket adapters that wrap the existing ws package components.
type WebSocketFactory struct{}

// NewWebSocketFactory creates a new WebSocketFactory instance.
func NewWebSocketFactory() *WebSocketFactory {
	return &WebSocketFactory{}
}

// CreateTransport creates a new server-side WebSocket transport instance.
func (f *WebSocketFactory) CreateTransport(config transport.Config) (transport.Transport, error) {
	wsConfig, ok := config.(*transport.WebSocketConfig)
	if !ok {
		return nil, transport.ErrInvalidConfig
	}

	if err := wsConfig.Validate(); err != nil {
		return nil, err
	}

	// Create a new ws.Server with appropriate options
	var serverOpts []ws.ServerOpt
	if wsConfig.TLSConfig != nil {
		// Note: This would require extending ws.NewServer to accept TLS config
		// For now, we'll create a basic server and handle TLS separately
	}

	wsServer := ws.NewServer(serverOpts...)

	// Create and return the adapter
	serverAdapter := NewServerAdapter(wsServer)
	return serverAdapter, nil
}

// CreateClientTransport creates a new client-side WebSocket transport instance.
func (f *WebSocketFactory) CreateClientTransport(config transport.Config) (transport.ClientTransport, error) {
	wsConfig, ok := config.(*transport.WebSocketConfig)
	if !ok {
		return nil, transport.ErrInvalidConfig
	}

	if err := wsConfig.Validate(); err != nil {
		return nil, err
	}

	// Create a new ws.Client with appropriate options
	var clientOpts []ws.ClientOpt
	if wsConfig.TLSConfig != nil {
		clientOpts = append(clientOpts, ws.WithClientTLSConfig(wsConfig.TLSConfig))
	}

	wsClient := ws.NewClient(clientOpts...)

	// Create and return the adapter
	clientAdapter := NewClientAdapter(wsClient)
	return clientAdapter, nil
}

// SupportedTypes returns a list of transport types supported by this factory.
func (f *WebSocketFactory) SupportedTypes() []transport.TransportType {
	return []transport.TransportType{transport.WebSocketTransport}
}

// RegisterWithDefaultFactory registers the WebSocket factory with the default transport factory.
// This allows the WebSocket transport to be used through the standard transport factory interface.
func RegisterWithDefaultFactory(factory *transport.DefaultFactory) {
	wsFactory := NewWebSocketFactory()

	// Register server transport creator
	factory.RegisterTransport(transport.WebSocketTransport, func(config transport.Config) (transport.Transport, error) {
		return wsFactory.CreateTransport(config)
	})

	// Register client transport creator
	factory.RegisterClientTransport(transport.WebSocketTransport, func(config transport.Config) (transport.ClientTransport, error) {
		return wsFactory.CreateClientTransport(config)
	})
}

// CreateServerFromExisting creates a ServerAdapter from an existing ws.Server instance.
// This is useful when you already have a configured ws.Server that you want to wrap.
func CreateServerFromExisting(wsServer ws.Server) transport.Transport {
	return NewServerAdapter(wsServer)
}

// CreateClientFromExisting creates a ClientAdapter from an existing ws.Client instance.
// This is useful when you already have a configured ws.Client that you want to wrap.
func CreateClientFromExisting(wsClient ws.Client) transport.ClientTransport {
	return NewClientAdapter(wsClient)
}