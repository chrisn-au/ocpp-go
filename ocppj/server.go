package ocppj

import (
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"net"
	"net/http"

	"github.com/gorilla/websocket"
	"gopkg.in/go-playground/validator.v9"

	"github.com/lorenzodonini/ocpp-go/ocpp"
	"github.com/lorenzodonini/ocpp-go/transport"
	"github.com/lorenzodonini/ocpp-go/ws"
)

// The endpoint waiting for incoming connections from OCPP clients, in an OCPP-J topology.
// During message exchange, the two roles may be reversed (depending on the message direction), but a server struct remains associated to a central system.
type Server struct {
	Endpoint
	server                    ws.Server            // Keep for backward compatibility
	transport                 transport.Transport  // New transport interface
	checkClientHandler        ws.CheckClientHandler
	newClientHandler          ClientHandler
	disconnectedClientHandler ClientHandler
	requestHandler            RequestHandler
	responseHandler           ResponseHandler
	errorHandler              ErrorHandler
	invalidMessageHook        InvalidMessageHook
	dispatcher                ServerDispatcher
	RequestState              ServerState
}

type ClientHandler func(client ws.Channel)
type RequestHandler func(client ws.Channel, request ocpp.Request, requestId string, action string)
type ResponseHandler func(client ws.Channel, response ocpp.Response, requestId string)
type ErrorHandler func(client ws.Channel, err *ocpp.Error, details interface{})
type InvalidMessageHook func(client ws.Channel, err *ocpp.Error, rawJson string, parsedFields []interface{}) *ocpp.Error

// Transport-compatible handler types
type TransportClientHandler func(clientID string)
type TransportRequestHandler func(clientID string, request ocpp.Request, requestId string, action string)
type TransportResponseHandler func(clientID string, response ocpp.Response, requestId string)
type TransportErrorHandler func(clientID string, err *ocpp.Error, details interface{})
type TransportInvalidMessageHook func(clientID string, err *ocpp.Error, rawJson string, parsedFields []interface{}) *ocpp.Error

// NewServerWithTransport creates a new Server endpoint with transport abstraction.
// This is the recommended constructor for new implementations as it supports
// multiple transport types (WebSocket, Redis, etc.) through the transport interface.
//
// You may create a simple new server by using these default values:
//
//	transport := websocket.NewWebSocketTransport()
//	s := ocppj.NewServerWithTransport(transport, nil, nil)
//
// The dispatcher's associated ClientState will be set during initialization.
func NewServerWithTransport(transport transport.Transport, dispatcher ServerDispatcher, stateHandler ServerState, profiles ...*ocpp.Profile) *Server {
	if dispatcher == nil {
		dispatcher = NewDefaultServerDispatcher(NewFIFOQueueMap(0))
	}
	if stateHandler == nil {
		d, ok := dispatcher.(*DefaultServerDispatcher)
		if !ok {
			stateHandler = NewServerState(nil)
		} else {
			stateHandler = d.pendingRequestState
		}
	}
	if transport == nil {
		panic("transport parameter cannot be nil")
	}

	// For transport-based servers, we create a mock websocket server for dispatcher compatibility
	// This maintains the existing dispatcher interface while using the new transport
	mockWsServer := &mockWebSocketServer{transport: transport}
	dispatcher.SetNetworkServer(mockWsServer)
	dispatcher.SetPendingRequestState(stateHandler)

	// Create server and add profiles
	s := Server{Endpoint: Endpoint{}, transport: transport, RequestState: stateHandler, dispatcher: dispatcher}
	for _, profile := range profiles {
		s.AddProfile(profile)
	}
	return &s
}

