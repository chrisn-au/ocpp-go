package ocppj

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/go-redis/redis/v8"
)

// Business state types for OCPP management
type ChargePointInfo struct {
	ClientID      string            `json:"clientId"`
	LastSeen      time.Time         `json:"lastSeen"`
	IsOnline      bool              `json:"isOnline"`
	Configuration map[string]string `json:"configuration"`
}

type TransactionInfo struct {
	TransactionID int       `json:"transactionId"`
	ClientID      string    `json:"clientId"`
	ConnectorID   int       `json:"connectorId"`
	IdTag         string    `json:"idTag"`
	StartTime     time.Time `json:"startTime"`
	MeterStart    int       `json:"meterStart"`
	CurrentMeter  int       `json:"currentMeter"`
	Status        string    `json:"status"` // "Active", "Stopped"
}

type ConnectorStatus struct {
	Status      string `json:"status"`      // "Available", "Occupied", "Charging", "Faulted"
	Transaction *int   `json:"transaction"` // nil if available, transactionID if charging
}

// RedisBusinessState manages OCPP business state in Redis
type RedisBusinessState struct {
	client    *redis.Client
	keyPrefix string
	ttl       time.Duration
}

// NewRedisBusinessState creates a new Redis business state manager
func NewRedisBusinessState(client *redis.Client, keyPrefix string, ttl time.Duration) *RedisBusinessState {
	if ttl == 0 {
		ttl = 24 * time.Hour // Default TTL for business state
	}
	return &RedisBusinessState{
		client:    client,
		keyPrefix: keyPrefix,
		ttl:       ttl,
	}
}

// ChargePoint management
func (r *RedisBusinessState) SetChargePointInfo(info *ChargePointInfo) error {
	key := fmt.Sprintf("%s:chargepoint:%s", r.keyPrefix, info.ClientID)
	ctx := context.Background()

	data, err := json.Marshal(info)
	if err != nil {
		return fmt.Errorf("failed to marshal charge point info: %w", err)
	}

	err = r.client.Set(ctx, key, data, r.ttl).Err()
	if err != nil {
		return fmt.Errorf("failed to set charge point info: %w", err)
	}

	return nil
}

func (r *RedisBusinessState) GetChargePointInfo(clientID string) (*ChargePointInfo, error) {
	key := fmt.Sprintf("%s:chargepoint:%s", r.keyPrefix, clientID)
	ctx := context.Background()

	data, err := r.client.Get(ctx, key).Result()
	if err == redis.Nil {
		return nil, nil // Not found
	}
	if err != nil {
		return nil, fmt.Errorf("failed to get charge point info: %w", err)
	}

	var info ChargePointInfo
	err = json.Unmarshal([]byte(data), &info)
	if err != nil {
		return nil, fmt.Errorf("failed to unmarshal charge point info: %w", err)
	}

	return &info, nil
}

func (r *RedisBusinessState) UpdateChargePointLastSeen(clientID string) error {
	info, err := r.GetChargePointInfo(clientID)
	if err != nil {
		return err
	}

	if info == nil {
		// Create new charge point info
		info = &ChargePointInfo{
			ClientID:      clientID,
			LastSeen:      time.Now(),
			IsOnline:      true,
			Configuration: make(map[string]string),
		}
	} else {
		info.LastSeen = time.Now()
		info.IsOnline = true
	}

	return r.SetChargePointInfo(info)
}

func (r *RedisBusinessState) SetChargePointOffline(clientID string) error {
	info, err := r.GetChargePointInfo(clientID)
	if err != nil {
		return err
	}

	if info != nil {
		info.IsOnline = false
		return r.SetChargePointInfo(info)
	}

	return nil
}

func (r *RedisBusinessState) GetAllChargePoints() ([]*ChargePointInfo, error) {
	pattern := fmt.Sprintf("%s:chargepoint:*", r.keyPrefix)
	ctx := context.Background()

	keys, err := r.client.Keys(ctx, pattern).Result()
	if err != nil {
		return nil, fmt.Errorf("failed to get charge point keys: %w", err)
	}

	var chargePoints []*ChargePointInfo
	for _, key := range keys {
		data, err := r.client.Get(ctx, key).Result()
		if err != nil {
			log.Errorf("Failed to get charge point data for key %s: %v", key, err)
			continue
		}

		var info ChargePointInfo
		err = json.Unmarshal([]byte(data), &info)
		if err != nil {
			log.Errorf("Failed to unmarshal charge point data for key %s: %v", key, err)
			continue
		}

		chargePoints = append(chargePoints, &info)
	}

	return chargePoints, nil
}

