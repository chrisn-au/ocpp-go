package transport

import "errors"

// Common transport errors.
var (
	// ErrNotConnected indicates that the transport is not connected.
	ErrNotConnected = errors.New("transport not connected")

	// ErrAlreadyStarted indicates that the transport is already started.
	ErrAlreadyStarted = errors.New("transport already started")

	// ErrNotStarted indicates that the transport has not been started.
	ErrNotStarted = errors.New("transport not started")

	// ErrClientNotFound indicates that the specified client ID was not found.
	ErrClientNotFound = errors.New("client not found")

	// ErrInvalidConfig indicates that the provided configuration is invalid.
	ErrInvalidConfig = errors.New("invalid configuration")

	// ErrInvalidPort indicates that the provided port number is invalid.
	ErrInvalidPort = errors.New("invalid port number")

	// ErrInvalidAddress indicates that the provided address is invalid.
	ErrInvalidAddress = errors.New("invalid address")

	// ErrConnectionClosed indicates that the connection has been closed.
	ErrConnectionClosed = errors.New("connection closed")

	// ErrWriteTimeout indicates that a write operation timed out.
	ErrWriteTimeout = errors.New("write timeout")

	// ErrReadTimeout indicates that a read operation timed out.
	ErrReadTimeout = errors.New("read timeout")

	// ErrMessageTooLarge indicates that a message exceeds the maximum allowed size.
	ErrMessageTooLarge = errors.New("message too large")

	// ErrUnsupportedTransport indicates that the transport type is not supported.
	ErrUnsupportedTransport = errors.New("unsupported transport type")
)