// Creates a new Server endpoint.
// Requires a a websocket server. Optionally a structure for queueing/dispatching requests,
// a custom state handler and a list of profiles may be passed.
//
// You may create a simple new server by using these default values:
//
//	s := ocppj.NewServer(ws.NewServer(), nil, nil)
//
// The dispatcher's associated ClientState will be set during initialization.
//
// Deprecated: Use NewServerWithTransport for new implementations. This constructor
// is maintained for backward compatibility but lacks the flexibility of the transport abstraction.
func NewServer(wsServer ws.Server, dispatcher ServerDispatcher, stateHandler ServerState, profiles ...*ocpp.Profile) *Server {
	if dispatcher == nil {
		dispatcher = NewDefaultServerDispatcher(NewFIFOQueueMap(0))
	}
	if stateHandler == nil {
		d, ok := dispatcher.(*DefaultServerDispatcher)
		if !ok {
			stateHandler = NewServerState(nil)
		} else {
			stateHandler = d.pendingRequestState
		}
	}
	if wsServer == nil {
		wsServer = ws.NewServer()
	}
	dispatcher.SetNetworkServer(wsServer)
	dispatcher.SetPendingRequestState(stateHandler)

	// Create server and add profiles
	s := Server{Endpoint: Endpoint{}, server: wsServer, RequestState: stateHandler, dispatcher: dispatcher}
	for _, profile := range profiles {
		s.AddProfile(profile)
	}
	return &s
}

// Registers a handler for incoming requests.
func (s *Server) SetRequestHandler(handler RequestHandler) {
	s.requestHandler = handler
}

// Registers a handler for incoming responses.
func (s *Server) SetResponseHandler(handler ResponseHandler) {
	s.responseHandler = handler
}

// Registers a handler for incoming error messages.
func (s *Server) SetErrorHandler(handler ErrorHandler) {
	s.errorHandler = handler
}

// SetInvalidMessageHook registers an optional hook for incoming messages that couldn't be parsed.
// This hook is called when a message is received but cannot be parsed to the target OCPP message struct.
//
// The application is notified synchronously of the error.
// The callback provides the raw JSON string, along with the parsed fields.
// The application MUST return as soon as possible, since the hook is called synchronously and awaits a return value.
//
// The hook does not allow responding to the message directly,
// but the return value will be used to send an OCPP error to the other endpoint.
//
// If no handler is registered (or no error is returned by the hook),
// the internal error message is sent to the client without further processing.
//
// Note: Failing to return from the hook will cause the handler for this client to block indefinitely.
func (s *Server) SetInvalidMessageHook(hook InvalidMessageHook) {
	s.invalidMessageHook = hook
}

// Registers a handler for canceled request messages.
func (s *Server) SetCanceledRequestHandler(handler CanceledRequestHandler) {
	s.dispatcher.SetOnRequestCanceled(handler)
}

// Registers a handler for incoming client connections.
func (s *Server) SetNewClientHandler(handler ClientHandler) {
	s.newClientHandler = handler
}

// Registers a handler for validate incoming client connections.
func (s *Server) SetNewClientValidationHandler(handler ws.CheckClientHandler) {
	s.checkClientHandler = handler
}

// Registers a handler for client disconnections.
func (s *Server) SetDisconnectedClientHandler(handler ClientHandler) {
	s.disconnectedClientHandler = handler
}

// Transport-compatible handler setters for new transport interface
// These are used when the server is created with NewServerWithTransport

// SetTransportRequestHandler registers a handler for incoming requests (transport-compatible).
// This handler receives the client ID as a string instead of a ws.Channel.
func (s *Server) SetTransportRequestHandler(handler TransportRequestHandler) {
	s.requestHandler = func(client ws.Channel, request ocpp.Request, requestId string, action string) {
		handler(client.ID(), request, requestId, action)
	}
}

// SetTransportResponseHandler registers a handler for incoming responses (transport-compatible).
func (s *Server) SetTransportResponseHandler(handler TransportResponseHandler) {
	s.responseHandler = func(client ws.Channel, response ocpp.Response, requestId string) {
		handler(client.ID(), response, requestId)
	}
}

// SetTransportErrorHandler registers a handler for incoming error messages (transport-compatible).
func (s *Server) SetTransportErrorHandler(handler TransportErrorHandler) {
	s.errorHandler = func(client ws.Channel, err *ocpp.Error, details interface{}) {
		handler(client.ID(), err, details)
	}
}

// SetTransportInvalidMessageHook registers an optional hook for invalid messages (transport-compatible).
func (s *Server) SetTransportInvalidMessageHook(hook TransportInvalidMessageHook) {
	s.invalidMessageHook = func(client ws.Channel, err *ocpp.Error, rawJson string, parsedFields []interface{}) *ocpp.Error {
		return hook(client.ID(), err, rawJson, parsedFields)
	}
}

