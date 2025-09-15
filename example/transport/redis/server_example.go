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
	"github.com/lorenzodonini/ocpp-go/transport/redis"
)

const (
	defaultRedisAddr = "localhost:6379"
)

func main() {
	// Parse Redis address from environment or use default
	redisAddr := defaultRedisAddr
	if addr := os.Getenv("REDIS_ADDR"); addr != "" {
		redisAddr = addr
	}

	// Parse Redis password from environment
	redisPassword := os.Getenv("REDIS_PASSWORD")

	// Create Redis transport
	factory := redis.NewRedisFactory()
	config := &transport.RedisConfig{
		Addr:          redisAddr,
		Password:      redisPassword,
		DB:            0,
		ChannelPrefix: "ocpp",
	}

	redisTransport, err := factory.CreateTransport(config)
	if err != nil {
		log.Fatalf("Failed to create Redis transport: %v", err)
	}

	// Create OCPP server using transport interface
	server := ocppj.NewServerWithTransport(redisTransport, nil, nil, ocpp16.Profile)

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

	log.Printf("Starting Redis OCPP server with Redis at %s...", redisAddr)
	go func() {
		if err := server.StartWithTransport(ctx, config); err != nil {
			log.Fatalf("Server failed to start: %v", err)
		}
	}()

	log.Println("Server started and listening for Redis messages")

	// Send periodic remote start transactions to demonstrate server->client communication
	go func() {
		ticker := time.NewTicker(60 * time.Second)
		defer ticker.Stop()

		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				// Get list of connected clients
				clients := redisTransport.GetConnectedClients()
				if len(clients) > 0 {
					clientID := clients[0] // Use first client
					log.Printf("Sending remote start transaction to client %s", clientID)

					// This would require implementing the remote start request
					// For now, just log that we would send it
					log.Printf("Would send RemoteStartTransaction to client %s", clientID)
				}
			}
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