package router

import (
	"fmt"

	"github.com/croge130/archipelago/wire"
)

// Error is what a handler returns to send a specific coded error to the
// caller. Any other error a handler returns is reported as ErrInternal
// with a generic message, and its text is logged on this side only.
type Error struct {
	Code    wire.ErrorCode
	Message string
}

func (e *Error) Error() string { return fmt.Sprintf("%s: %s", e.Code, e.Message) }

// Errorf builds an *Error.
func Errorf(code wire.ErrorCode, format string, args ...any) *Error {
	return &Error{Code: code, Message: fmt.Sprintf(format, args...)}
}

// RemoteError is a coded error the peer answered a call with.
type RemoteError struct {
	Code    wire.ErrorCode
	Message string
}

func (e *RemoteError) Error() string {
	if e.Message == "" {
		return fmt.Sprintf("router: remote error: %s", e.Code)
	}
	return fmt.Sprintf("router: remote error: %s: %s", e.Code, e.Message)
}