// SetTransportNewClientHandler registers a handler for incoming client connections (transport-compatible).
func (s *Server) SetTransportNewClientHandler(handler TransportClientHandler) {
	s.newClientHandler = func(client ws.Channel) {
		handler(client.ID())
	}
}

// SetTransportDisconnectedClientHandler registers a handler for client disconnections (transport-compatible).
func (s *Server) SetTransportDisconnectedClientHandler(handler TransportClientHandler) {
	s.disconnectedClientHandler = func(client ws.Channel) {
		handler(client.ID())
	}
}

// StartWithTransport starts the server using the configured transport interface.
// This method should be used when the server was created with NewServerWithTransport.
// The config parameter should match the transport type (WebSocketConfig, RedisConfig, etc.).
//
// The function runs indefinitely, until the server is stopped.
// Invoke this function in a separate goroutine, to perform other operations on the main thread.
//
// An error may be returned, if the transport couldn't be started.
func (s *Server) StartWithTransport(ctx context.Context, config transport.Config) error {
	if s.transport == nil {
		return fmt.Errorf("server was not configured with transport interface, use Start() method instead")
	}

	// Set transport handlers
	s.transport.SetMessageHandler(s.transportMessageHandler)
	s.transport.SetConnectionHandler(s.onTransportClientConnected)
	s.transport.SetDisconnectionHandler(s.onTransportClientDisconnected)
	s.transport.SetErrorHandler(s.onTransportError)

	s.dispatcher.Start()

	// Start the transport
	return s.transport.Start(ctx, config)
}

// Starts the underlying Websocket server on a specified listenPort and listenPath.
//
// The function runs indefinitely, until the server is stopped.
// Invoke this function in a separate goroutine, to perform other operations on the main thread.
//
// An error may be returned, if the websocket server couldn't be started.
//
// Deprecated: Use StartWithTransport for new implementations when using transport abstraction.
// This method is maintained for backward compatibility with existing WebSocket-based code.
func (s *Server) Start(listenPort int, listenPath string) {
	if s.server == nil {
		panic("server was not configured with WebSocket server, use StartWithTransport() method instead")
	}

	// Set internal message handler
	s.server.SetCheckClientHandler(s.checkClientHandler)
	s.server.SetNewClientHandler(s.onClientConnected)
	s.server.SetDisconnectedClientHandler(s.onClientDisconnected)
	s.server.SetMessageHandler(s.ocppMessageHandler)
	s.dispatcher.Start()
	// Serve & run
	s.server.Start(listenPort, listenPath)
	// TODO: return error?
}

// StopWithTransport stops the server using the configured transport interface.
// This method should be used when the server was started with StartWithTransport.
func (s *Server) StopWithTransport(ctx context.Context) error {
	if s.transport == nil {
		return fmt.Errorf("server was not configured with transport interface")
	}

	s.dispatcher.Stop()
	return s.transport.Stop(ctx)
}

// Stops the server.
// This clears all pending requests and causes the Start function to return.
func (s *Server) Stop() {
	s.dispatcher.Stop()
	if s.server != nil {
		s.server.Stop()
	}
	if s.transport != nil {
		// Use a background context for backward compatibility
		_ = s.transport.Stop(context.Background())
	}
}

// Sends an OCPP Request to a client, identified by the clientID parameter.
//
// Returns an error in the following cases:
//
// - the server wasn't started
//
// - message validation fails (request is malformed)
//
// - the endpoint doesn't support the feature
//
// - the output queue is full
func (s *Server) SendRequest(clientID string, request ocpp.Request) error {
	if !s.dispatcher.IsRunning() {
		return fmt.Errorf("ocppj server is not started, couldn't send request")
	}
	call, err := s.CreateCall(request)
	if err != nil {
		return err
	}
	jsonMessage, err := call.MarshalJSON()
	if err != nil {
		return err
	}
	// Will not send right away. Queuing message and let it be processed by dedicated requestPump routine
	if err = s.dispatcher.SendRequest(clientID, RequestBundle{call, jsonMessage}); err != nil {
		log.Errorf("error dispatching request [%s, %s] to %s: %v", call.UniqueId, call.Action, clientID, err)
		return err
	}
	log.Debugf("enqueued CALL [%s, %s] for %s", call.UniqueId, call.Action, clientID)
	return nil
}

