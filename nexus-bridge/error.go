package bridge

import (
	"errors"
	"fmt"

	"github.com/gorilla/websocket"
)

// ErrInteractionRequired is returned when the gateway responds with 409
// attention_required, indicating the user must re-authenticate. The bridge
// must stop retrying — reconnecting will never succeed without user action.
var ErrInteractionRequired = errors.New("interaction required: user must re-authenticate")

// permanentCloseCodes contains WebSocket close codes that should not be retried.
var permanentCloseCodes = map[int]bool{
	websocket.CloseNormalClosure:           true,
	websocket.ClosePolicyViolation:         true,
	websocket.CloseInternalServerErr:       true,
	websocket.CloseInvalidFramePayloadData: true, // e.g. invalid auth
}

// PermanentError represents an error that should not be retried.
// When the bridge encounters this error, it will stop the reconnection loop.
type PermanentError struct {
	Err error
}

// NewPermanentError creates a new PermanentError.
func NewPermanentError(err error) *PermanentError {
	return &PermanentError{Err: err}
}

// Error implements the error interface.
func (e *PermanentError) Error() string {
	return fmt.Sprintf("permanent error: %v", e.Err)
}

// Unwrap provides compatibility for Go 1.13+ error chains.
func (e *PermanentError) Unwrap() error {
	return e.Err
}
