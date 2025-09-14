package redis

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"
	"time"

	"github.com/go-redis/redis/v8"
)

// MessageEnvelope represents the message format used by the WS server.
type MessageEnvelope struct {
	ChargePointID string      `json:"charge_point_id"`
	MessageType   int         `json:"message_type"`
	MessageID     string      `json:"message_id"`
	Action        string      `json:"action,omitempty"`
	Payload       interface{} `json:"payload"`
	Timestamp     time.Time   `json:"timestamp"`
	WSServerID    string      `json:"ws_server_id"`
}

// ConnectionEvent represents connection events from the WS server.
type ConnectionEvent struct {
	Event         string    `json:"event"`          // "connect" or "disconnect"
	ChargePointID string    `json:"charge_point_id"`
	WSServerID    string    `json:"ws_server_id"`
	Timestamp     time.Time `json:"timestamp"`
	RemoteAddr    string    `json:"remote_addr"`
}

// RedisKeys defines the Redis key patterns used by the transport.
type RedisKeys struct {
	Requests    string
	Responses   string
	Control     string
	Session     string
	Active      string
	Correlation string
}

// DefaultRedisKeys returns the default Redis key patterns.
func DefaultRedisKeys(prefix string) RedisKeys {
	if prefix == "" {
		prefix = "ocpp"
	}
	return RedisKeys{
		Requests:    prefix + ":requests",
		Responses:   prefix + ":responses:",
		Control:     prefix + ":control:",
		Session:     prefix + ":session:",
		Active:      prefix + ":active",
		Correlation: prefix + ":correlation:",
	}
}

// redisClient wraps the Redis client with convenience methods.
type redisClient struct {
	*redis.Client
	keys RedisKeys
}

// newRedisClient creates a new Redis client wrapper.
func newRedisClient(addr, password string, db int, prefix string) *redisClient {
	client := redis.NewClient(&redis.Options{
		Addr:     addr,
		Password: password,
		DB:       db,
	})

	return &redisClient{
		Client: client,
		keys:   DefaultRedisKeys(prefix),
	}
}

// publishMessage publishes a message to a Redis channel.
func (r *redisClient) publishMessage(ctx context.Context, channel string, data interface{}) error {
	payload, err := json.Marshal(data)
	if err != nil {
		return fmt.Errorf("failed to marshal message: %w", err)
	}

	return r.Publish(ctx, channel, payload).Err()
}

// subscribeToChannel subscribes to a Redis channel and returns a channel for messages.
func (r *redisClient) subscribeToChannel(ctx context.Context, channel string) (*redis.PubSub, <-chan *redis.Message, error) {
	pubsub := r.Subscribe(ctx, channel)
	ch := pubsub.Channel()
	return pubsub, ch, nil
}

// popFromQueue pops a message from a Redis list (blocking).
func (r *redisClient) popFromQueue(ctx context.Context, queue string, timeout time.Duration) (string, error) {
	result := r.BRPop(ctx, timeout, queue)
	if result.Err() != nil {
		return "", result.Err()
	}

	vals := result.Val()
	if len(vals) != 2 {
		return "", fmt.Errorf("unexpected BRPOP result format")
	}

	return vals[1], nil
}

// pushToQueue pushes a message to a Redis list.
func (r *redisClient) pushToQueue(ctx context.Context, queue string, data interface{}) error {
	var payload []byte
	var err error

	// Check if data is already a byte slice
	if b, ok := data.([]byte); ok {
		payload = b
	} else {
		payload, err = json.Marshal(data)
		if err != nil {
			return fmt.Errorf("failed to marshal message: %w", err)
		}
	}

	return r.LPush(ctx, queue, payload).Err()
}

// setSessionData sets session data in Redis.
func (r *redisClient) setSessionData(ctx context.Context, chargePointID string, data map[string]interface{}) error {
	sessionKey := r.keys.Session + chargePointID
	return r.HMSet(ctx, sessionKey, data).Err()
}

// getSessionData gets session data from Redis.
func (r *redisClient) getSessionData(ctx context.Context, chargePointID string) (map[string]string, error) {
	sessionKey := r.keys.Session + chargePointID
	return r.HGetAll(ctx, sessionKey).Result()
}

// deleteSession deletes session data from Redis.
func (r *redisClient) deleteSession(ctx context.Context, chargePointID string) error {
	sessionKey := r.keys.Session + chargePointID
	return r.Del(ctx, sessionKey).Err()
}

// addActiveSession adds a charge point to the active set.
func (r *redisClient) addActiveSession(ctx context.Context, chargePointID string) error {
	return r.SAdd(ctx, r.keys.Active, chargePointID).Err()
}

// removeActiveSession removes a charge point from the active set.
func (r *redisClient) removeActiveSession(ctx context.Context, chargePointID string) error {
	return r.SRem(ctx, r.keys.Active, chargePointID).Err()
}

// getActiveSessions gets all active charge point IDs.
func (r *redisClient) getActiveSessions(ctx context.Context) ([]string, error) {
	return r.SMembers(ctx, r.keys.Active).Result()
}

// setCorrelation sets correlation data with TTL.
func (r *redisClient) setCorrelation(ctx context.Context, messageID string, data interface{}, ttl time.Duration) error {
	key := r.keys.Correlation + messageID
	payload, err := json.Marshal(data)
	if err != nil {
		return fmt.Errorf("failed to marshal correlation data: %w", err)
	}

	return r.SetEX(ctx, key, payload, ttl).Err()
}

// getCorrelation gets correlation data.
func (r *redisClient) getCorrelation(ctx context.Context, messageID string) (string, error) {
	key := r.keys.Correlation + messageID
	return r.Get(ctx, key).Result()
}

// deleteCorrelation deletes correlation data.
func (r *redisClient) deleteCorrelation(ctx context.Context, messageID string) error {
	key := r.keys.Correlation + messageID
	return r.Del(ctx, key).Err()
}

// baseTransport provides common functionality for both server and client transports.
type baseTransport struct {
	client    *redisClient
	ctx       context.Context
	cancel    context.CancelFunc
	running   bool
	mu        sync.RWMutex
	errorCh   chan error
	wg        sync.WaitGroup
}

// newBaseTransport creates a new base transport.
func newBaseTransport(client *redisClient) *baseTransport {
	return &baseTransport{
		client:  client,
		errorCh: make(chan error, 10),
	}
}

// start initializes the base transport context.
func (bt *baseTransport) start() {
	bt.mu.Lock()
	defer bt.mu.Unlock()

	if bt.running {
		return
	}

	bt.ctx, bt.cancel = context.WithCancel(context.Background())
	bt.running = true
}

// stop shuts down the base transport.
func (bt *baseTransport) stop() error {
	bt.mu.Lock()
	defer bt.mu.Unlock()

	if !bt.running {
		return nil
	}

	bt.cancel()
	bt.running = false

	// Wait for all goroutines to finish
	done := make(chan struct{})
	go func() {
		bt.wg.Wait()
		close(done)
	}()

	select {
	case <-done:
		return nil
	case <-time.After(10 * time.Second):
		return fmt.Errorf("timeout waiting for transport to stop")
	}
}

// isRunning returns true if the transport is running.
func (bt *baseTransport) isRunning() bool {
	bt.mu.RLock()
	defer bt.mu.RUnlock()
	return bt.running
}

// errors returns the error channel.
func (bt *baseTransport) errors() <-chan error {
	return bt.errorCh
}

// reportError reports an error to the error channel.
func (bt *baseTransport) reportError(err error) {
	select {
	case bt.errorCh <- err:
	default:
		// Channel is full, drop the error
	}
}