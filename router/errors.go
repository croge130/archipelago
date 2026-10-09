package router

import (
	"errors"
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

// Retryable reports whether sending the same request again later can
// succeed: only `busy`, which says the peer is over its in-flight limit,
// not that anything about the request was wrong. Every other code is a
// verdict on the request or on the caller (`unauthorized`, `invalid`,
// `unknown_route`, `version_unsupported`), a cancellation the caller or
// the peer chose, or an internal failure whose retry safety only the
// operation's own idempotency can decide. Retrying those is the caller's
// call, not the router's.
func (e *RemoteError) Retryable() bool { return e.Code == wire.ErrBusy }

// IsRetryable reports whether err is a RemoteError the peer marked, by
// its code, as worth retrying. A lost session or a local timeout is not
// one: the caller decides whether to redial.
func IsRetryable(err error) bool {
	var re *RemoteError
	return errors.As(err, &re) && re.Retryable()
}
