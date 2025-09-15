package ocppj

import (
	"context"
	"errors"
	"fmt"

	"gopkg.in/go-playground/validator.v9"

	"github.com/lorenzodonini/ocpp-go/ocpp"
	"github.com/lorenzodonini/ocpp-go/transport"
	"github.com/lorenzodonini/ocpp-go/ws"
)

// The endpoint initiating the connection to an OCPP server, in an OCPP-J topology.
// During message exchange, the two roles may be reversed (depending on the message direction), but a client struct remains associated to a charge point/charging station.
type Client struct {
	Endpoint
	client                ws.Client                      // Keep for backward compatibility
	transport             transport.ClientTransport     // New transport interface
	Id                    string
	requestHandler        func(request ocpp.Request, requestId string, action string)
	responseHandler       func(response ocpp.Response, requestId string)
	errorHandler          func(err *ocpp.Error, details interface{})
	onDisconnectedHandler func(err error)
	onReconnectedHandler  func()
	invalidMessageHook    func(err *ocpp.Error, rawMessage string, parsedFields []interface{}) *ocpp.Error
	dispatcher            ClientDispatcher
	RequestState          ClientState
}

// NewClientWithTransport creates a new Client endpoint with transport abstraction.
// This is the recommended constructor for new implementations as it supports
// multiple transport types (WebSocket, Redis, etc.) through the transport interface.
//
// You may create a simple new client by using these default values:
//
//	transport := websocket.NewWebSocketClientTransport()
//	c := ocppj.NewClientWithTransport("client-id", transport, nil, nil)
//
// The transport parameter cannot be nil.
func NewClientWithTransport(id string, transport transport.ClientTransport, dispatcher ClientDispatcher, stateHandler ClientState, profiles ...*ocpp.Profile) *Client {
	endpoint := Endpoint{}
	if transport == nil {
		panic("transport parameter cannot be nil")
	}
	for _, profile := range profiles {
		endpoint.AddProfile(profile)
	}
	if dispatcher == nil {
		dispatcher = NewDefaultClientDispatcher(NewFIFOClientQueue(10))
	}
	if stateHandler == nil {
		stateHandler = NewClientState()
	}

	// For transport-based clients, we create a mock websocket client for dispatcher compatibility
	mockWsClient := &mockWebSocketClient{transport: transport}
	dispatcher.SetNetworkClient(mockWsClient)
	dispatcher.SetPendingRequestState(stateHandler)
	return &Client{Endpoint: endpoint, transport: transport, Id: id, dispatcher: dispatcher, RequestState: stateHandler}
}

// Creates a new Client endpoint.
// Requires a unique client ID, a websocket client, a struct for queueing/dispatching requests,
// a state handler and a list of supported profiles (optional).
//
// You may create a simple new server by using these default values:
//
//	s := ocppj.NewClient(ws.NewClient(), nil, nil)
//
// The wsClient parameter cannot be nil. Refer to the ws package for information on how to create and
// customize a websocket client.
//
// Deprecated: Use NewClientWithTransport for new implementations. This constructor
// is maintained for backward compatibility but lacks the flexibility of the transport abstraction.
func NewClient(id string, wsClient ws.Client, dispatcher ClientDispatcher, stateHandler ClientState, profiles ...*ocpp.Profile) *Client {
	endpoint := Endpoint{}
	if wsClient == nil {
		panic("wsClient parameter cannot be nil")
	}
	for _, profile := range profiles {
		endpoint.AddProfile(profile)
	}
	if dispatcher == nil {
		dispatcher = NewDefaultClientDispatcher(NewFIFOClientQueue(10))
	}
	if stateHandler == nil {
		stateHandler = NewClientState()
	}
	dispatcher.SetNetworkClient(wsClient)
	dispatcher.SetPendingRequestState(stateHandler)
	return &Client{Endpoint: endpoint, client: wsClient, Id: id, dispatcher: dispatcher, RequestState: stateHandler}
}

// Return incoming requests handler.
func (c *Client) GetRequestHandler() func(request ocpp.Request, requestId string, action string) {
	return c.requestHandler
}

// Registers a handler for incoming requests.
func (c *Client) SetRequestHandler(handler func(request ocpp.Request, requestId string, action string)) {
	c.requestHandler = handler
}

