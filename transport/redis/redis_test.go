package redis

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/lorenzodonini/ocpp-go/transport"
	"github.com/go-redis/redis/v8"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const (
	testRedisAddr = "localhost:6379"
	testPrefix    = "test_ocpp"
)

func getTestConfig() *transport.RedisConfig {
	return &transport.RedisConfig{
		Addr:               testRedisAddr,
		ChannelPrefix:      testPrefix,
		ClientTimeout:      10 * time.Second,
		ConnectionPoolSize: 5,
		RetryAttempts:      3,
		RetryDelay:         time.Second,
	}
}

func setupTestRedis(t *testing.T) *redisClient {
	config := getTestConfig()
	client := newRedisClient(config.Addr, config.Password, config.DB, config.ChannelPrefix)

	// Test connection
	ctx := context.Background()
	err := client.Ping(ctx).Err()
	if err != nil {
		t.Skipf("Redis not available at %s: %v", testRedisAddr, err)
	}

	// Clean up test keys
	pattern := testPrefix + ":*"
	keys, _ := client.Keys(ctx, pattern).Result()
	if len(keys) > 0 {
		client.Del(ctx, keys...)
	}

	return client
}

func TestRedisClient(t *testing.T) {
	client := setupTestRedis(t)
	defer client.Close()

	ctx := context.Background()

	t.Run("Queue Operations", func(t *testing.T) {
		queueName := testPrefix + ":test_queue"

		// Test push to queue
		testData := map[string]interface{}{
			"test": "data",
			"timestamp": time.Now(),
		}

		err := client.pushToQueue(ctx, queueName, testData)
		require.NoError(t, err)

		// Test pop from queue
		result, err := client.popFromQueue(ctx, queueName, 1*time.Second)
		require.NoError(t, err)

		var retrieved map[string]interface{}
		err = json.Unmarshal([]byte(result), &retrieved)
		require.NoError(t, err)

		assert.Equal(t, testData["test"], retrieved["test"])
	})

	t.Run("Session Operations", func(t *testing.T) {
		chargePointID := "CP001"

		// Test set session data
		sessionData := map[string]interface{}{
			"charge_point_id": chargePointID,
			"remote_addr":     "192.168.1.100:8080",
			"connected_at":    time.Now().Format(time.RFC3339),
		}

		err := client.setSessionData(ctx, chargePointID, sessionData)
		require.NoError(t, err)

		// Test get session data
		retrieved, err := client.getSessionData(ctx, chargePointID)
		require.NoError(t, err)

		assert.Equal(t, chargePointID, retrieved["charge_point_id"])
		assert.Equal(t, "192.168.1.100:8080", retrieved["remote_addr"])

		// Test add to active sessions
		err = client.addActiveSession(ctx, chargePointID)
		require.NoError(t, err)

		// Test get active sessions
		active, err := client.getActiveSessions(ctx)
		require.NoError(t, err)
		assert.Contains(t, active, chargePointID)

		// Test remove from active sessions
		err = client.removeActiveSession(ctx, chargePointID)
		require.NoError(t, err)

		// Test delete session
		err = client.deleteSession(ctx, chargePointID)
		require.NoError(t, err)
	})

	t.Run("Correlation Operations", func(t *testing.T) {
		messageID := "msg-123"
		correlationData := map[string]interface{}{
			"message_id":      messageID,
			"charge_point_id": "CP001",
			"request_time":    time.Now().Format(time.RFC3339),
		}

		// Test set correlation with TTL
		err := client.setCorrelation(ctx, messageID, correlationData, 10*time.Second)
		require.NoError(t, err)

		// Test get correlation
		result, err := client.getCorrelation(ctx, messageID)
		require.NoError(t, err)

		var retrieved map[string]interface{}
		err = json.Unmarshal([]byte(result), &retrieved)
		require.NoError(t, err)

		assert.Equal(t, messageID, retrieved["message_id"])

		// Test delete correlation
		err = client.deleteCorrelation(ctx, messageID)
		require.NoError(t, err)

		// Verify deletion
		_, err = client.getCorrelation(ctx, messageID)
		assert.Equal(t, redis.Nil, err)
	})
}