// Sends an OCPP Response to a client, identified by the clientID parameter.
// The requestID parameter is required and identifies the previously received request.
//
// Returns an error in the following cases:
//
// - message validation fails (response is malformed)
//
// - the endpoint doesn't support the feature
//
// - a network error occurred
func (s *Server) SendResponse(clientID string, requestId string, response ocpp.Response) error {
	callResult, err := s.CreateCallResult(response, requestId)
	if err != nil {
		return err
	}
	jsonMessage, err := callResult.MarshalJSON()
	if err != nil {
		return ocpp.NewError(GenericError, err.Error(), requestId)
	}

	// Use transport interface if available, otherwise fall back to WebSocket
	if s.transport != nil {
		err = s.transport.Write(clientID, jsonMessage)
	} else if s.server != nil {
		err = s.server.Write(clientID, jsonMessage)
	} else {
		return fmt.Errorf("no transport or server configured")
	}

	if err != nil {
		log.Errorf("error sending response [%s] to %s: %v", callResult.GetUniqueId(), clientID, err)
		return ocpp.NewError(GenericError, err.Error(), requestId)
	}
	log.Debugf("sent CALL RESULT [%s] for %s", callResult.GetUniqueId(), clientID)
	log.Debugf("sent JSON message to %s: %s", clientID, string(jsonMessage))
	return nil
}

// Sends an OCPP Error to a client, identified by the clientID parameter.
// The requestID parameter is required and identifies the previously received request.
//
// Returns an error in the following cases:
//
// - message validation fails (error is malformed)
//
// - a network error occurred
func (s *Server) SendError(clientID string, requestId string, errorCode ocpp.ErrorCode, description string, details interface{}) error {
	callError, err := s.CreateCallError(requestId, errorCode, description, details)
	if err != nil {
		return err
	}
	jsonMessage, err := callError.MarshalJSON()
	if err != nil {
		return ocpp.NewError(GenericError, err.Error(), requestId)
	}

	// Use transport interface if available, otherwise fall back to WebSocket
	if s.transport != nil {
		err = s.transport.Write(clientID, jsonMessage)
	} else if s.server != nil {
		err = s.server.Write(clientID, jsonMessage)
	} else {
		return fmt.Errorf("no transport or server configured")
	}

	if err != nil {
		log.Errorf("error sending response error [%s] to %s: %v", callError.UniqueId, clientID, err)
		return ocpp.NewError(GenericError, err.Error(), requestId)
	}
	log.Debugf("sent CALL ERROR [%s] for %s", callError.UniqueId, clientID)
	log.Debugf("sent JSON message to %s: %s", clientID, string(jsonMessage))
	return nil
}

