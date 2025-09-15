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
	"github.com/lorenzodonini/ocpp-go/ocpp1.6/types"
	"github.com/lorenzodonini/ocpp-go/ocppj"
	"github.com/lorenzodonini/ocpp-go/transport"
	"github.com/lorenzodonini/ocpp-go/transport/redis"
)

const (
	defaultRedisAddr = "localhost:6379"
	clientID         = "chargepoint001"
)

func main() {
	// Parse Redis address from environment or use default
	redisAddr := defaultRedisAddr
	if addr := os.Getenv("REDIS_ADDR"); addr != "" {
		redisAddr = addr
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
		log.Printf("Received response [%s]: %T", requestId, response)
	})

	client.SetErrorHandler(func(err *ocpp.Error, details interface{}) {
		log.Printf("Received error: %v, details: %v", err, details)
	})

	// Set up disconnect and reconnect handlers
	client.SetOnDisconnectedHandler(func(err error) {
		log.Printf("Disconnected from server: %v", err)
	})

	client.SetOnReconnectedHandler(func() {
		log.Printf("Reconnected to server")
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

	// Connect to server
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	log.Printf("Connecting to OCPP server via Redis at %s...", redisAddr)
	// For Redis transport, the endpoint is not used the same way as WebSocket
	if err := client.StartWithTransport(ctx, clientID, config); err != nil {
		log.Fatalf("Failed to connect to server: %v", err)
	}

	log.Printf("Connected to server as client %s via Redis", clientID)

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
				if client.IsConnected() {
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

		statusRequest := core.NewStatusNotificationRequest(1, types.ChargePointErrorCodeNoError, types.ChargePointStatusAvailable)
		if err := client.SendRequest(statusRequest); err != nil {
			log.Printf("Error sending status notification: %v", err)
		}
	}()

	// Simulate meter value readings
	go func() {
		ticker := time.NewTicker(60 * time.Second)
		defer ticker.Stop()

		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				if client.IsConnected() {
					// This would require implementing meter values request
					log.Println("Would send MeterValues (not implemented in this example)")
				}
			}
		}
	}()

	// Wait for interrupt signal
	c := make(chan os.Signal, 1)
	signal.Notify(c, os.Interrupt, syscall.SIGTERM)
	<-c

	log.Println("Shutting down client...")

	// Stop client gracefully
	shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer shutdownCancel()

	if err := client.StopWithTransport(shutdownCtx); err != nil {
		log.Printf("Error stopping client: %v", err)
	}

	log.Println("Client stopped")
}

// Helper function to create string pointers
func stringPtr(s string) *string {
	return &s
}