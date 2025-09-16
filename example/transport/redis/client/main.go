package main

import (
	"context"
	"log"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/lorenzodonini/ocpp-go/ocpp"
	"github.com/lorenzodonini/ocpp-go/ocpp1.6/core"
	"github.com/lorenzodonini/ocpp-go/ocppj"
	"github.com/lorenzodonini/ocpp-go/transport"
	"github.com/lorenzodonini/ocpp-go/transport/redis"
)

const (
	defaultRedisAddr = "localhost:6379"
	defaultClientID  = "chargepoint001"
)

func main() {
	// Parse Redis address from environment or use default
	redisAddr := defaultRedisAddr
	if addr := os.Getenv("REDIS_ADDR"); addr != "" {
		redisAddr = addr
	}

	// Parse client ID from environment or use default
	clientID := defaultClientID
	if id := os.Getenv("CLIENT_ID"); id != "" {
		clientID = id
	}

	// Parse Redis password from environment
	redisPassword := os.Getenv("REDIS_PASSWORD")

	// Create Redis client transport
	factory := redis.NewRedisFactory()
	config := &transport.RedisConfig{
		Addr:          redisAddr,
		Password:      redisPassword,
		DB:            0,
		ChannelPrefix: "ocpp",
	}

	redisTransport, err := factory.CreateClientTransport(config)
	if err != nil {
		log.Fatalf("Failed to create Redis client transport: %v", err)
	}

	// Create OCPP client using transport interface
	client := ocppj.NewClientWithTransport(clientID, redisTransport, nil, nil, core.Profile)

	// Set up response handlers
	client.SetResponseHandler(func(response ocpp.Response, requestId string) {
		log.Printf("🔥 RESPONSE HANDLER CALLED [%s]: %T", requestId, response)
		switch resp := response.(type) {
		case *core.BootNotificationConfirmation:
			log.Printf("✅ Received BootNotificationConfirmation [%s]: Status=%s, Interval=%d", requestId, resp.Status, resp.Interval)
		case *core.HeartbeatConfirmation:
			log.Printf("💓 Received HeartbeatConfirmation [%s]: CurrentTime=%s", requestId, resp.CurrentTime.String())
		case *core.StatusNotificationConfirmation:
			log.Printf("📊 Received StatusNotificationConfirmation [%s]", requestId)
		default:
			log.Printf("❓ Received unknown response [%s]: %T", requestId, response)
		}
	})

	client.SetErrorHandler(func(err *ocpp.Error, details interface{}) {
		log.Printf("Received error: %v, details: %v", err, details)
	})

	// Set up Redis transport handlers (Redis handles connection resilience internally)
	client.SetOnDisconnectedHandler(func(err error) {
		log.Printf("Redis transport error: %v", err)
	})

	client.SetOnReconnectedHandler(func() {
		log.Printf("Redis transport recovered")
	})

	// Set up request handler for incoming server requests
	client.SetRequestHandler(func(request ocpp.Request, requestId string, action string) {
		log.Printf("Received request [%s]: %s", requestId, action)

		switch req := request.(type) {
		case *core.GetConfigurationRequest:
			// Respond to get configuration request
			configKeys := []core.ConfigurationKey{
				{Key: "HeartbeatInterval", Readonly: false, Value: stringPtr("300")},
				{Key: "MeterValueSampleInterval", Readonly: false, Value: stringPtr("60")},
			}
			response := core.NewGetConfigurationConfirmation(configKeys)
			if err := client.SendResponse(requestId, response); err != nil {
				log.Printf("Error sending response: %v", err)
			}
		default:
			log.Printf("Unsupported request type: %T", req)
			if err := client.SendError(requestId, "NotSupported", "Request not supported", nil); err != nil {
				log.Printf("Error sending error response: %v", err)
			}
		}
	})

	// Start Redis transport client
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	log.Printf("Starting OCPP client via Redis transport at %s...", redisAddr)
	// For Redis transport, endpoint is empty (client ID is used directly)
	if err := client.StartWithTransport(ctx, "", config); err != nil {
		log.Fatalf("Failed to start Redis transport: %v", err)
	}

	log.Printf("Started Redis transport client %s, listening for messages", clientID)

	// Send boot notification
	bootRequest := core.NewBootNotificationRequest("ExampleVendor", "ExampleModel")
	if err := client.SendRequest(bootRequest); err != nil {
		log.Printf("Error sending boot notification: %v", err)
	}

	// Start heartbeat loop
	go func() {
		ticker := time.NewTicker(30 * time.Second)
		defer ticker.Stop()

		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				if client.IsConnected() { // Redis transport active
					heartbeatRequest := core.NewHeartbeatRequest()
					if err := client.SendRequest(heartbeatRequest); err != nil {
						log.Printf("Error sending heartbeat: %v", err)
					}
				}
			}
		}
	}()

	// Send status notification
	go func() {
		time.Sleep(2 * time.Second) // Wait for boot notification response

		statusRequest := core.NewStatusNotificationRequest(1, core.NoError, core.ChargePointStatusAvailable)
		if err := client.SendRequest(statusRequest); err != nil {
			log.Printf("Error sending status notification: %v", err)
		}
	}()

	// Send periodic heartbeats every 30 seconds
	go func() {
		ticker := time.NewTicker(30 * time.Second)
		defer ticker.Stop()

		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				if client.IsConnected() { // Redis transport active
					log.Println("Sending periodic heartbeat via Redis...")
					heartbeatRequest := core.NewHeartbeatRequest()
					if err := client.SendRequest(heartbeatRequest); err != nil {
						log.Printf("Error sending heartbeat: %v", err)
					}
				}
			}
		}
	}()

	// Wait for interrupt signal
	c := make(chan os.Signal, 1)
	signal.Notify(c, os.Interrupt, syscall.SIGTERM)
	<-c

	log.Println("Shutting down Redis transport client...")

	// Stop Redis transport client gracefully
	shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer shutdownCancel()

	if err := client.StopWithTransport(shutdownCtx); err != nil {
		log.Printf("Error stopping Redis transport client: %v", err)
	}

	log.Println("Redis transport client stopped")
}

// Helper function to create string pointers
func stringPtr(s string) *string {
	return &s
}