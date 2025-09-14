package redis

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"
	"time"
)

// CorrelationData represents data stored for message correlation.
type CorrelationData struct {
	MessageID     string                 `json:"message_id"`
	ChargePointID string                 `json:"charge_point_id"`
	RequestTime   time.Time              `json:"request_time"`
	Timeout       time.Duration          `json:"timeout"`
	Metadata      map[string]interface{} `json:"metadata,omitempty"`
}

// PendingRequest represents a pending CALL message waiting for a response.
type PendingRequest struct {
	MessageID     string
	ChargePointID string
	RequestTime   time.Time
	ResponseCh    chan []byte
	ErrorCh       chan error
	Timeout       time.Duration
	ctx           context.Context
	cancel        context.CancelFunc
}

// CorrelationManager manages message correlation for OCPP request/response pairs.
type CorrelationManager struct {
	client           *redisClient
	pendingRequests  map[string]*PendingRequest
	mu               sync.RWMutex
	defaultTimeout   time.Duration
	cleanupInterval  time.Duration
	ctx              context.Context
	cancel           context.CancelFunc
	wg               sync.WaitGroup
}

// NewCorrelationManager creates a new correlation manager.
func NewCorrelationManager(client *redisClient, defaultTimeout time.Duration) *CorrelationManager {
	if defaultTimeout <= 0 {
		defaultTimeout = 30 * time.Second
	}

	ctx, cancel := context.WithCancel(context.Background())

	cm := &CorrelationManager{
		client:          client,
		pendingRequests: make(map[string]*PendingRequest),
		defaultTimeout:  defaultTimeout,
		cleanupInterval: 1 * time.Minute,
		ctx:             ctx,
		cancel:          cancel,
	}

	// Start cleanup routine
	cm.wg.Add(1)
	go cm.cleanupRoutine()

	return cm
}

// TrackRequest tracks an outgoing CALL message and returns channels for the response.
func (cm *CorrelationManager) TrackRequest(ctx context.Context, messageID, chargePointID string, timeout time.Duration) (<-chan []byte, <-chan error, error) {
	if timeout <= 0 {
		timeout = cm.defaultTimeout
	}

	cm.mu.Lock()
	defer cm.mu.Unlock()

	// Check if already tracking
	if _, exists := cm.pendingRequests[messageID]; exists {
		return nil, nil, fmt.Errorf("message ID %s already being tracked", messageID)
	}

	// Create pending request
	reqCtx, cancel := context.WithTimeout(ctx, timeout)
	pending := &PendingRequest{
		MessageID:     messageID,
		ChargePointID: chargePointID,
		RequestTime:   time.Now(),
		ResponseCh:    make(chan []byte, 1),
		ErrorCh:       make(chan error, 1),
		Timeout:       timeout,
		ctx:           reqCtx,
		cancel:        cancel,
	}

	cm.pendingRequests[messageID] = pending

	// Store correlation data in Redis with TTL
	correlationData := CorrelationData{
		MessageID:     messageID,
		ChargePointID: chargePointID,
		RequestTime:   time.Now(),
		Timeout:       timeout,
	}

	if err := cm.storeCorrelationData(ctx, messageID, correlationData, timeout); err != nil {
		delete(cm.pendingRequests, messageID)
		cancel()
		return nil, nil, fmt.Errorf("failed to store correlation data: %w", err)
	}

	// Start timeout handler
	go cm.handleTimeout(pending)

	return pending.ResponseCh, pending.ErrorCh, nil
}

// CompleteRequest completes a tracked request with a response.
func (cm *CorrelationManager) CompleteRequest(messageID string, response []byte) error {
	cm.mu.Lock()
	pending, exists := cm.pendingRequests[messageID]
	if !exists {
		cm.mu.Unlock()
		return fmt.Errorf("no pending request found for message ID %s", messageID)
	}

	// Send response and cleanup before unlocking
	select {
	case pending.ResponseCh <- response:
		cm.cleanupPendingRequest(messageID)
	default:
		// Channel full, cleanup anyway
		cm.cleanupPendingRequest(messageID)
	}
	cm.mu.Unlock()

	return nil
}

