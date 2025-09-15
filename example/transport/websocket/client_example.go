package main

import (
	"context"
	"log"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/lorenzodonini/ocpp-go/ocpp"
	ocpp16 "github.com/lorenzodonini/ocpp-go/ocpp1.6"
	"github.com/lorenzodonini/ocpp-go/ocpp1.6/core"
	"github.com/lorenzodonini/ocpp-go/ocpp1.6/types"
	"github.com/lorenzodonini/ocpp-go/ocppj"
	"github.com/lorenzodonini/ocpp-go/transport"
	"github.com/lorenzodonini/ocpp-go/transport/websocket"
)

const (
	defaultServerURL = "ws://localhost:8887/ws"
	clientID         = "chargepoint001"
)

func main() {
	// Parse server URL from environment or use default
	serverURL := defaultServerURL
	if url := os.Getenv("SERVER_URL"); url != "" {
		serverURL = url
	}

	// Create WebSocket client transport
	factory := websocket.NewWebSocketFactory()
	config := &transport.WebSocketConfig{
		Port: 8887, // Not used for client connections
	}

	wsTransport, err := factory.CreateClientTransport(config)
	if err != nil {
		log.Fatalf("Failed to create WebSocket client transport: %v", err)
	}

	// Create OCPP client using transport interface
	client := ocppj.NewClientWithTransport(clientID, wsTransport, nil, nil, ocpp16.Profile)

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

	// Connect to server
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	log.Printf("Connecting to OCPP server at %s...", serverURL)
	if err := client.StartWithTransport(ctx, serverURL, config); err != nil {
		log.Fatalf("Failed to connect to server: %v", err)
	}

	log.Printf("Connected to server as client %s", clientID)

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