package websocket

import (
	"context"
	"crypto/tls"
	"net"
	"sync"
	"testing"

	"github.com/lorenzodonini/ocpp-go/transport"
	"github.com/lorenzodonini/ocpp-go/ws"
)

// TestWebSocketFactory tests the WebSocketFactory implementation
func TestWebSocketFactory(t *testing.T) {
	factory := NewWebSocketFactory()

	// Test supported types
	supportedTypes := factory.SupportedTypes()
	if len(supportedTypes) != 1 || supportedTypes[0] != transport.WebSocketTransport {
		t.Errorf("Expected [WebSocketTransport], got %v", supportedTypes)
	}

	// Test creating server transport
	config := &transport.WebSocketConfig{
		Port:       8080,
		ListenPath: "/ws/{id}",
	}

	serverTransport, err := factory.CreateTransport(config)
	if err != nil {
		t.Fatalf("Failed to create server transport: %v", err)
	}

	if serverTransport == nil {
		t.Fatal("Server transport is nil")
	}

	// Verify it's the correct type
	if _, ok := serverTransport.(*ServerAdapter); !ok {
		t.Errorf("Expected *ServerAdapter, got %T", serverTransport)
	}

	// Test creating client transport
	clientTransport, err := factory.CreateClientTransport(config)
	if err != nil {
		t.Fatalf("Failed to create client transport: %v", err)
	}

	if clientTransport == nil {
		t.Fatal("Client transport is nil")
	}

	// Verify it's the correct type
	if _, ok := clientTransport.(*ClientAdapter); !ok {
		t.Errorf("Expected *ClientAdapter, got %T", clientTransport)
	}
}

// TestWebSocketFactoryInvalidConfig tests factory with invalid configuration
func TestWebSocketFactoryInvalidConfig(t *testing.T) {
	factory := NewWebSocketFactory()

	// Test with nil config
	_, err := factory.CreateTransport(nil)
	if err != transport.ErrInvalidConfig {
		t.Errorf("Expected ErrInvalidConfig, got %v", err)
	}

	// Test with wrong config type
	redisConfig := &transport.RedisConfig{
		Addr: "localhost:6379",
	}
	_, err = factory.CreateTransport(redisConfig)
	if err != transport.ErrInvalidConfig {
		t.Errorf("Expected ErrInvalidConfig, got %v", err)
	}
}

// TestServerAdapter tests the ServerAdapter implementation
func TestServerAdapter(t *testing.T) {
	// Create a ws.Server and wrap it
	wsServer := ws.NewServer()
	serverAdapter := NewServerAdapter(wsServer)

	// Test initial state
	if serverAdapter.IsRunning() {
		t.Error("Server should not be running initially")
	}

	if len(serverAdapter.GetConnectedClients()) != 0 {
		t.Error("Should have no connected clients initially")
	}

	if serverAdapter.IsClientConnected("test-client") {
		t.Error("Should not have test-client connected")
	}

	// Test setting handlers (just verify they don't panic)
	serverAdapter.SetMessageHandler(func(clientID string, data []byte) error {
		return nil
	})

	serverAdapter.SetConnectionHandler(func(clientID string) {
	})

	serverAdapter.SetDisconnectionHandler(func(clientID string, err error) {
	})

	serverAdapter.SetErrorHandler(func(clientID string, err error) {
	})

	// Test error channel
	errChan := serverAdapter.Errors()
	if errChan == nil {
		t.Error("Error channel should not be nil")
	}

	// Test WriteToAll with no clients (should not error)
	errors := serverAdapter.WriteToAll([]byte("test message"))
	if len(errors) != 0 {
		t.Errorf("Expected no errors for empty client list, got %v", errors)
	}
}

// TestClientAdapter tests the ClientAdapter implementation
func TestClientAdapter(t *testing.T) {
	// Create a ws.Client and wrap it
	wsClient := ws.NewClient()
	clientAdapter := NewClientAdapter(wsClient)

	// Test initial state
	if clientAdapter.IsConnected() {
		t.Error("Client should not be connected initially")
	}

	// Test setting handlers (just verify they don't panic)
	clientAdapter.SetMessageHandler(func(data []byte) error {
		return nil
	})

	clientAdapter.SetDisconnectionHandler(func(err error) {
	})

	clientAdapter.SetReconnectionHandler(func() {
	})

	clientAdapter.SetErrorHandler(func(err error) {
	})

	// Test error channel
	errChan := clientAdapter.Errors()
	if errChan == nil {
		t.Error("Error channel should not be nil")
	}

	// Test write when not connected
	err := clientAdapter.Write([]byte("test message"))
	if err != transport.ErrNotConnected {
		t.Errorf("Expected ErrNotConnected, got %v", err)
	}
}

