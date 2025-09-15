package redis

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/go-redis/redis/v8"
	"github.com/lorenzodonini/ocpp-go/ocppj"
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

// CreateServerState creates a server state implementation based on configuration.
// If distributed state is disabled, returns memory-based state.
// If enabled, creates Redis-backed distributed state.
func (rf *RedisFactory) CreateServerState(config *transport.RedisConfig) (ocppj.ServerState, error) {
	if !config.UseDistributedState {
		return ocppj.NewServerState(&sync.RWMutex{}), nil
	}

	redisClient := redis.NewClient(&redis.Options{
		Addr:     config.Addr,
		Password: config.Password,
		DB:       config.DB + 1, // Use different DB for state
	})

	// Test connection
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	if err := redisClient.Ping(ctx).Err(); err != nil {
		return nil, fmt.Errorf("failed to connect to Redis for state: %w", err)
	}

	return ocppj.NewRedisServerState(redisClient, config.StateKeyPrefix, config.StateTTL), nil
}

// CreateBusinessState creates a Redis-backed business state manager
func (rf *RedisFactory) CreateBusinessState(config *transport.RedisConfig) (*ocppj.RedisBusinessState, error) {
	redisClient := redis.NewClient(&redis.Options{
		Addr:     config.Addr,
		Password: config.Password,
		DB:       config.DB + 2, // Use different DB for business state (DB 0 = transport, DB 1 = request state, DB 2 = business state)
	})

	// Test connection
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	if err := redisClient.Ping(ctx).Err(); err != nil {
		return nil, fmt.Errorf("failed to connect to Redis for business state: %w", err)
	}

	return ocppj.NewRedisBusinessState(redisClient, config.StateKeyPrefix, 24*time.Hour), nil
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