func TestChannel(t *testing.T) {
	chargePointID := "CP001"
	remoteAddr := "192.168.1.100:8080"

	channel := NewChannel(chargePointID, remoteAddr)

	t.Run("Basic Properties", func(t *testing.T) {
		assert.Equal(t, chargePointID, channel.ID())
		assert.True(t, channel.IsConnected())
		assert.NotNil(t, channel.RemoteAddr())
		assert.NotZero(t, channel.LastActivity())
	})

	t.Run("Connection State", func(t *testing.T) {
		channel.SetConnected(false)
		assert.False(t, channel.IsConnected())

		channel.SetConnected(true)
		assert.True(t, channel.IsConnected())
	})

	t.Run("Activity Tracking", func(t *testing.T) {
		initialTime := channel.LastActivity()
		time.Sleep(10 * time.Millisecond)

		channel.UpdateActivity()
		newTime := channel.LastActivity()
		assert.True(t, newTime.After(initialTime))
	})

	t.Run("Metadata", func(t *testing.T) {
		key := "test_key"
		value := "test_value"

		channel.SetMetadata(key, value)

		retrieved, exists := channel.GetMetadata(key)
		assert.True(t, exists)
		assert.Equal(t, value, retrieved)

		allMetadata := channel.GetAllMetadata()
		assert.Equal(t, value, allMetadata[key])
	})

	t.Run("Context and Closure", func(t *testing.T) {
		ctx := channel.Context()
		assert.NotNil(t, ctx)

		select {
		case <-ctx.Done():
			t.Fatal("Context should not be done initially")
		default:
			// Expected
		}

		err := channel.Close()
		require.NoError(t, err)

		assert.False(t, channel.IsConnected())

		select {
		case <-ctx.Done():
			// Expected after close
		case <-time.After(100 * time.Millisecond):
			t.Fatal("Context should be done after close")
		}
	})
}

func TestSessionManager(t *testing.T) {
	client := setupTestRedis(t)
	defer client.Close()

	sessionManager := NewSessionManager(client)
	ctx := context.Background()

	t.Run("Create and Get Session", func(t *testing.T) {
		chargePointID := "CP001"
		remoteAddr := "192.168.1.100:8080"
		wsServerID := "ws-server-1"

		// Create session
		channel, err := sessionManager.CreateSession(ctx, chargePointID, remoteAddr, wsServerID)
		require.NoError(t, err)
		assert.NotNil(t, channel)
		assert.Equal(t, chargePointID, channel.ID())

		// Get session
		retrieved, exists := sessionManager.GetSession(chargePointID)
		assert.True(t, exists)
		assert.Equal(t, channel, retrieved)

		// Verify it's in active sessions
		activeSessions, err := sessionManager.GetActiveSessions(ctx)
		require.NoError(t, err)
		assert.Contains(t, activeSessions, chargePointID)
	})

	t.Run("Update Session Activity", func(t *testing.T) {
		chargePointID := "CP002"
		remoteAddr := "192.168.1.101:8080"
		wsServerID := "ws-server-1"

		// Create session
		channel, err := sessionManager.CreateSession(ctx, chargePointID, remoteAddr, wsServerID)
		require.NoError(t, err)

		initialTime := channel.LastActivity()
		time.Sleep(10 * time.Millisecond)

		// Update activity
		err = sessionManager.UpdateSessionActivity(ctx, chargePointID)
		require.NoError(t, err)

		newTime := channel.LastActivity()
		assert.True(t, newTime.After(initialTime))
	})

	t.Run("Remove Session", func(t *testing.T) {
		chargePointID := "CP003"
		remoteAddr := "192.168.1.102:8080"
		wsServerID := "ws-server-1"

		// Create session
		_, err := sessionManager.CreateSession(ctx, chargePointID, remoteAddr, wsServerID)
		require.NoError(t, err)

		// Remove session
		err = sessionManager.RemoveSession(ctx, chargePointID)
		require.NoError(t, err)

		// Verify removal
		_, exists := sessionManager.GetSession(chargePointID)
		assert.False(t, exists)

		// Verify it's not in active sessions
		activeSessions, err := sessionManager.GetActiveSessions(ctx)
		require.NoError(t, err)
		assert.NotContains(t, activeSessions, chargePointID)
	})

	t.Run("Get All Sessions", func(t *testing.T) {
		// Clean up first
		sessions := sessionManager.GetAllSessions()
		for id := range sessions {
			sessionManager.RemoveSession(ctx, id)
		}

		chargePointIDs := []string{"CP004", "CP005", "CP006"}

		// Create multiple sessions
		for _, id := range chargePointIDs {
			_, err := sessionManager.CreateSession(ctx, id, "192.168.1.1:8080", "ws-server-1")
			require.NoError(t, err)
		}

		// Get all sessions
		allSessions := sessionManager.GetAllSessions()
		assert.Len(t, allSessions, len(chargePointIDs))

		for _, id := range chargePointIDs {
			assert.Contains(t, allSessions, id)
		}
	})
}