// TestChannelAdapter tests the ChannelAdapter implementation
func TestChannelAdapter(t *testing.T) {
	// Create a mock ws.Channel using a test implementation
	mockChannel := &mockWSChannel{
		id:          "test-client-123",
		isConnected: true,
	}

	ctx := context.Background()
	channelAdapter := NewChannelAdapter(mockChannel, ctx)

	// Test ID
	if channelAdapter.ID() != "test-client-123" {
		t.Errorf("Expected ID 'test-client-123', got %s", channelAdapter.ID())
	}

	// Test IsConnected
	if !channelAdapter.IsConnected() {
		t.Error("Channel should be connected")
	}

	// Test Context
	if channelAdapter.Context() != ctx {
		t.Error("Context should match the provided context")
	}

	// Test SetContext
	newCtx := context.WithValue(ctx, "key", "value")
	channelAdapter.SetContext(newCtx)
	if channelAdapter.Context() != newCtx {
		t.Error("Context should be updated")
	}

	// Test GetWrappedChannel
	wrapped := channelAdapter.GetWrappedChannel()
	if wrapped == nil {
		t.Error("GetWrappedChannel should not return nil")
	}

	// Test with nil channel
	nilAdapter := NewChannelAdapter(nil, ctx)
	if nilAdapter.ID() != "" {
		t.Error("Nil channel should return empty ID")
	}
	if nilAdapter.IsConnected() {
		t.Error("Nil channel should not be connected")
	}
}

// TestCreateFromExisting tests creating adapters from existing ws components
func TestCreateFromExisting(t *testing.T) {
	// Test CreateServerFromExisting
	wsServer := ws.NewServer()
	serverTransport := CreateServerFromExisting(wsServer)
	if serverTransport == nil {
		t.Error("CreateServerFromExisting should not return nil")
	}
	if _, ok := serverTransport.(*ServerAdapter); !ok {
		t.Errorf("Expected *ServerAdapter, got %T", serverTransport)
	}

	// Test CreateClientFromExisting
	wsClient := ws.NewClient()
	clientTransport := CreateClientFromExisting(wsClient)
	if clientTransport == nil {
		t.Error("CreateClientFromExisting should not return nil")
	}
	if _, ok := clientTransport.(*ClientAdapter); !ok {
		t.Errorf("Expected *ClientAdapter, got %T", clientTransport)
	}
}

// TestRegisterWithDefaultFactory tests registering with the default factory
func TestRegisterWithDefaultFactory(t *testing.T) {
	factory := transport.NewDefaultFactory()

	// Initially should not support WebSocket
	initialTypes := factory.SupportedTypes()
	for _, typ := range initialTypes {
		if typ == transport.WebSocketTransport {
			t.Error("Factory should not support WebSocket initially")
		}
	}

	// Register WebSocket support
	RegisterWithDefaultFactory(factory)

	// Now should support WebSocket
	supportedTypes := factory.SupportedTypes()
	found := false
	for _, typ := range supportedTypes {
		if typ == transport.WebSocketTransport {
			found = true
			break
		}
	}
	if !found {
		t.Error("Factory should support WebSocket after registration")
	}

	// Test creating through default factory
	config := &transport.WebSocketConfig{
		Port:       8080,
		ListenPath: "/ws/{id}",
	}

	serverTransport, err := factory.CreateTransport(config)
	if err != nil {
		t.Fatalf("Failed to create transport through default factory: %v", err)
	}
	if _, ok := serverTransport.(*ServerAdapter); !ok {
		t.Errorf("Expected *ServerAdapter, got %T", serverTransport)
	}

	clientTransport, err := factory.CreateClientTransport(config)
	if err != nil {
		t.Fatalf("Failed to create client transport through default factory: %v", err)
	}
	if _, ok := clientTransport.(*ClientAdapter); !ok {
		t.Errorf("Expected *ClientAdapter, got %T", clientTransport)
	}
}

// mockWSChannel is a test implementation of ws.Channel
type mockWSChannel struct {
	id          string
	isConnected bool
	mu          sync.RWMutex
}

func (m *mockWSChannel) ID() string {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.id
}

func (m *mockWSChannel) RemoteAddr() net.Addr {
	return nil
}

func (m *mockWSChannel) TLSConnectionState() *tls.ConnectionState {
	return nil
}

func (m *mockWSChannel) IsConnected() bool {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.isConnected
}

func (m *mockWSChannel) SetConnected(connected bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.isConnected = connected
}