// FailRequest fails a tracked request with an error.
func (cm *CorrelationManager) FailRequest(messageID string, err error) error {
	cm.mu.Lock()
	pending, exists := cm.pendingRequests[messageID]
	if !exists {
		cm.mu.Unlock()
		return fmt.Errorf("no pending request found for message ID %s", messageID)
	}

	// Send error and cleanup before unlocking
	select {
	case pending.ErrorCh <- err:
		cm.cleanupPendingRequest(messageID)
	default:
		// Channel full, cleanup anyway
		cm.cleanupPendingRequest(messageID)
	}
	cm.mu.Unlock()

	return nil
}

// GetPendingRequestsForChargePoint returns all pending requests for a charge point.
func (cm *CorrelationManager) GetPendingRequestsForChargePoint(chargePointID string) []*PendingRequest {
	cm.mu.RLock()
	defer cm.mu.RUnlock()

	var requests []*PendingRequest
	for _, pending := range cm.pendingRequests {
		if pending.ChargePointID == chargePointID {
			requests = append(requests, pending)
		}
	}

	return requests
}

// GetPendingRequestCount returns the number of pending requests.
func (cm *CorrelationManager) GetPendingRequestCount() int {
	cm.mu.RLock()
	defer cm.mu.RUnlock()
	return len(cm.pendingRequests)
}

// Close shuts down the correlation manager.
func (cm *CorrelationManager) Close() error {
	cm.cancel()
	cm.wg.Wait()

	cm.mu.Lock()
	defer cm.mu.Unlock()

	// Fail all pending requests
	for messageID := range cm.pendingRequests {
		if pending, exists := cm.pendingRequests[messageID]; exists {
			select {
			case pending.ErrorCh <- fmt.Errorf("correlation manager closed"):
			default:
			}
		}
		cm.cleanupPendingRequest(messageID)
	}

	return nil
}

// cleanupPendingRequest removes a pending request without locking (assumes caller holds lock).
func (cm *CorrelationManager) cleanupPendingRequest(messageID string) {
	if pending, exists := cm.pendingRequests[messageID]; exists {
		pending.cancel()
		delete(cm.pendingRequests, messageID)

		// Remove from Redis (best effort)
		go func() {
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			cm.client.deleteCorrelation(ctx, messageID)
		}()
	}
}

// removePendingRequest removes a pending request and cleans up its resources (with locking).
func (cm *CorrelationManager) removePendingRequest(messageID string) {
	cm.mu.Lock()
	defer cm.mu.Unlock()
	cm.cleanupPendingRequest(messageID)
}

// handleTimeout handles request timeouts.
func (cm *CorrelationManager) handleTimeout(pending *PendingRequest) {
	select {
	case <-pending.ctx.Done():
		// Context cancelled or timed out
		if pending.ctx.Err() == context.DeadlineExceeded {
			cm.FailRequest(pending.MessageID, fmt.Errorf("request timeout after %v", pending.Timeout))
		}
	}
}

// cleanupRoutine periodically cleans up expired correlations.
func (cm *CorrelationManager) cleanupRoutine() {
	defer cm.wg.Done()

	ticker := time.NewTicker(cm.cleanupInterval)
	defer ticker.Stop()

	for {
		select {
		case <-cm.ctx.Done():
			return
		case <-ticker.C:
			cm.cleanupExpiredRequests()
		}
	}
}

// cleanupExpiredRequests removes expired requests from local memory.
func (cm *CorrelationManager) cleanupExpiredRequests() {
	cm.mu.Lock()
	defer cm.mu.Unlock()

	var expiredIDs []string
	for messageID, pending := range cm.pendingRequests {
		if time.Since(pending.RequestTime) > pending.Timeout {
			expiredIDs = append(expiredIDs, messageID)
		}
	}

	for _, messageID := range expiredIDs {
		cm.cleanupPendingRequest(messageID)
	}
}

// storeCorrelationData stores correlation data in Redis with TTL.
func (cm *CorrelationManager) storeCorrelationData(ctx context.Context, messageID string, data CorrelationData, ttl time.Duration) error {
	return cm.client.setCorrelation(ctx, messageID, data, ttl)
}

// loadCorrelationData loads correlation data from Redis.
func (cm *CorrelationManager) loadCorrelationData(ctx context.Context, messageID string) (*CorrelationData, error) {
	dataStr, err := cm.client.getCorrelation(ctx, messageID)
	if err != nil {
		return nil, fmt.Errorf("failed to get correlation data: %w", err)
	}

	var data CorrelationData
	if err := json.Unmarshal([]byte(dataStr), &data); err != nil {
		return nil, fmt.Errorf("failed to unmarshal correlation data: %w", err)
	}

	return &data, nil
}