func (s *Server) ocppMessageHandler(wsChannel ws.Channel, data []byte) error {
	parsedJson, err := ParseRawJsonMessage(data)
	if err != nil {
		log.Error(err)
		return err
	}
	log.Debugf("received JSON message from %s: %s", wsChannel.ID(), string(data))
	// Get pending requests for client
	pending := s.RequestState.GetClientState(wsChannel.ID())
	message, err := s.ParseMessage(parsedJson, pending)
	if err != nil {
		ocppErr := err.(*ocpp.Error)
		messageID := ocppErr.MessageId
		// Support ad-hoc callback for invalid message handling
		if s.invalidMessageHook != nil {
			err2 := s.invalidMessageHook(wsChannel, ocppErr, string(data), parsedJson)
			// If the hook returns an error, use it as output error. If not, use the original error.
			if err2 != nil {
				ocppErr = err2
				ocppErr.MessageId = messageID
			}
		}
		err = ocppErr
		// Send error to other endpoint if a message ID is available
		if ocppErr.MessageId != "" {
			err2 := s.SendError(wsChannel.ID(), ocppErr.MessageId, ocppErr.Code, ocppErr.Description, nil)
			if err2 != nil {
				return err2
			}
		}
		log.Error(err)
		return err
	}
	if message != nil {
		switch message.GetMessageTypeId() {
		case CALL:
			call := message.(*Call)
			log.Debugf("handling incoming CALL [%s, %s] from %s", call.UniqueId, call.Action, wsChannel.ID())
			if s.requestHandler != nil {
				s.requestHandler(wsChannel, call.Payload, call.UniqueId, call.Action)
			}
		case CALL_RESULT:
			callResult := message.(*CallResult)
			log.Debugf("handling incoming CALL RESULT [%s] from %s", callResult.UniqueId, wsChannel.ID())
			s.dispatcher.CompleteRequest(wsChannel.ID(), callResult.GetUniqueId())
			if s.responseHandler != nil {
				s.responseHandler(wsChannel, callResult.Payload, callResult.UniqueId)
			}
		case CALL_ERROR:
			callError := message.(*CallError)
			log.Debugf("handling incoming CALL RESULT [%s] from %s", callError.UniqueId, wsChannel.ID())
			s.dispatcher.CompleteRequest(wsChannel.ID(), callError.GetUniqueId())
			if s.errorHandler != nil {
				s.errorHandler(wsChannel, ocpp.NewError(callError.ErrorCode, callError.ErrorDescription, callError.UniqueId), callError.ErrorDetails)
			}
		}
	}
	return nil
}

// HandleFailedResponseError allows to handle failures while sending responses (either CALL_RESULT or CALL_ERROR).
// It internally analyzes and creates an ocpp.Error based on the given error.
// It will the attempt to send it to the client.
//
// The function helps to prevent starvation on the other endpoint, which is caused by a response never reaching it.
// The method will, however, only attempt to send a default error once.
// If this operation fails, the other endpoint may still starve.
func (s *Server) HandleFailedResponseError(clientID string, requestID string, err error, featureName string) {
	log.Debugf("handling error for failed response [%s]", requestID)
	var responseErr *ocpp.Error
	// There's several possible errors: invalid profile, invalid payload or send error
	switch err.(type) {
	case validator.ValidationErrors:
		// Validation error
		var validationErr validator.ValidationErrors
		errors.As(err, &validationErr)
		responseErr = errorFromValidation(s, validationErr, requestID, featureName)
	case *ocpp.Error:
		// Internal OCPP error
		errors.As(err, &responseErr)
	case error:
		// Unknown error
		responseErr = ocpp.NewError(GenericError, err.Error(), requestID)
	}
	// Send an OCPP error to the target, since no regular response could be sent
	_ = s.SendError(clientID, requestID, responseErr.Code, responseErr.Description, nil)
}

func (s *Server) onClientConnected(ws ws.Channel) {
	// Create state for connected client
	s.dispatcher.CreateClient(ws.ID())
	// Invoke callback
	if s.newClientHandler != nil {
		s.newClientHandler(ws)
	}
}

func (s *Server) onClientDisconnected(ws ws.Channel) {
	// Clear state for disconnected client
	s.dispatcher.DeleteClient(ws.ID())
	s.RequestState.ClearClientPendingRequest(ws.ID())
	// Invoke callback
	if s.disconnectedClientHandler != nil {
		s.disconnectedClientHandler(ws)
	}
}

// Transport-specific handlers for new transport interface

func (s *Server) transportMessageHandler(clientID string, data []byte) error {
	// Create a mock ws.Channel to maintain compatibility with existing handlers
	// In a full implementation, you might want to create a proper adapter
	mockChannel := &mockChannel{id: clientID}
	return s.ocppMessageHandler(mockChannel, data)
}

func (s *Server) onTransportClientConnected(clientID string) {
	// Create state for connected client
	s.dispatcher.CreateClient(clientID)
	// Invoke callback with mock channel
	if s.newClientHandler != nil {
		mockChannel := &mockChannel{id: clientID}
		s.newClientHandler(mockChannel)
	}
}

func (s *Server) onTransportClientDisconnected(clientID string, err error) {
	// Clear state for disconnected client
	s.dispatcher.DeleteClient(clientID)
	s.RequestState.ClearClientPendingRequest(clientID)
	// Invoke callback with mock channel
	if s.disconnectedClientHandler != nil {
		mockChannel := &mockChannel{id: clientID}
		s.disconnectedClientHandler(mockChannel)
	}
}