func TestCorrelationManager(t *testing.T) {
	client := setupTestRedis(t)
	defer client.Close()

	correlationManager := NewCorrelationManager(client, 5*time.Second)
	defer correlationManager.Close()

	ctx := context.Background()

	t.Run("Track and Complete Request", func(t *testing.T) {
		messageID := "msg-001"
		chargePointID := "CP001"
		timeout := 5 * time.Second

		// Track request
		responseCh, errorCh, err := correlationManager.TrackRequest(ctx, messageID, chargePointID, timeout)
		require.NoError(t, err)

		// Verify pending request count
		assert.Equal(t, 1, correlationManager.GetPendingRequestCount())

		// Complete request
		response := []byte(`{"status": "success"}`)
		err = correlationManager.CompleteRequest(messageID, response)
		require.NoError(t, err)

		// Verify response received
		select {
		case receivedResponse := <-responseCh:
			assert.Equal(t, response, receivedResponse)
		case err := <-errorCh:
			t.Fatalf("Unexpected error: %v", err)
		case <-time.After(1 * time.Second):
			t.Fatal("Timeout waiting for response")
		}

		// Verify request removed
		assert.Equal(t, 0, correlationManager.GetPendingRequestCount())
	})

	t.Run("Track and Fail Request", func(t *testing.T) {
		messageID := "msg-002"
		chargePointID := "CP001"
		timeout := 5 * time.Second

		// Track request
		responseCh, errorCh, err := correlationManager.TrackRequest(ctx, messageID, chargePointID, timeout)
		require.NoError(t, err)

		// Fail request
		expectedError := assert.AnError
		err = correlationManager.FailRequest(messageID, expectedError)
		require.NoError(t, err)

		// Verify error received
		select {
		case <-responseCh:
			t.Fatal("Unexpected response received")
		case receivedError := <-errorCh:
			assert.Equal(t, expectedError, receivedError)
		case <-time.After(1 * time.Second):
			t.Fatal("Timeout waiting for error")
		}

		// Verify request removed
		assert.Equal(t, 0, correlationManager.GetPendingRequestCount())
	})

	t.Run("Request Timeout", func(t *testing.T) {
		messageID := "msg-003"
		chargePointID := "CP001"
		timeout := 100 * time.Millisecond

		// Track request with short timeout
		responseCh, errorCh, err := correlationManager.TrackRequest(ctx, messageID, chargePointID, timeout)
		require.NoError(t, err)

		// Wait for timeout
		select {
		case <-responseCh:
			t.Fatal("Unexpected response received")
		case err := <-errorCh:
			assert.Contains(t, err.Error(), "timeout")
		case <-time.After(1 * time.Second):
			t.Fatal("Timeout waiting for timeout error")
		}
	})

	t.Run("Get Pending Requests for Charge Point", func(t *testing.T) {
		chargePointID := "CP001"
		messageIDs := []string{"msg-004", "msg-005", "msg-006"}

		// Track multiple requests for the same charge point
		for _, messageID := range messageIDs {
			_, _, err := correlationManager.TrackRequest(ctx, messageID, chargePointID, 10*time.Second)
			require.NoError(t, err)
		}

		// Get pending requests for charge point
		pendingRequests := correlationManager.GetPendingRequestsForChargePoint(chargePointID)
		assert.Len(t, pendingRequests, len(messageIDs))

		// Clean up
		for _, messageID := range messageIDs {
			correlationManager.FailRequest(messageID, assert.AnError)
		}
	})
}

