package redis

import (
	"context"
	"net"
	"sync"
	"time"
)

// Channel represents a logical Redis-based communication channel.
// It implements the transport.Channel interface but represents a logical
// session rather than a physical connection.
type Channel struct {
	chargePointID  string
	remoteAddr     net.Addr
	connected      bool
	lastActivity   time.Time
	ctx            context.Context
	cancel         context.CancelFunc
	mu             sync.RWMutex
	metadata       map[string]interface{}
}

// NewChannel creates a new Redis channel for the specified charge point.
func NewChannel(chargePointID string, remoteAddr string) *Channel {
	ctx, cancel := context.WithCancel(context.Background())

	// Parse remote address
	var addr net.Addr
	if remoteAddr != "" {
		if tcpAddr, err := net.ResolveTCPAddr("tcp", remoteAddr); err == nil {
			addr = tcpAddr
		} else {
			// Fallback to a custom addr implementation
			addr = &redisAddr{addr: remoteAddr}
		}
	}

	return &Channel{
		chargePointID: chargePointID,
		remoteAddr:    addr,
		connected:     true,
		lastActivity:  time.Now(),
		ctx:           ctx,
		cancel:        cancel,
		metadata:      make(map[string]interface{}),
	}
}

// ID returns the charge point ID for this channel.
func (c *Channel) ID() string {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.chargePointID
}

// RemoteAddr returns the remote address of the charge point.
func (c *Channel) RemoteAddr() net.Addr {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.remoteAddr
}

// IsConnected returns true if the channel is connected.
func (c *Channel) IsConnected() bool {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.connected
}

// SetConnected sets the connection status of the channel.
func (c *Channel) SetConnected(connected bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.connected = connected
	c.lastActivity = time.Now()
}

// LastActivity returns the last activity time for this channel.
func (c *Channel) LastActivity() time.Time {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.lastActivity
}

// UpdateActivity updates the last activity time to now.
func (c *Channel) UpdateActivity() {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.lastActivity = time.Now()
}

// Context returns the channel's context.
func (c *Channel) Context() context.Context {
	return c.ctx
}

// Close closes the channel and cancels its context.
func (c *Channel) Close() error {
	c.mu.Lock()
	defer c.mu.Unlock()

	c.connected = false
	c.cancel()
	return nil
}

// SetMetadata sets metadata for the channel.
func (c *Channel) SetMetadata(key string, value interface{}) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.metadata[key] = value
}

// GetMetadata gets metadata from the channel.
func (c *Channel) GetMetadata(key string) (interface{}, bool) {
	c.mu.RLock()
	defer c.mu.RUnlock()
	val, exists := c.metadata[key]
	return val, exists
}

// GetAllMetadata returns a copy of all metadata.
func (c *Channel) GetAllMetadata() map[string]interface{} {
	c.mu.RLock()
	defer c.mu.RUnlock()

	result := make(map[string]interface{})
	for k, v := range c.metadata {
		result[k] = v
	}
	return result
}

// redisAddr is a custom implementation of net.Addr for Redis addresses.
type redisAddr struct {
	addr string
}

// Network returns the network type.
func (r *redisAddr) Network() string {
	return "redis"
}

// String returns the address string.
func (r *redisAddr) String() string {
	return r.addr
}