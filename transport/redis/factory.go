package redis

import (
	"github.com/lorenzodonini/ocpp-go/transport"
)

// RedisFactory implements the transport.Factory interface for Redis transports.
type RedisFactory struct{}

// NewRedisFactory creates a new Redis transport factory.
func NewRedisFactory() *RedisFactory {
	return &RedisFactory{}
}

// CreateTransport creates a new server-side Redis transport instance.
func (rf *RedisFactory) CreateTransport(config transport.Config) (transport.Transport, error) {
	redisConfig, ok := config.(*transport.RedisConfig)
	if !ok {
		return nil, transport.ErrInvalidConfig
	}

	if err := redisConfig.Validate(); err != nil {
		return nil, err
	}

	return NewRedisServerTransport(), nil
}

// CreateClientTransport creates a new client-side Redis transport instance.
func (rf *RedisFactory) CreateClientTransport(config transport.Config) (transport.ClientTransport, error) {
	redisConfig, ok := config.(*transport.RedisConfig)
	if !ok {
		return nil, transport.ErrInvalidConfig
	}

	if err := redisConfig.Validate(); err != nil {
		return nil, err
	}

	return NewRedisClientTransport(), nil
}

// SupportedTypes returns the transport types supported by this factory.
func (rf *RedisFactory) SupportedTypes() []transport.TransportType {
	return []transport.TransportType{transport.RedisTransport}
}

// RegisterWithDefaultFactory registers the Redis factory with the default transport factory.
func RegisterWithDefaultFactory(factory *transport.DefaultFactory) {
	redisFactory := NewRedisFactory()

	factory.RegisterTransport(transport.RedisTransport, func(config transport.Config) (transport.Transport, error) {
		return redisFactory.CreateTransport(config)
	})

	factory.RegisterClientTransport(transport.RedisTransport, func(config transport.Config) (transport.ClientTransport, error) {
		return redisFactory.CreateClientTransport(config)
	})
}