// Transaction management
func (r *RedisBusinessState) CreateTransaction(info *TransactionInfo) error {
	key := fmt.Sprintf("%s:transaction:%d", r.keyPrefix, info.TransactionID)
	ctx := context.Background()

	data, err := json.Marshal(info)
	if err != nil {
		return fmt.Errorf("failed to marshal transaction info: %w", err)
	}

	err = r.client.Set(ctx, key, data, r.ttl).Err()
	if err != nil {
		return fmt.Errorf("failed to create transaction: %w", err)
	}

	return nil
}

func (r *RedisBusinessState) GetTransaction(transactionID int) (*TransactionInfo, error) {
	key := fmt.Sprintf("%s:transaction:%d", r.keyPrefix, transactionID)
	ctx := context.Background()

	data, err := r.client.Get(ctx, key).Result()
	if err == redis.Nil {
		return nil, nil // Not found
	}
	if err != nil {
		return nil, fmt.Errorf("failed to get transaction: %w", err)
	}

	var info TransactionInfo
	err = json.Unmarshal([]byte(data), &info)
	if err != nil {
		return nil, fmt.Errorf("failed to unmarshal transaction: %w", err)
	}

	return &info, nil
}

func (r *RedisBusinessState) UpdateTransaction(info *TransactionInfo) error {
	return r.CreateTransaction(info) // Same implementation
}

func (r *RedisBusinessState) DeleteTransaction(transactionID int) error {
	key := fmt.Sprintf("%s:transaction:%d", r.keyPrefix, transactionID)
	ctx := context.Background()

	err := r.client.Del(ctx, key).Err()
	if err != nil {
		return fmt.Errorf("failed to delete transaction: %w", err)
	}

	return nil
}

func (r *RedisBusinessState) GetActiveTransactions(clientID string) ([]*TransactionInfo, error) {
	pattern := fmt.Sprintf("%s:transaction:*", r.keyPrefix)
	ctx := context.Background()

	keys, err := r.client.Keys(ctx, pattern).Result()
	if err != nil {
		return nil, fmt.Errorf("failed to get transaction keys: %w", err)
	}

	var transactions []*TransactionInfo
	for _, key := range keys {
		data, err := r.client.Get(ctx, key).Result()
		if err != nil {
			log.Errorf("Failed to get transaction data for key %s: %v", key, err)
			continue
		}

		var info TransactionInfo
		err = json.Unmarshal([]byte(data), &info)
		if err != nil {
			log.Errorf("Failed to unmarshal transaction data for key %s: %v", key, err)
			continue
		}

		// Filter by clientID if specified
		if clientID == "" || info.ClientID == clientID {
			if info.Status == "Active" {
				transactions = append(transactions, &info)
			}
		}
	}

	return transactions, nil
}

// Connector management
func (r *RedisBusinessState) SetConnectorStatus(clientID string, connectorID int, status *ConnectorStatus) error {
	key := fmt.Sprintf("%s:connector:%s:%d", r.keyPrefix, clientID, connectorID)
	ctx := context.Background()

	data, err := json.Marshal(status)
	if err != nil {
		return fmt.Errorf("failed to marshal connector status: %w", err)
	}

	err = r.client.Set(ctx, key, data, r.ttl).Err()
	if err != nil {
		return fmt.Errorf("failed to set connector status: %w", err)
	}

	return nil
}

func (r *RedisBusinessState) GetConnectorStatus(clientID string, connectorID int) (*ConnectorStatus, error) {
	key := fmt.Sprintf("%s:connector:%s:%d", r.keyPrefix, clientID, connectorID)
	ctx := context.Background()

	data, err := r.client.Get(ctx, key).Result()
	if err == redis.Nil {
		return nil, nil // Not found
	}
	if err != nil {
		return nil, fmt.Errorf("failed to get connector status: %w", err)
	}

	var status ConnectorStatus
	err = json.Unmarshal([]byte(data), &status)
	if err != nil {
		return nil, fmt.Errorf("failed to unmarshal connector status: %w", err)
	}

	return &status, nil
}

func (r *RedisBusinessState) GetAllConnectors(clientID string) (map[int]*ConnectorStatus, error) {
	pattern := fmt.Sprintf("%s:connector:%s:*", r.keyPrefix, clientID)
	ctx := context.Background()

	keys, err := r.client.Keys(ctx, pattern).Result()
	if err != nil {
		return nil, fmt.Errorf("failed to get connector keys: %w", err)
	}

	connectors := make(map[int]*ConnectorStatus)
	for _, key := range keys {
		data, err := r.client.Get(ctx, key).Result()
		if err != nil {
			log.Errorf("Failed to get connector data for key %s: %v", key, err)
			continue
		}

		var status ConnectorStatus
		err = json.Unmarshal([]byte(data), &status)
		if err != nil {
			log.Errorf("Failed to unmarshal connector data for key %s: %v", key, err)
			continue
		}

		// Extract connector ID from key (last part after last colon)
		var connectorID int
		fmt.Sscanf(key, fmt.Sprintf("%s:connector:%s:%%d", r.keyPrefix, clientID), &connectorID)
		connectors[connectorID] = &status
	}

	return connectors, nil
}