// Return incoming responses handler.
func (c *Client) GetResponseHandler() func(response ocpp.Response, requestId string) {
	return c.responseHandler
}

// Registers a handler for incoming responses.
func (c *Client) SetResponseHandler(handler func(response ocpp.Response, requestId string)) {
	c.responseHandler = handler
}

// Return incoming error messages handler.
func (c *Client) GetErrorHandler() func(err *ocpp.Error, details interface{}) {
	return c.errorHandler
}

// Registers a handler for incoming error messages.
func (c *Client) SetErrorHandler(handler func(err *ocpp.Error, details interface{})) {
	c.errorHandler = handler
}

// SetInvalidMessageHook registers an optional hook for incoming messages that couldn't be parsed.
// This hook is called when a message is received but cannot be parsed to the target OCPP message struct.
//
// The application is notified synchronously of the error.
// The callback provides the raw JSON string, along with the parsed fields.
// The application MUST return as soon as possible, since the hook is called synchronously and awaits a return value.
//
// While the hook does not allow responding to the message directly,
// the return value will be used to send an OCPP error to the other endpoint.
//
// If no handler is registered (or no error is returned by the hook),
// the internal error message is sent to the client without further processing.
//
// Note: Failing to return from the hook will cause the client to block indefinitely.
func (c *Client) SetInvalidMessageHook(hook func(err *ocpp.Error, rawMessage string, parsedFields []interface{}) *ocpp.Error) {
	c.invalidMessageHook = hook
}

func (c *Client) SetOnDisconnectedHandler(handler func(err error)) {
	c.onDisconnectedHandler = handler
}

func (c *Client) SetOnReconnectedHandler(handler func()) {
	c.onReconnectedHandler = handler
}

// Registers the handler to be called on timeout.
func (c *Client) SetOnRequestCanceled(handler func(requestId string, request ocpp.Request, err *ocpp.Error)) {
	c.dispatcher.SetOnRequestCanceled(handler)
}

// StartWithTransport connects to the server using the configured transport interface.
// This method should be used when the client was created with NewClientWithTransport.
// The endpoint format depends on the transport implementation.
// The config parameter should match the transport type (WebSocketConfig, RedisConfig, etc.).
//
// If the connection is established successfully, the function returns control to the caller immediately.
// The read/write routines are run on dedicated goroutines, so the main thread can perform other operations.
//
// An error may be returned, if establishing the connection failed.
func (c *Client) StartWithTransport(ctx context.Context, endpoint string, config transport.Config) error {
	if c.transport == nil {
		return fmt.Errorf("client was not configured with transport interface, use Start() method instead")
	}

	// Set transport handlers
	c.transport.SetMessageHandler(c.transportMessageHandler)
	c.transport.SetDisconnectionHandler(c.onTransportDisconnected)
	c.transport.SetReconnectionHandler(c.onTransportReconnected)
	c.transport.SetErrorHandler(c.onTransportError)

	// Connect & run
	fullUrl := fmt.Sprintf("%v/%v", endpoint, c.Id)
	err := c.transport.Start(ctx, fullUrl, config)
	if err == nil {
		c.dispatcher.Start()
	}
	return err
}

// Connects to the given serverURL and starts running the I/O loop for the underlying connection.
//
// If the connection is established successfully, the function returns control to the caller immediately.
// The read/write routines are run on dedicated goroutines, so the main thread can perform other operations.
//
// In case of disconnection, the client handles re-connection automatically.
// The client will attempt to re-connect to the server forever, until it is stopped by invoking the Stop method.
//
// An error may be returned, if establishing the connection failed.
//
// Deprecated: Use StartWithTransport for new implementations when using transport abstraction.
// This method is maintained for backward compatibility with existing WebSocket-based code.
func (c *Client) Start(serverURL string) error {
	if c.client == nil {
		return fmt.Errorf("client was not configured with WebSocket client, use StartWithTransport() method instead")
	}

	// Set internal message handler
	c.client.SetMessageHandler(c.ocppMessageHandler)
	c.client.SetDisconnectedHandler(c.onDisconnected)
	c.client.SetReconnectedHandler(c.onReconnected)
	// Connect & run
	fullUrl := fmt.Sprintf("%v/%v", serverURL, c.Id)
	err := c.client.Start(fullUrl)
	if err == nil {
		c.dispatcher.Start()
	}
	return err
}

