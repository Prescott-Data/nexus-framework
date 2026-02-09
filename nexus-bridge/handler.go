package bridge

// Handler defines the interface for handling events from a persistent connection.
type Handler interface {
	// OnConnect is called when a new connection is successfully established.
	// The `send` function can be used by the handler to send messages back
	// through the bridge.
	OnConnect(send func(message []byte) error)

	// OnMessage is called when a new message is received from the connection.
	OnMessage(message []byte)

	// OnDisconnect is called when the connection is lost. The bridge will
	// automatically attempt to reconnect.
	OnDisconnect(err error)
}
