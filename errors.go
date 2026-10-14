package apiresponse

import (
	"context"
	"errors"
	"net/http"
	"slices"
)

// Machine-readable error codes. Clients should switch on these, not on Message.
const (
	CodeBadRequest   = "bad_request"
	CodeUnauthorized = "unauthorized"
	CodeForbidden    = "forbidden"
	CodeNotFound     = "not_found"
	CodeConflict     = "conflict"
	CodeValidation   = "validation_failed"
	CodeRateLimited  = "rate_limited"
	CodeInternal     = "internal_error"

	CodeMethodNotAllowed     = "method_not_allowed"
	CodeUnsupportedMediaType = "unsupported_media_type"
	CodePayloadTooLarge      = "payload_too_large"
	CodeServiceUnavailable   = "service_unavailable"
	CodeDeadlineExceeded     = "deadline_exceeded"
)

// Detail describes one field-level problem, typically for validation errors.
type Detail struct {
	Field   string `json:"field,omitempty"`
	Code    string `json:"code,omitempty"` // stable machine-readable reason, e.g. "required"
	Message string `json:"message"`
}

// ErrorBody is the error payload. It implements error, so handlers can return
// it directly. Its With* methods return copies and never mutate the receiver,
// which makes it safe to keep package-level sentinel values. Details is an
// exported field: copies from With* do not share it, but do not mutate it in
// place on a value other code may hold.
type ErrorBody struct {
	Code      string   `json:"code"`
	Message   string   `json:"message"`
	Details   []Detail `json:"details,omitempty"`
	RequestID string   `json:"request_id,omitempty"`

	status int
	cause  error
}

// ErrorResponse is the error envelope.
type ErrorResponse struct {
	Error *ErrorBody `json:"error"`
}

// New creates an ErrorBody with a custom code and HTTP status.
func New(status int, code, message string) *ErrorBody {
	return &ErrorBody{Code: code, Message: message, status: status}
}

func BadRequest(msg string) *ErrorBody   { return New(http.StatusBadRequest, CodeBadRequest, msg) }
func Unauthorized(msg string) *ErrorBody { return New(http.StatusUnauthorized, CodeUnauthorized, msg) }
func Forbidden(msg string) *ErrorBody    { return New(http.StatusForbidden, CodeForbidden, msg) }
func NotFound(msg string) *ErrorBody     { return New(http.StatusNotFound, CodeNotFound, msg) }
func Conflict(msg string) *ErrorBody     { return New(http.StatusConflict, CodeConflict, msg) }
func RateLimited(msg string) *ErrorBody  { return New(http.StatusTooManyRequests, CodeRateLimited, msg) }

// Internal builds a 500. Never pass err.Error() as msg: the message goes to the
// client verbatim. Keep the real error with [ErrorBody.WithCause] instead.
func Internal(msg string) *ErrorBody { return New(http.StatusInternalServerError, CodeInternal, msg) }

func MethodNotAllowed(msg string) *ErrorBody {
	return New(http.StatusMethodNotAllowed, CodeMethodNotAllowed, msg)
}

func UnsupportedMediaType(msg string) *ErrorBody {
	return New(http.StatusUnsupportedMediaType, CodeUnsupportedMediaType, msg)
}

func PayloadTooLarge(msg string) *ErrorBody {
	return New(http.StatusRequestEntityTooLarge, CodePayloadTooLarge, msg)
}

func ServiceUnavailable(msg string) *ErrorBody {
	return New(http.StatusServiceUnavailable, CodeServiceUnavailable, msg)
}

// DeadlineExceeded is a 504: the server gave up waiting on its own work.
func DeadlineExceeded(msg string) *ErrorBody {
	return New(http.StatusGatewayTimeout, CodeDeadlineExceeded, msg)
}