// Deprecated: Use StartWithTransport for new implementations when using transport abstraction.
// This method is maintained for backward compatibility with existing WebSocket-based code.
func (c *Client) StartWithRetries(serverURL string) {
	if c.client == nil {
		log.Errorf("client was not configured with WebSocket client, use StartWithTransport() method instead")
		return
	}

	// Set internal message handler
	c.client.SetMessageHandler(c.ocppMessageHandler)
	c.client.SetDisconnectedHandler(c.onDisconnected)
	c.client.SetReconnectedHandler(c.onReconnected)
	// Connect & run
	fullUrl := fmt.Sprintf("%v/%v", serverURL, c.Id)
	c.client.StartWithRetries(fullUrl)
	c.dispatcher.Start()
}

// StopWithTransport stops the client using the configured transport interface.
// This method should be used when the client was started with StartWithTransport.
func (c *Client) StopWithTransport(ctx context.Context) error {
	if c.transport == nil {
		return fmt.Errorf("client was not configured with transport interface")
	}

	if c.dispatcher.IsRunning() {
		c.dispatcher.Stop()
	}
	return c.transport.Stop(ctx)
}

// Stops the client.
// The underlying I/O loop is stopped and all pending requests are cleared.
func (c *Client) Stop() {
	if c.transport != nil {
		// Use transport interface if available
		if c.dispatcher.IsRunning() {
			c.dispatcher.Stop()
		}
		_ = c.transport.Stop(context.Background())
		return
	}

	if c.client != nil {
		// Use WebSocket client for backward compatibility
		// Overwrite handler to intercept disconnected signal
		cleanupC := make(chan struct{}, 1)
		if c.IsConnected() {
			c.client.SetDisconnectedHandler(func(err error) {
				cleanupC <- struct{}{}
			})
		} else {
			close(cleanupC)
		}
		c.client.Stop()
		if c.dispatcher.IsRunning() {
			c.dispatcher.Stop()
		}
		// Wait for websocket to be cleaned up
		<-cleanupC
	}
}

func (c *Client) IsConnected() bool {
	if c.transport != nil {
		return c.transport.IsConnected()
	}
	if c.client != nil {
		return c.client.IsConnected()
	}
	return false
}

// Sends an OCPP Request to the server.
// The protocol is based on request-response and cannot send multiple messages concurrently.
// To guarantee this, outgoing messages are added to a queue and processed sequentially.
//
// Returns an error in the following cases:
//
// - the client wasn't started
//
// - message validation fails (request is malformed)
//
// - the endpoint doesn't support the feature
//
// - the output queue is full
func (c *Client) SendRequest(request ocpp.Request) error {
	if !c.dispatcher.IsRunning() {
		return fmt.Errorf("ocppj client is not started, couldn't send request")
	}
	call, err := c.CreateCall(request)
	if err != nil {
		return err
	}
	jsonMessage, err := call.MarshalJSON()
	if err != nil {
		return err
	}
	// Message will be processed by dispatcher. A dedicated mechanism allows to delegate the message queue handling.
	if err = c.dispatcher.SendRequest(RequestBundle{Call: call, Data: jsonMessage}); err != nil {
		log.Errorf("error dispatching request [%s, %s]: %v", call.UniqueId, call.Action, err)
		return err
	}
	log.Debugf("enqueued CALL [%s, %s]", call.UniqueId, call.Action)
	return nil
}

// Sends an OCPP Response to the server.
// The requestID parameter is required and identifies the previously received request.
//
// Returns an error in the following cases:
//
// - message validation fails (response is malformed)
//
// - the endpoint doesn't support the feature
//
// - a network error occurred
func (c *Client) SendResponse(requestId string, response ocpp.Response) error {
	callResult, err := c.CreateCallResult(response, requestId)
	if err != nil {
		return err
	}
	jsonMessage, err := callResult.MarshalJSON()
	if err != nil {
		return ocpp.NewError(GenericError, err.Error(), requestId)
	}

	// Use transport interface if available, otherwise fall back to WebSocket
	if c.transport != nil {
		err = c.transport.Write(jsonMessage)
	} else if c.client != nil {
		err = c.client.Write(jsonMessage)
	} else {
		return fmt.Errorf("no transport or client configured")
	}

	if err != nil {
		log.Errorf("error sending response [%s]: %v", callResult.GetUniqueId(), err)
		return ocpp.NewError(GenericError, err.Error(), requestId)
	}
	log.Debugf("sent CALL RESULT [%s]", callResult.GetUniqueId())
	log.Debugf("sent JSON message to server: %s", string(jsonMessage))
	return nil
}