func TestRedisFactory(t *testing.T) {
	factory := NewRedisFactory()

	t.Run("Supported Types", func(t *testing.T) {
		supportedTypes := factory.SupportedTypes()
		assert.Contains(t, supportedTypes, transport.RedisTransport)
	})

	t.Run("Create Server Transport", func(t *testing.T) {
		config := getTestConfig()

		serverTransport, err := factory.CreateTransport(config)
		require.NoError(t, err)
		assert.NotNil(t, serverTransport)

		// Verify it implements the interface
		_, ok := serverTransport.(transport.Transport)
		assert.True(t, ok)
	})

	t.Run("Create Client Transport", func(t *testing.T) {
		config := getTestConfig()

		clientTransport, err := factory.CreateClientTransport(config)
		require.NoError(t, err)
		assert.NotNil(t, clientTransport)

		// Verify it implements the interface
		_, ok := clientTransport.(transport.ClientTransport)
		assert.True(t, ok)
	})

	t.Run("Invalid Config", func(t *testing.T) {
		invalidConfig := &transport.WebSocketConfig{}

		_, err := factory.CreateTransport(invalidConfig)
		assert.Equal(t, transport.ErrInvalidConfig, err)

		_, err = factory.CreateClientTransport(invalidConfig)
		assert.Equal(t, transport.ErrInvalidConfig, err)
	})
}

func TestRedisTransportIntegration(t *testing.T) {
	client := setupTestRedis(t)
	defer client.Close()

	config := getTestConfig()
	factory := NewRedisFactory()

	// Create server transport
	serverTransport, err := factory.CreateTransport(config)
	require.NoError(t, err)

	redisServer := serverTransport.(*RedisServerTransport)

	// Create client transport
	clientTransport, err := factory.CreateClientTransport(config)
	require.NoError(t, err)

	redisClient := clientTransport.(*RedisClientTransport)

	ctx := context.Background()

	t.Run("Basic Message Flow", func(t *testing.T) {
		var receivedMessage []byte
		var receivedClientID string
		messageReceived := make(chan bool, 1)

		// Set up server message handler
		redisServer.SetMessageHandler(func(clientID string, data []byte) error {
			receivedClientID = clientID
			receivedMessage = data
			select {
			case messageReceived <- true:
			default:
			}
			return nil
		})

		// Start server
		err := redisServer.Start(ctx, config)
		require.NoError(t, err)
		defer redisServer.Stop(ctx)

		// Wait for server to be ready
		time.Sleep(200 * time.Millisecond)

		// Start client
		chargePointID := "CP001"
		err = redisClient.Start(ctx, chargePointID, config)
		require.NoError(t, err)
		defer redisClient.Stop(ctx)

		// Verify client is connected
		assert.True(t, redisClient.IsConnected())
		t.Logf("Client connected: %v", redisClient.IsConnected())

		// Wait for connection event to be processed
		time.Sleep(200 * time.Millisecond)

		// Send message from client
		testMessage := []byte(`{"messageType": 2, "messageId": "123", "action": "Heartbeat", "payload": {}}`)
		t.Logf("Sending message: %s", string(testMessage))
		err = redisClient.Write(testMessage)
		require.NoError(t, err)
		t.Logf("Write completed successfully")

		// Debug: Check if message is in the queue
		queueLength := client.LLen(ctx, client.keys.Requests).Val()
		t.Logf("Queue length after write: %d", queueLength)

		// Wait for message to be processed
		select {
		case <-messageReceived:
			// Message received
		case <-time.After(2 * time.Second):
			// Debug: Check queue length again
			finalQueueLength := client.LLen(ctx, client.keys.Requests).Val()
			t.Logf("Final queue length: %d", finalQueueLength)
			t.Fatal("Timeout waiting for message to be received")
		}

		// Verify message received
		assert.Equal(t, chargePointID, receivedClientID)
		assert.NotNil(t, receivedMessage)

		// Verify client is connected on server
		assert.True(t, redisServer.IsClientConnected(chargePointID))
		connectedClients := redisServer.GetConnectedClients()
		assert.Contains(t, connectedClients, chargePointID)
	})
}