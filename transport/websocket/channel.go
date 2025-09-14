package websocket

import (
	"context"
	"crypto/tls"
	"net"

	"github.com/lorenzodonini/ocpp-go/ws"
)

// ChannelAdapter wraps a ws.Channel to implement transport.Channel interface.
// It provides context support and maps ws.Channel methods to the transport layer.
type ChannelAdapter struct {
	wsChannel ws.Channel
	ctx       context.Context
}

// NewChannelAdapter creates a new ChannelAdapter wrapping the given ws.Channel.
func NewChannelAdapter(wsChannel ws.Channel, ctx context.Context) *ChannelAdapter {
	if ctx == nil {
		ctx = context.Background()
	}
	return &ChannelAdapter{
		wsChannel: wsChannel,
		ctx:       ctx,
	}
}

// ID returns the unique identifier of the client, which identifies this unique channel.
// This extracts the charge point ID from the ws.Channel.
func (c *ChannelAdapter) ID() string {
	if c.wsChannel == nil {
		return ""
	}
	return c.wsChannel.ID()
}

// RemoteAddr returns the remote IP network address of the connected peer.
func (c *ChannelAdapter) RemoteAddr() net.Addr {
	if c.wsChannel == nil {
		return nil
	}
	return c.wsChannel.RemoteAddr()
}

// TLSConnectionState returns information about the active TLS connection, if any.
func (c *ChannelAdapter) TLSConnectionState() *tls.ConnectionState {
	if c.wsChannel == nil {
		return nil
	}
	return c.wsChannel.TLSConnectionState()
}

// IsConnected returns true if the connection to the peer is active, false if it was closed already.
func (c *ChannelAdapter) IsConnected() bool {
	if c.wsChannel == nil {
		return false
	}
	return c.wsChannel.IsConnected()
}

// Context returns the context associated with this channel.
// This provides context support that wasn't available in the original ws.Channel.
func (c *ChannelAdapter) Context() context.Context {
	return c.ctx
}

// SetContext updates the context associated with this channel.
func (c *ChannelAdapter) SetContext(ctx context.Context) {
	if ctx != nil {
		c.ctx = ctx
	}
}

// GetWrappedChannel returns the underlying ws.Channel.
// This is useful for accessing ws-specific functionality when needed.
func (c *ChannelAdapter) GetWrappedChannel() ws.Channel {
	return c.wsChannel
}