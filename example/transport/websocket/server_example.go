package main

import (
	"context"
	"log"
	"os"
	"os/signal"
	"strconv"
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
	defaultPort = 8887
	defaultPath = "/ws/{id}"
)

func main() {
	// Parse port from environment or use default
	port := defaultPort
	if p := os.Getenv("PORT"); p != "" {
		if parsed, err := strconv.Atoi(p); err == nil {
			port = parsed
		}
	}

	// Create WebSocket transport
	factory := websocket.NewWebSocketFactory()
	config := &transport.WebSocketConfig{
		Port:       port,
		ListenPath: defaultPath,
	}

	wsTransport, err := factory.CreateTransport(config)
	if err != nil {
		log.Fatalf("Failed to create WebSocket transport: %v", err)
	}

	// Create OCPP server using transport interface
	server := ocppj.NewServerWithTransport(wsTransport, nil, nil, core.Profile)

	// Set up handlers using transport-compatible setters
	server.SetTransportRequestHandler(func(clientID string, request ocpp.Request, requestId string, action string) {
		log.Printf("Received request [%s] from client %s: %s", requestId, clientID, action)

		switch req := request.(type) {
		case *core.BootNotificationRequest:
			// Respond to boot notification
			response := core.NewBootNotificationResponse(types.RegistrationStatusAccepted, time.Now(), 300)
			if err := server.SendResponse(clientID, requestId, response); err != nil {
				log.Printf("Error sending response: %v", err)
			}
		case *core.HeartbeatRequest:
			// Respond to heartbeat
			response := core.NewHeartbeatResponse(time.Now())
			if err := server.SendResponse(clientID, requestId, response); err != nil {
				log.Printf("Error sending response: %v", err)
			}
		case *core.StatusNotificationRequest:
			// Respond to status notification
			response := core.NewStatusNotificationResponse()
			if err := server.SendResponse(clientID, requestId, response); err != nil {
				log.Printf("Error sending response: %v", err)
			}
		default:
			log.Printf("Unsupported request type: %T", req)
			if err := server.SendError(clientID, requestId, ocpp.NotSupported, "Request not supported", nil); err != nil {
				log.Printf("Error sending error response: %v", err)
			}
		}
	})

	server.SetTransportNewClientHandler(func(clientID string) {
		log.Printf("New client connected: %s", clientID)
	})

	server.SetTransportDisconnectedClientHandler(func(clientID string) {
		log.Printf("Client disconnected: %s", clientID)
	})

	// Start server with context
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	log.Printf("Starting WebSocket OCPP server on port %d...", port)
	go func() {
		if err := server.StartWithTransport(ctx, config); err != nil {
			log.Fatalf("Server failed to start: %v", err)
		}
	}()

	// Wait for interrupt signal
	c := make(chan os.Signal, 1)
	signal.Notify(c, os.Interrupt, syscall.SIGTERM)
	<-c

	log.Println("Shutting down server...")

	// Stop server gracefully
	shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer shutdownCancel()

	if err := server.StopWithTransport(shutdownCtx); err != nil {
		log.Printf("Error stopping server: %v", err)
	}

	log.Println("Server stopped")
}