// Sends an OCPP Error to the server.
// The requestID parameter is required and identifies the previously received request.
//
// Returns an error in the following cases:
//
// - message validation fails (error is malformed)
//
// - a network error occurred
func (c *Client) SendError(requestId string, errorCode ocpp.ErrorCode, description string, details interface{}) error {
	callError, err := c.CreateCallError(requestId, errorCode, description, details)
	if err != nil {
		return err
	}
	jsonMessage, err := callError.MarshalJSON()
	if err != nil {
		return ocpp.NewError(GenericError, err.Error(), requestId)
	}

	// Use transport interface if available, otherwise fall back to WebSocket
	if c.transport != nil {
		err = c.transport.Write(jsonMessage)
	} else if c.client != nil {
		err = c.client.Write(jsonMessage)
	} else {
		return fmt.Errorf("no transport or client configured")
	}

	if err != nil {
		log.Errorf("error sending response error [%s]: %v", callError.UniqueId, err)
		return ocpp.NewError(GenericError, err.Error(), requestId)
	}
	log.Debugf("sent CALL ERROR [%s]", callError.UniqueId)
	log.Debugf("sent JSON message to server: %s", string(jsonMessage))
	return nil
}

func (c *Client) ocppMessageHandler(data []byte) error {
	parsedJson, err := ParseRawJsonMessage(data)
	if err != nil {
		log.Error(err)
		return err
	}
	log.Debugf("received JSON message from server: %s", string(data))
	message, err := c.ParseMessage(parsedJson, c.RequestState)
	if err != nil {
		ocppErr := err.(*ocpp.Error)
		messageID := ocppErr.MessageId
		// Support ad-hoc callback for invalid message handling
		if c.invalidMessageHook != nil {
			err2 := c.invalidMessageHook(ocppErr, string(data), parsedJson)
			// If the hook returns an error, use it as output error. If not, use the original error.
			if err2 != nil {
				ocppErr = err2
				ocppErr.MessageId = messageID
			}
		}
		err = ocppErr
		// Send error to other endpoint if a message ID is available
		if ocppErr.MessageId != "" {
			err2 := c.SendError(ocppErr.MessageId, ocppErr.Code, ocppErr.Description, nil)
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
			log.Debugf("handling incoming CALL [%s, %s]", call.UniqueId, call.Action)
			c.requestHandler(call.Payload, call.UniqueId, call.Action)
		case CALL_RESULT:
			callResult := message.(*CallResult)
			log.Debugf("handling incoming CALL RESULT [%s]", callResult.UniqueId)
			c.dispatcher.CompleteRequest(callResult.GetUniqueId()) // Remove current request from queue and send next one
			if c.responseHandler != nil {
				c.responseHandler(callResult.Payload, callResult.UniqueId)
			}
		case CALL_ERROR:
			callError := message.(*CallError)
			log.Debugf("handling incoming CALL ERROR [%s]", callError.UniqueId)
			c.dispatcher.CompleteRequest(callError.GetUniqueId()) // Remove current request from queue and send next one
			if c.errorHandler != nil {
				c.errorHandler(ocpp.NewError(callError.ErrorCode, callError.ErrorDescription, callError.UniqueId), callError.ErrorDetails)
			}
		}
	}
	return nil
}

