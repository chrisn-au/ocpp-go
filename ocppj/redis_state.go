package ocppj

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"
	"time"

	"github.com/go-redis/redis/v8"
	"github.com/lorenzodonini/ocpp-go/ocpp"
)

// RedisServerState implements ServerState interface using Redis for distributed state
type RedisServerState struct {
	client     *redis.Client
	keyPrefix  string
	defaultTTL time.Duration
	mu         sync.RWMutex
}

// NewRedisServerState creates a new Redis-backed ServerState
func NewRedisServerState(client *redis.Client, keyPrefix string, ttl time.Duration) ServerState {
	if ttl == 0 {
		ttl = 30 * time.Second
	}
	return &RedisServerState{
		client:     client,
		keyPrefix:  keyPrefix,
		defaultTTL: ttl,
	}
}

// RequestMetadata stores minimal info needed for request correlation
type RequestMetadata struct {
	FeatureName string `json:"featureName"`
}

// SimpleRequest implements ocpp.Request interface with just a feature name
type SimpleRequest struct {
	featureName string
}

func (r *SimpleRequest) GetFeatureName() string {
	return r.featureName
}

// AddPendingRequest stores a pending request with feature name in Redis
func (r *RedisServerState) AddPendingRequest(clientID, requestID string, req ocpp.Request) {
	key := fmt.Sprintf("%s:pending:%s", r.keyPrefix, clientID)
	ctx := context.Background()

	// Store the feature name which is all we need for ParseMessage
	metadata := RequestMetadata{
		FeatureName: req.GetFeatureName(),
	}

	metadataBytes, err := json.Marshal(metadata)
	if err != nil {
		log.Errorf("Redis state error serializing metadata: %v", err)
		return
	}

	err = r.client.HSet(ctx, key, requestID, metadataBytes).Err()
	if err != nil {
		log.Errorf("Redis state error adding pending request: %v", err)
		return
	}

	r.client.Expire(ctx, key, r.defaultTTL)
}

// GetClientState returns a Redis-backed ClientState for the given client
func (r *RedisServerState) GetClientState(clientID string) ClientState {
	return &RedisClientState{
		clientID:   clientID,
		redis:      r.client,
		keyPrefix:  r.keyPrefix,
		defaultTTL: r.defaultTTL,
	}
}

// HasPendingRequest checks if a specific client has any pending requests
func (r *RedisServerState) HasPendingRequest(clientID string) bool {
	key := fmt.Sprintf("%s:pending:%s", r.keyPrefix, clientID)
	ctx := context.Background()

	count, err := r.client.HLen(ctx, key).Result()
	if err != nil {
		log.Errorf("Redis state error checking pending requests: %v", err)
		return false
	}
	return count > 0
}

// HasPendingRequests checks if any client has pending requests
func (r *RedisServerState) HasPendingRequests() bool {
	pattern := fmt.Sprintf("%s:pending:*", r.keyPrefix)
	ctx := context.Background()

	keys, err := r.client.Keys(ctx, pattern).Result()
	if err != nil {
		log.Errorf("Redis state error checking all pending requests: %v", err)
		return false
	}

	for _, key := range keys {
		count, err := r.client.HLen(ctx, key).Result()
		if err == nil && count > 0 {
			return true
		}
	}
	return false
}

// DeletePendingRequest removes a specific pending request
func (r *RedisServerState) DeletePendingRequest(clientID, requestID string) {
	key := fmt.Sprintf("%s:pending:%s", r.keyPrefix, clientID)
	ctx := context.Background()

	err := r.client.HDel(ctx, key, requestID).Err()
	if err != nil {
		log.Errorf("Redis state error deleting pending request: %v", err)
	}
}

// ClearClientPendingRequest removes all pending requests for a client
func (r *RedisServerState) ClearClientPendingRequest(clientID string) {
	key := fmt.Sprintf("%s:pending:%s", r.keyPrefix, clientID)
	ctx := context.Background()

	err := r.client.Del(ctx, key).Err()
	if err != nil {
		log.Errorf("Redis state error clearing client pending requests: %v", err)
	}
}