// Helper methods for atomic operations
func (r *RedisBusinessState) StartTransaction(clientID string, connectorID int, transactionID int, idTag string, meterStart int) error {
	ctx := context.Background()

	// Create transaction
	transaction := &TransactionInfo{
		TransactionID: transactionID,
		ClientID:      clientID,
		ConnectorID:   connectorID,
		IdTag:         idTag,
		StartTime:     time.Now(),
		MeterStart:    meterStart,
		CurrentMeter:  meterStart,
		Status:        "Active",
	}

	// Update connector status
	connectorStatus := &ConnectorStatus{
		Status:      "Charging",
		Transaction: &transactionID,
	}

	// Use pipeline for atomic operation
	pipe := r.client.Pipeline()

	// Add transaction
	transactionKey := fmt.Sprintf("%s:transaction:%d", r.keyPrefix, transactionID)
	transactionData, _ := json.Marshal(transaction)
	pipe.Set(ctx, transactionKey, transactionData, r.ttl)

	// Update connector
	connectorKey := fmt.Sprintf("%s:connector:%s:%d", r.keyPrefix, clientID, connectorID)
	connectorData, _ := json.Marshal(connectorStatus)
	pipe.Set(ctx, connectorKey, connectorData, r.ttl)

	_, err := pipe.Exec(ctx)
	if err != nil {
		return fmt.Errorf("failed to start transaction atomically: %w", err)
	}

	return nil
}

func (r *RedisBusinessState) StopTransaction(transactionID int, meterStop int) error {
	ctx := context.Background()

	// Get transaction to find connector
	transaction, err := r.GetTransaction(transactionID)
	if err != nil {
		return err
	}
	if transaction == nil {
		return fmt.Errorf("transaction %d not found", transactionID)
	}

	// Update transaction status
	transaction.Status = "Stopped"
	transaction.CurrentMeter = meterStop

	// Update connector status
	connectorStatus := &ConnectorStatus{
		Status:      "Available",
		Transaction: nil,
	}

	// Use pipeline for atomic operation
	pipe := r.client.Pipeline()

	// Update transaction
	transactionKey := fmt.Sprintf("%s:transaction:%d", r.keyPrefix, transactionID)
	transactionData, _ := json.Marshal(transaction)
	pipe.Set(ctx, transactionKey, transactionData, r.ttl)

	// Update connector
	connectorKey := fmt.Sprintf("%s:connector:%s:%d", r.keyPrefix, transaction.ClientID, transaction.ConnectorID)
	connectorData, _ := json.Marshal(connectorStatus)
	pipe.Set(ctx, connectorKey, connectorData, r.ttl)

	_, err = pipe.Exec(ctx)
	if err != nil {
		return fmt.Errorf("failed to stop transaction atomically: %w", err)
	}

	return nil
}

// GetChargePointConfiguration retrieves configuration for a charge point
func (rbs *RedisBusinessState) GetChargePointConfiguration(clientID string) (map[string]string, error) {
	key := fmt.Sprintf("%s:config:%s", rbs.keyPrefix, clientID)
	ctx := context.Background()

	result, err := rbs.client.HGetAll(ctx, key).Result()
	if err != nil {
		return nil, err
	}

	return result, nil
}

// SetChargePointConfiguration stores configuration for a charge point
func (rbs *RedisBusinessState) SetChargePointConfiguration(clientID string, config map[string]string) error {
	key := fmt.Sprintf("%s:config:%s", rbs.keyPrefix, clientID)
	ctx := context.Background()

	// Delete existing configuration
	if err := rbs.client.Del(ctx, key).Err(); err != nil {
		return err
	}

	// Set new configuration
	if len(config) > 0 {
		if err := rbs.client.HMSet(ctx, key, config).Err(); err != nil {
			return err
		}

		// Set expiry to keep data fresh
		rbs.client.Expire(ctx, key, 7*24*time.Hour)
	}

	return nil
}

// Generic Redis operations for meter values and other data

// Set stores a value in Redis with default TTL
func (r *RedisBusinessState) Set(ctx context.Context, key, value string) error {
	return r.client.Set(ctx, key, value, r.ttl).Err()
}

// Get retrieves a value from Redis
func (r *RedisBusinessState) Get(ctx context.Context, key string) (string, error) {
	result, err := r.client.Get(ctx, key).Result()
	if err == redis.Nil {
		return "", fmt.Errorf("key not found: %s", key)
	}
	return result, err
}

// SetWithTTL stores a value in Redis with custom TTL
func (r *RedisBusinessState) SetWithTTL(ctx context.Context, key, value string, ttl time.Duration) error {
	return r.client.Set(ctx, key, value, ttl).Err()
}