// HandleFailedResponseError allows to handle failures while sending responses (either CALL_RESULT or CALL_ERROR).
// It internally analyzes and creates an ocpp.Error based on the given error.
// It will the attempt to send it to the server.
//
// The function helps to prevent starvation on the other endpoint, which is caused by a response never reaching it.
// The method will, however, only attempt to send a default error once.
// If this operation fails, the other endpoint may still starve.
func (c *Client) HandleFailedResponseError(requestID string, err error, featureName string) {
	log.Debugf("handling error for failed response [%s]", requestID)
	var responseErr *ocpp.Error
	// There's several possible errors: invalid profile, invalid payload or send error
	switch err.(type) {
	case validator.ValidationErrors:
		// Validation error
		var validationErr validator.ValidationErrors
		errors.As(err, &validationErr)
		responseErr = errorFromValidation(c, validationErr, requestID, featureName)
	case *ocpp.Error:
		// Internal OCPP error
		errors.As(err, &responseErr)
	case error:
		// Unknown error
		responseErr = ocpp.NewError(GenericError, err.Error(), requestID)
	}
	// Send an OCPP error to the target, since no regular response could be sent
	_ = c.SendError(requestID, responseErr.Code, responseErr.Description, nil)
}

func (c *Client) onDisconnected(err error) {
	log.Error("disconnected from server", err)
	c.dispatcher.Pause()
	if c.onDisconnectedHandler != nil {
		c.onDisconnectedHandler(err)
	}
}

func (c *Client) onReconnected() {
	if c.onReconnectedHandler != nil {
		c.onReconnectedHandler()
	}
	c.dispatcher.Resume()
}

// Transport-specific handlers for new transport interface

func (c *Client) transportMessageHandler(data []byte) error {
	return c.ocppMessageHandler(data)
}

func (c *Client) onTransportDisconnected(err error) {
	log.Error("disconnected from server", err)
	c.dispatcher.Pause()
	if c.onDisconnectedHandler != nil {
		c.onDisconnectedHandler(err)
	}
}

func (c *Client) onTransportReconnected() {
	if c.onReconnectedHandler != nil {
		c.onReconnectedHandler()
	}
	c.dispatcher.Resume()
}

func (c *Client) onTransportError(err error) {
	log.Errorf("transport error: %v", err)
}

// mockWebSocketClient is a minimal implementation of ws.Client to maintain compatibility
// with existing dispatchers when using transport interface
type mockWebSocketClient struct {
	transport transport.ClientTransport
}

func (m *mockWebSocketClient) Start(url string) error {
	// This should not be called when using transport interface
	return fmt.Errorf("Start not supported on mock client, use transport interface")
}

func (m *mockWebSocketClient) StartWithRetries(url string) {
	// This should not be called when using transport interface
	log.Errorf("StartWithRetries not supported on mock client, use transport interface")
}

func (m *mockWebSocketClient) Stop() {
	// This should not be called when using transport interface
	log.Errorf("Stop not supported on mock client, use transport interface")
}

func (m *mockWebSocketClient) Write(data []byte) error {
	// Delegate to the transport interface
	if m.transport != nil {
		return m.transport.Write(data)
	}
	return fmt.Errorf("no transport configured")
}

func (m *mockWebSocketClient) SetMessageHandler(handler func([]byte) error) {
	// This is handled by the transport interface
}

func (m *mockWebSocketClient) SetDisconnectedHandler(handler func(error)) {
	// This is handled by the transport interface
}

func (m *mockWebSocketClient) SetReconnectedHandler(handler func()) {
	// This is handled by the transport interface
}

func (m *mockWebSocketClient) IsConnected() bool {
	// Delegate to the transport interface
	if m.transport != nil {
		return m.transport.IsConnected()
	}
	return false
}

func (m *mockWebSocketClient) Errors() <-chan error {
	// Delegate to the transport interface
	if m.transport != nil {
		return m.transport.Errors()
	}
	// Return an empty error channel
	errorChan := make(chan error)
	close(errorChan)
	return errorChan
}

func (m *mockWebSocketClient) SetTimeoutConfig(config ws.ClientTimeoutConfig) {
	// This is not supported with transport interface
}

func (m *mockWebSocketClient) AddOption(option interface{}) {
	// This is not supported with transport interface
}

func (m *mockWebSocketClient) SetRequestedSubProtocol(subProto string) {
	// This is not supported with transport interface
}

func (m *mockWebSocketClient) SetBasicAuth(username string, password string) {
	// This is not supported with transport interface
}

func (m *mockWebSocketClient) SetHeaderValue(key string, value string) {
	// This is not supported with transport interface
}