// Validation builds a 422 error carrying field-level details.
func Validation(details ...Detail) *ErrorBody {
	e := New(http.StatusUnprocessableEntity, CodeValidation, "validation failed")
	e.Details = slices.Clone(details)
	return e
}

// Error implements the error interface.
func (e *ErrorBody) Error() string {
	if e == nil {
		return "<nil>"
	}
	if e.cause != nil {
		return e.Code + ": " + e.Message + ": " + e.cause.Error()
	}
	return e.Code + ": " + e.Message
}

// Unwrap exposes the underlying cause to errors.Is / errors.As.
func (e *ErrorBody) Unwrap() error {
	if e == nil {
		return nil
	}
	return e.cause
}

// Is reports whether target is an *ErrorBody with the same Code and Message.
// This keeps errors.Is(err, ErrSentinel) working after With* methods return
// copies, while sentinels that share a code (ErrUserNotFound, ErrOrderNotFound)
// stay distinct. Use [IsCode] to match on the code alone.
func (e *ErrorBody) Is(target error) bool {
	t, ok := target.(*ErrorBody)
	return ok && e != nil && t != nil && e.Code == t.Code && e.Message == t.Message
}

// IsCode reports whether err wraps an *ErrorBody with the given code.
func IsCode(err error, code string) bool {
	eb := findErrorBody(err)
	return eb != nil && eb.Code == code
}

// ContextErrors is an opt-in ErrorMapper that turns context.DeadlineExceeded
// into a 504. context.Canceled is left alone: the client has already gone.
//
//	apiresponse.NewWriter(apiresponse.WithErrorMapper(apiresponse.ContextErrors))
func ContextErrors(err error) *ErrorBody {
	if errors.Is(err, context.DeadlineExceeded) {
		return DeadlineExceeded("request timed out")
	}
	return nil
}

// HTTPStatus returns the HTTP status for this error (500 if unset).
func (e *ErrorBody) HTTPStatus() int {
	if e == nil || e.status == 0 {
		return http.StatusInternalServerError
	}
	return e.status
}

// clone returns a copy that shares no mutable state with e, so mutating one
// copy's Details never affects the original (or a package-level sentinel). A
// nil e yields a generic internal error rather than a panic.
func (e *ErrorBody) clone() *ErrorBody {
	if e == nil {
		return Internal("internal server error")
	}
	c := *e
	c.Details = slices.Clone(e.Details)
	return &c
}

// WithCause attaches an underlying error for logging and errors.Is/As.
// The cause is never serialized to the client.
func (e *ErrorBody) WithCause(err error) *ErrorBody {
	c := e.clone()
	c.cause = err
	return c
}

// WithDetails returns a copy with the given details.
func (e *ErrorBody) WithDetails(details ...Detail) *ErrorBody {
	c := e.clone()
	c.Details = slices.Clone(details)
	return c
}

// WithRequestID returns a copy tagged with a request ID.
func (e *ErrorBody) WithRequestID(id string) *ErrorBody {
	c := e.clone()
	c.RequestID = id
	return c
}

// WithStatus returns a copy with a different HTTP status. The status must be in
// 100-999; anything else makes Write answer with a generic 500.
func (e *ErrorBody) WithStatus(status int) *ErrorBody {
	c := e.clone()
	c.status = status
	return c
}

// FromError converts any error into an *ErrorBody. If err already wraps an
// *ErrorBody it is returned as is. Anything else becomes a generic 500 whose
// message does not leak internals; the original is kept as the cause.
func FromError(err error) *ErrorBody {
	if eb := findErrorBody(err); eb != nil {
		return eb
	}
	return Internal("internal server error").WithCause(err)
}

// findErrorBody returns the first non-nil *ErrorBody in err's chain, or nil.
// A typed-nil *ErrorBody is skipped so callers never dereference it.
func findErrorBody(err error) *ErrorBody {
	var eb *ErrorBody
	if errors.As(err, &eb) && eb != nil {
		return eb
	}
	return nil
}
