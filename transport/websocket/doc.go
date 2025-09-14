// Package websocket provides adapter implementations that make the existing ws package
// conform to the transport interfaces without modifying the original ws package.
//
// This package acts as a bridge between the new transport abstraction layer and the
// existing WebSocket implementation in the ws package. It wraps ws.Server, ws.Client,
// and ws.Channel components to implement the transport.Transport, transport.ClientTransport,
// and related interfaces.
//
// # Zero-Modification Guarantee
//
// This adapter package guarantees that NO modifications are made to the existing ws package.
// All functionality is achieved through composition and wrapping, ensuring backward
// compatibility and maintaining the integrity of the original WebSocket implementation.
//
// # Key Components
//
// The package provides the following main adapters:
//
//   - ServerAdapter: Wraps ws.Server to implement transport.Transport
//   - ClientAdapter: Wraps ws.Client to implement transport.ClientTransport
//   - ChannelAdapter: Wraps ws.Channel to provide context support and transport.Channel interface
//   - WebSocketFactory: Implements transport.Factory to create WebSocket transport instances
//
// # Usage Examples
//
// ## Server Usage
//
//	// Create a WebSocket factory
//	factory := websocket.NewWebSocketFactory()
//
//	// Create server transport
//	config := &transport.WebSocketConfig{
//		Port:       8080,
//		ListenPath: "/ws/{id}",
//	}
//
//	serverTransport, err := factory.CreateTransport(config)
//	if err != nil {
//		log.Fatal(err)
//	}
//
//	// Set handlers
//	serverTransport.SetMessageHandler(func(clientID string, data []byte) error {
//		fmt.Printf("Received from %s: %s\n", clientID, string(data))
//		return nil
//	})
//
//	// Start the server
//	ctx := context.Background()
//	err = serverTransport.Start(ctx, config)
//	if err != nil {
//		log.Fatal(err)
//	}
//
// ## Client Usage
//
//	// Create client transport
//	clientTransport, err := factory.CreateClientTransport(config)
//	if err != nil {
//		log.Fatal(err)
//	}
//
//	// Set message handler
//	clientTransport.SetMessageHandler(func(data []byte) error {
//		fmt.Printf("Received: %s\n", string(data))
//		return nil
//	})
//
//	// Connect to server
//	err = clientTransport.Start(ctx, "ws://localhost:8080/ws/client1", config)
//	if err != nil {
//		log.Fatal(err)
//	}
//
//	// Send message
//	err = clientTransport.Write([]byte("Hello, server!"))
//	if err != nil {
//		log.Printf("Error sending message: %v", err)
//	}
//
// ## Using with Default Factory
//
//	// Register with the default factory for seamless integration
//	defaultFactory := transport.NewDefaultFactory()
//	websocket.RegisterWithDefaultFactory(defaultFactory)
//
//	// Now you can create WebSocket transports through the default factory
//	manager := transport.NewDefaultManager(defaultFactory)
//	serverTransport, err := manager.StartTransport(ctx, config)
//
// ## Working with Existing ws Components
//
//	// Wrap existing ws.Server
//	wsServer := ws.NewServer()
//	serverAdapter := websocket.CreateServerFromExisting(wsServer)
//
//	// Wrap existing ws.Client
//	wsClient := ws.NewClient()
//	clientAdapter := websocket.CreateClientFromExisting(wsClient)
//
// # Design Principles
//
// 1. **Composition over Inheritance**: The adapters wrap rather than extend the ws components
// 2. **Zero Modification**: No changes to the ws package are required or made
// 3. **Full Compatibility**: All existing ws functionality remains available
// 4. **Context Support**: Adds modern context support where the original ws package lacks it
// 5. **Error Handling**: Provides consistent error handling aligned with transport interfaces
// 6. **Thread Safety**: Maintains thread safety through proper synchronization
//
// # Limitations
//
// While this adapter maintains full compatibility with the ws package, some advanced
// configuration options may require direct access to the underlying ws components.
// In such cases, use the GetWrappedServer(), GetWrappedClient(), or GetWrappedChannel()
// methods to access the original ws instances.
//
// # Integration with OCPP
//
// This adapter seamlessly integrates with the OCPP protocol implementations,
// allowing existing OCPP code that uses the ws package to work unchanged while
// providing the benefits of the new transport abstraction layer.
package websocket