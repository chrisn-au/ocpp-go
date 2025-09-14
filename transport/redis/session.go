package redis

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"
	"time"
)

// SessionData represents the data stored for each session.
type SessionData struct {
	ChargePointID string                 `json:"charge_point_id"`
	RemoteAddr    string                 `json:"remote_addr"`
	WSServerID    string                 `json:"ws_server_id"`
	ConnectedAt   time.Time              `json:"connected_at"`
	LastActivity  time.Time              `json:"last_activity"`
	Metadata      map[string]interface{} `json:"metadata,omitempty"`
}

// SessionManager manages Redis-based sessions for charge points.
type SessionManager struct {
	client   *redisClient
	sessions map[string]*Channel
	mu       sync.RWMutex
}

// NewSessionManager creates a new session manager.
func NewSessionManager(client *redisClient) *SessionManager {
	return &SessionManager{
		client:   client,
		sessions: make(map[string]*Channel),
	}
}

// CreateSession creates a new session for a charge point.
func (sm *SessionManager) CreateSession(ctx context.Context, chargePointID, remoteAddr, wsServerID string) (*Channel, error) {
	sm.mu.Lock()
	defer sm.mu.Unlock()

	// Check if session already exists
	if existing, exists := sm.sessions[chargePointID]; exists {
		// Update existing session
		existing.SetConnected(true)
		existing.UpdateActivity()
		return existing, nil
	}

	// Create new channel
	channel := NewChannel(chargePointID, remoteAddr)
	sm.sessions[chargePointID] = channel

	// Store session data in Redis
	sessionData := SessionData{
		ChargePointID: chargePointID,
		RemoteAddr:    remoteAddr,
		WSServerID:    wsServerID,
		ConnectedAt:   time.Now(),
		LastActivity:  time.Now(),
		Metadata:      make(map[string]interface{}),
	}

	if err := sm.storeSessionData(ctx, chargePointID, sessionData); err != nil {
		delete(sm.sessions, chargePointID)
		return nil, fmt.Errorf("failed to store session data: %w", err)
	}

	// Add to active sessions
	if err := sm.client.addActiveSession(ctx, chargePointID); err != nil {
		delete(sm.sessions, chargePointID)
		return nil, fmt.Errorf("failed to add to active sessions: %w", err)
	}

	return channel, nil
}

// GetSession retrieves a session by charge point ID.
func (sm *SessionManager) GetSession(chargePointID string) (*Channel, bool) {
	sm.mu.RLock()
	defer sm.mu.RUnlock()

	channel, exists := sm.sessions[chargePointID]
	return channel, exists
}

// RemoveSession removes a session for a charge point.
func (sm *SessionManager) RemoveSession(ctx context.Context, chargePointID string) error {
	sm.mu.Lock()
	defer sm.mu.Unlock()

	// Close the channel if it exists
	if channel, exists := sm.sessions[chargePointID]; exists {
		channel.Close()
		delete(sm.sessions, chargePointID)
	}

	// Remove from Redis
	if err := sm.client.deleteSession(ctx, chargePointID); err != nil {
		return fmt.Errorf("failed to delete session data: %w", err)
	}

	// Remove from active sessions
	if err := sm.client.removeActiveSession(ctx, chargePointID); err != nil {
		return fmt.Errorf("failed to remove from active sessions: %w", err)
	}

	return nil
}

// GetActiveSessions returns all active session IDs.
func (sm *SessionManager) GetActiveSessions(ctx context.Context) ([]string, error) {
	return sm.client.getActiveSessions(ctx)
}

// GetAllSessions returns all local sessions.
func (sm *SessionManager) GetAllSessions() map[string]*Channel {
	sm.mu.RLock()
	defer sm.mu.RUnlock()

	result := make(map[string]*Channel)
	for id, channel := range sm.sessions {
		result[id] = channel
	}
	return result
}

// UpdateSessionActivity updates the last activity time for a session.
func (sm *SessionManager) UpdateSessionActivity(ctx context.Context, chargePointID string) error {
	sm.mu.RLock()
	channel, exists := sm.sessions[chargePointID]
	sm.mu.RUnlock()

	if exists {
		channel.UpdateActivity()
	}

	// Update in Redis
	data := map[string]interface{}{
		"last_activity": time.Now().Format(time.RFC3339),
	}

	return sm.client.setSessionData(ctx, chargePointID, data)
}

// LoadSessionsFromRedis loads existing sessions from Redis on startup.
func (sm *SessionManager) LoadSessionsFromRedis(ctx context.Context) error {
	activeIDs, err := sm.client.getActiveSessions(ctx)
	if err != nil {
		return fmt.Errorf("failed to get active sessions: %w", err)
	}

	for _, chargePointID := range activeIDs {
		sessionData, err := sm.loadSessionData(ctx, chargePointID)
		if err != nil {
			// Log error but continue with other sessions
			continue
		}

		// Create channel for existing session
		channel := NewChannel(chargePointID, sessionData.RemoteAddr)
		channel.SetConnected(true)

		sm.mu.Lock()
		sm.sessions[chargePointID] = channel
		sm.mu.Unlock()
	}

	return nil
}

// CleanupExpiredSessions removes sessions that haven't been active for the specified duration.
func (sm *SessionManager) CleanupExpiredSessions(ctx context.Context, maxIdle time.Duration) error {
	sm.mu.RLock()
	var expiredSessions []string
	for id, channel := range sm.sessions {
		if time.Since(channel.LastActivity()) > maxIdle {
			expiredSessions = append(expiredSessions, id)
		}
	}
	sm.mu.RUnlock()

	for _, id := range expiredSessions {
		if err := sm.RemoveSession(ctx, id); err != nil {
			// Log error but continue with cleanup
			continue
		}
	}

	return nil
}

// storeSessionData stores session data in Redis.
func (sm *SessionManager) storeSessionData(ctx context.Context, chargePointID string, data SessionData) error {
	dataMap := map[string]interface{}{
		"charge_point_id": data.ChargePointID,
		"remote_addr":     data.RemoteAddr,
		"ws_server_id":    data.WSServerID,
		"connected_at":    data.ConnectedAt.Format(time.RFC3339),
		"last_activity":   data.LastActivity.Format(time.RFC3339),
	}

	if data.Metadata != nil {
		metadataJSON, _ := json.Marshal(data.Metadata)
		dataMap["metadata"] = string(metadataJSON)
	}

	return sm.client.setSessionData(ctx, chargePointID, dataMap)
}

// loadSessionData loads session data from Redis.
func (sm *SessionManager) loadSessionData(ctx context.Context, chargePointID string) (*SessionData, error) {
	dataMap, err := sm.client.getSessionData(ctx, chargePointID)
	if err != nil {
		return nil, fmt.Errorf("failed to get session data: %w", err)
	}

	sessionData := &SessionData{
		ChargePointID: dataMap["charge_point_id"],
		RemoteAddr:    dataMap["remote_addr"],
		WSServerID:    dataMap["ws_server_id"],
		Metadata:      make(map[string]interface{}),
	}

	if connectedAt, err := time.Parse(time.RFC3339, dataMap["connected_at"]); err == nil {
		sessionData.ConnectedAt = connectedAt
	}

	if lastActivity, err := time.Parse(time.RFC3339, dataMap["last_activity"]); err == nil {
		sessionData.LastActivity = lastActivity
	}

	if metadataStr := dataMap["metadata"]; metadataStr != "" {
		json.Unmarshal([]byte(metadataStr), &sessionData.Metadata)
	}

	return sessionData, nil
}