func (s *Server) onTransportError(clientID string, err error) {
	log.Errorf("transport error for client %s: %v", clientID, err)
}

// mockChannel is a minimal implementation of ws.Channel to maintain compatibility
// with existing WebSocket-based handlers when using transport interface
type mockChannel struct {
	id string
}

func (m *mockChannel) ID() string {
	return m.id
}

func (m *mockChannel) Write(data []byte) error {
	// This should not be called when using transport interface
	// as writes go through the transport interface directly
	return fmt.Errorf("write not supported on mock channel, use transport interface")
}

func (m *mockChannel) Close() error {
	// This should not be called when using transport interface
	return fmt.Errorf("close not supported on mock channel, use transport interface")
}

func (m *mockChannel) IsConnected() bool {
	// This is a best-effort implementation for compatibility
	return true
}

func (m *mockChannel) RemoteAddr() net.Addr {
	// Return a mock address for compatibility
	return &net.TCPAddr{IP: net.IPv4(127, 0, 0, 1), Port: 0}
}

func (m *mockChannel) TLSConnectionState() *tls.ConnectionState {
	// Return nil for mock implementation
	return nil
}

// mockWebSocketServer is a minimal implementation of ws.Server to maintain compatibility
// with existing dispatchers when using transport interface
type mockWebSocketServer struct {
	transport transport.Transport
}

func (m *mockWebSocketServer) Start(port int, listenPath string) {
	// This should not be called when using transport interface
	log.Errorf("mockWebSocketServer.Start called - this is not supported with transport interface")
}

func (m *mockWebSocketServer) Stop() {
	// This should not be called when using transport interface
	log.Errorf("mockWebSocketServer.Stop called - this is not supported with transport interface")
}

func (m *mockWebSocketServer) GetChannel(id string) (ws.Channel, bool) {
	// Return a mock channel for compatibility
	mockChannel := &mockChannel{id: id}
	return mockChannel, true
}

func (m *mockWebSocketServer) StopConnection(id string, closeError websocket.CloseError) error {
	// This should not be called when using transport interface
	return fmt.Errorf("StopConnection not supported on mock server")
}

func (m *mockWebSocketServer) Errors() <-chan error {
	// Return an empty error channel
	errorChan := make(chan error)
	close(errorChan)
	return errorChan
}

func (m *mockWebSocketServer) SetTimeoutConfig(config ws.ServerTimeoutConfig) {
	// This is not supported with transport interface
}

func (m *mockWebSocketServer) Write(clientID string, data []byte) error {
	// Delegate to the transport interface
	if m.transport != nil {
		return m.transport.Write(clientID, data)
	}
	return fmt.Errorf("no transport configured")
}

func (m *mockWebSocketServer) AddSupportedSubprotocol(subProto string) {
	// This is not supported with transport interface
}

func (m *mockWebSocketServer) SetChargePointIdResolver(resolver func(r *http.Request) (string, error)) {
	// This is not supported with transport interface
}

func (m *mockWebSocketServer) SetBasicAuthHandler(handler func(username string, password string) bool) {
	// This is not supported with transport interface
}

func (m *mockWebSocketServer) SetCheckOriginHandler(handler func(r *http.Request) bool) {
	// This is not supported with transport interface
}

func (m *mockWebSocketServer) SetMessageHandler(handler ws.MessageHandler) {
	// This is handled by the transport interface
}

func (m *mockWebSocketServer) SetNewClientHandler(handler ws.ConnectedHandler) {
	// This is handled by the transport interface
}

func (m *mockWebSocketServer) SetDisconnectedClientHandler(handler func(ws.Channel)) {
	// This is handled by the transport interface
}

func (m *mockWebSocketServer) SetCheckClientHandler(handler ws.CheckClientHandler) {
	// This is handled by the transport interface
}

func (m *mockWebSocketServer) Addr() *net.TCPAddr {
	// Return a mock address
	return &net.TCPAddr{IP: net.IPv4(127, 0, 0, 1), Port: 0}
}
