package transport

import (
	"context"
	"fmt"
)

// Factory provides a way to create transport instances based on configuration.
type Factory interface {
	// CreateTransport creates a new server-side transport instance based on the provided configuration.
	CreateTransport(config Config) (Transport, error)

	// CreateClientTransport creates a new client-side transport instance based on the provided configuration.
	CreateClientTransport(config Config) (ClientTransport, error)

	// SupportedTypes returns a list of transport types supported by this factory.
	SupportedTypes() []TransportType
}

// DefaultFactory is the default implementation of the Factory interface.
// It supports creating transports based on the configuration type.
type DefaultFactory struct {
	// transportCreators maps transport types to their creation functions for server transports.
	transportCreators map[TransportType]func(Config) (Transport, error)

	// clientTransportCreators maps transport types to their creation functions for client transports.
	clientTransportCreators map[TransportType]func(Config) (ClientTransport, error)
}

// NewDefaultFactory creates a new DefaultFactory instance.
func NewDefaultFactory() *DefaultFactory {
	return &DefaultFactory{
		transportCreators:       make(map[TransportType]func(Config) (Transport, error)),
		clientTransportCreators: make(map[TransportType]func(Config) (ClientTransport, error)),
	}
}

// RegisterTransport registers a creator function for a specific transport type for server transports.
func (f *DefaultFactory) RegisterTransport(transportType TransportType, creator func(Config) (Transport, error)) {
	f.transportCreators[transportType] = creator
}

// RegisterClientTransport registers a creator function for a specific transport type for client transports.
func (f *DefaultFactory) RegisterClientTransport(transportType TransportType, creator func(Config) (ClientTransport, error)) {
	f.clientTransportCreators[transportType] = creator
}

// CreateTransport creates a new server-side transport instance based on the provided configuration.
func (f *DefaultFactory) CreateTransport(config Config) (Transport, error) {
	if config == nil {
		return nil, ErrInvalidConfig
	}

	if err := config.Validate(); err != nil {
		return nil, fmt.Errorf("invalid config: %w", err)
	}

	transportType := config.GetType()
	creator, exists := f.transportCreators[transportType]
	if !exists {
		return nil, fmt.Errorf("unsupported transport type: %v", transportType)
	}

	return creator(config)
}

// CreateClientTransport creates a new client-side transport instance based on the provided configuration.
func (f *DefaultFactory) CreateClientTransport(config Config) (ClientTransport, error) {
	if config == nil {
		return nil, ErrInvalidConfig
	}

	if err := config.Validate(); err != nil {
		return nil, fmt.Errorf("invalid config: %w", err)
	}

	transportType := config.GetType()
	creator, exists := f.clientTransportCreators[transportType]
	if !exists {
		return nil, fmt.Errorf("unsupported transport type: %v", transportType)
	}

	return creator(config)
}

// SupportedTypes returns a list of transport types supported by this factory.
func (f *DefaultFactory) SupportedTypes() []TransportType {
	types := make([]TransportType, 0, len(f.transportCreators))
	for transportType := range f.transportCreators {
		types = append(types, transportType)
	}
	return types
}

// Manager provides high-level transport management capabilities.
type Manager interface {
	// StartTransport starts a transport with the given configuration and returns the transport instance.
	StartTransport(ctx context.Context, config Config) (Transport, error)

	// StartClientTransport starts a client transport with the given endpoint and configuration.
	StartClientTransport(ctx context.Context, endpoint string, config Config) (ClientTransport, error)

	// StopTransport stops the specified transport.
	StopTransport(ctx context.Context, transport Transport) error

	// StopClientTransport stops the specified client transport.
	StopClientTransport(ctx context.Context, clientTransport ClientTransport) error

	// GetActiveTransports returns a list of currently active server transports.
	GetActiveTransports() []Transport

	// GetActiveClientTransports returns a list of currently active client transports.
	GetActiveClientTransports() []ClientTransport
}

// DefaultManager is the default implementation of the Manager interface.
type DefaultManager struct {
	factory          Factory
	activeTransports []Transport
	activeClients    []ClientTransport
}

// NewDefaultManager creates a new DefaultManager instance with the given factory.
func NewDefaultManager(factory Factory) *DefaultManager {
	return &DefaultManager{
		factory:          factory,
		activeTransports: make([]Transport, 0),
		activeClients:    make([]ClientTransport, 0),
	}
}

// StartTransport starts a transport with the given configuration and returns the transport instance.
func (m *DefaultManager) StartTransport(ctx context.Context, config Config) (Transport, error) {
	transport, err := m.factory.CreateTransport(config)
	if err != nil {
		return nil, err
	}

	if err := transport.Start(ctx, config); err != nil {
		return nil, fmt.Errorf("failed to start transport: %w", err)
	}

	m.activeTransports = append(m.activeTransports, transport)
	return transport, nil
}

// StartClientTransport starts a client transport with the given endpoint and configuration.
func (m *DefaultManager) StartClientTransport(ctx context.Context, endpoint string, config Config) (ClientTransport, error) {
	clientTransport, err := m.factory.CreateClientTransport(config)
	if err != nil {
		return nil, err
	}

	if err := clientTransport.Start(ctx, endpoint, config); err != nil {
		return nil, fmt.Errorf("failed to start client transport: %w", err)
	}

	m.activeClients = append(m.activeClients, clientTransport)
	return clientTransport, nil
}

// StopTransport stops the specified transport.
func (m *DefaultManager) StopTransport(ctx context.Context, transport Transport) error {
	if err := transport.Stop(ctx); err != nil {
		return err
	}

	// Remove from active transports
	for i, t := range m.activeTransports {
		if t == transport {
			m.activeTransports = append(m.activeTransports[:i], m.activeTransports[i+1:]...)
			break
		}
	}

	return nil
}

// StopClientTransport stops the specified client transport.
func (m *DefaultManager) StopClientTransport(ctx context.Context, clientTransport ClientTransport) error {
	if err := clientTransport.Stop(ctx); err != nil {
		return err
	}

	// Remove from active clients
	for i, c := range m.activeClients {
		if c == clientTransport {
			m.activeClients = append(m.activeClients[:i], m.activeClients[i+1:]...)
			break
		}
	}

	return nil
}

// GetActiveTransports returns a list of currently active server transports.
func (m *DefaultManager) GetActiveTransports() []Transport {
	return append([]Transport(nil), m.activeTransports...)
}

// GetActiveClientTransports returns a list of currently active client transports.
func (m *DefaultManager) GetActiveClientTransports() []ClientTransport {
	return append([]ClientTransport(nil), m.activeClients...)
}