// ClearAllPendingRequests removes all pending requests for all clients
func (r *RedisServerState) ClearAllPendingRequests() {
	pattern := fmt.Sprintf("%s:pending:*", r.keyPrefix)
	ctx := context.Background()

	keys, err := r.client.Keys(ctx, pattern).Result()
	if err != nil {
		log.Errorf("Redis state error getting keys for clear all: %v", err)
		return
	}

	if len(keys) > 0 {
		err = r.client.Del(ctx, keys...).Err()
		if err != nil {
			log.Errorf("Redis state error clearing all pending requests: %v", err)
		}
	}
}

// RedisClientState implements ClientState interface using Redis
type RedisClientState struct {
	clientID   string
	redis      *redis.Client
	keyPrefix  string
	defaultTTL time.Duration
}

// HasPendingRequest checks if this client has any pending requests
func (r *RedisClientState) HasPendingRequest() bool {
	key := fmt.Sprintf("%s:pending:%s", r.keyPrefix, r.clientID)
	ctx := context.Background()

	count, err := r.redis.HLen(ctx, key).Result()
	if err != nil {
		log.Errorf("Redis state error checking client pending requests: %v", err)
		return false
	}
	return count > 0
}

// GetPendingRequest retrieves a specific pending request with feature name
func (r *RedisClientState) GetPendingRequest(requestID string) (ocpp.Request, bool) {
	key := fmt.Sprintf("%s:pending:%s", r.keyPrefix, r.clientID)
	ctx := context.Background()

	// Get the metadata for this request
	metadataBytes, err := r.redis.HGet(ctx, key, requestID).Result()
	if err != nil {
		if err.Error() == "redis: nil" {
			return nil, false
		}
		log.Errorf("Redis state error getting pending request: %v", err)
		return nil, false
	}

	// Deserialize the metadata
	var metadata RequestMetadata
	err = json.Unmarshal([]byte(metadataBytes), &metadata)
	if err != nil {
		log.Errorf("Redis state error deserializing metadata: %v", err)
		return nil, false
	}

	// Return a SimpleRequest with the stored feature name
	return &SimpleRequest{featureName: metadata.FeatureName}, true
}

// AddPendingRequest stores a pending request with feature name
func (r *RedisClientState) AddPendingRequest(requestID string, req ocpp.Request) {
	key := fmt.Sprintf("%s:pending:%s", r.keyPrefix, r.clientID)
	ctx := context.Background()

	// Store the feature name which is all we need for ParseMessage
	metadata := RequestMetadata{
		FeatureName: req.GetFeatureName(),
	}

	metadataBytes, err := json.Marshal(metadata)
	if err != nil {
		log.Errorf("Redis state error serializing metadata: %v", err)
		return
	}

	err = r.redis.HSet(ctx, key, requestID, metadataBytes).Err()
	if err != nil {
		log.Errorf("Redis state error adding client pending request: %v", err)
		return
	}

	r.redis.Expire(ctx, key, r.defaultTTL)
}

// DeletePendingRequest removes a specific pending request
func (r *RedisClientState) DeletePendingRequest(requestID string) {
	key := fmt.Sprintf("%s:pending:%s", r.keyPrefix, r.clientID)
	ctx := context.Background()

	err := r.redis.HDel(ctx, key, requestID).Err()
	if err != nil {
		log.Errorf("Redis state error deleting client pending request: %v", err)
	}
}

// ClearPendingRequests removes all pending requests for this client
func (r *RedisClientState) ClearPendingRequests() {
	key := fmt.Sprintf("%s:pending:%s", r.keyPrefix, r.clientID)
	ctx := context.Background()

	err := r.redis.Del(ctx, key).Err()
	if err != nil {
		log.Errorf("Redis state error clearing client pending requests: %v", err)
	}
}