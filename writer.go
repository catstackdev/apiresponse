package apiresponse

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
)

// ErrorMapper converts a domain error into an *ErrorBody. Returning nil means
// "not mine" and lets the next mapper (or the generic 500 fallback) handle it.
type ErrorMapper func(err error) *ErrorBody

// Logger is called for every response with a 5xx status. r is nil when the
// caller used WriteError without a request. err is the original error, so the
// real cause is visible even though the client only sees a generic message.
type Logger func(r *http.Request, err error, eb *ErrorBody)

// Writer writes envelopes with configurable error mapping and logging. The
// zero value is not usable; create one with NewWriter. A Writer is safe for
// concurrent use once constructed.
type Writer struct {
	mappers   []ErrorMapper
	logger    Logger
	requestID func(r *http.Request) string
}

// Option configures a Writer.
type Option func(*Writer)

// WithErrorMapper registers a mapper. Mappers run in registration order, after
// the check for an existing *ErrorBody. The mapped message is sent to the client
// verbatim, so for 5xx results use a fixed message and keep the raw error as the
// cause (the Writer attaches it automatically).
func WithErrorMapper(m ErrorMapper) Option {
	return func(wr *Writer) { wr.mappers = append(wr.mappers, m) }
}

// WithLogger replaces the default slog-based logger. Pass nil to disable logging.
func WithLogger(l Logger) Option {
	return func(wr *Writer) { wr.logger = l }
}

// WithRequestIDFunc sets how to obtain a request ID from the request, e.g.
// reading the X-Request-ID header or a context value set by middleware. Errors
// without a request ID get one filled in automatically (on a copy, so shared
// sentinel errors are never mutated). Has no effect when WriteError is called
// without a request.
func WithRequestIDFunc(fn func(r *http.Request) string) Option {
	return func(wr *Writer) { wr.requestID = fn }
}

// NewWriter creates a Writer. By default 5xx errors are logged with slog
// (including the request ID when WithRequestIDFunc is set), except
// context.Canceled.
func NewWriter(opts ...Option) *Writer {
	wr := &Writer{logger: defaultLogger}
	for _, opt := range opts {
		opt(wr)
	}
	return wr
}

var defaultWriter = NewWriter()

// Write marshals v as JSON and writes it with the given status. Marshalling
// happens before any header is sent, so an encoding failure yields a clean 500
// instead of a half-written body. Statuses that forbid a body (1xx, 204, 304)
// write headers only. HTML characters are not escaped.
func (wr *Writer) Write(w http.ResponseWriter, status int, v any) error {
	if status < 100 || status > 999 {
		// net/http panics on these; answer with a clean 500 instead.
		_ = writeRaw(w, http.StatusInternalServerError, internalBody)
		return fmt.Errorf("apiresponse: invalid HTTP status %d", status)
	}
	if !bodyAllowed(status) {
		w.WriteHeader(status)
		return nil
	}
	body, err := marshal(v)
	if err != nil {
		_ = writeRaw(w, http.StatusInternalServerError, internalBody)
		return err
	}
	return writeRaw(w, status, body)
}

// WriteError resolves err to an *ErrorBody and writes it as an error envelope.
// Resolution order: an *ErrorBody already in the chain, then registered
// mappers, then a generic 500. r may be nil; it is only passed to the Logger.
// A nil err is treated like any unknown error: a generic 500 (mappers are not
// consulted, so they never see nil). The returned error is the
// write/marshal failure, not err itself.
func (wr *Writer) WriteError(w http.ResponseWriter, r *http.Request, err error) error {
	eb := wr.resolve(err)
	if eb.RequestID == "" && wr.requestID != nil && r != nil {
		if id := wr.requestID(r); id != "" {
			eb = eb.WithRequestID(id)
		}
	}
	if eb.HTTPStatus() >= http.StatusInternalServerError && wr.logger != nil {
		wr.logger(r, err, eb)
	}
	return wr.Write(w, eb.HTTPStatus(), ErrorResponse{Error: eb})
}

// Handler adapts a HandlerFunc to http.HandlerFunc, writing any returned error
// with WriteError. The result works with net/http, chi, and anything else that
// accepts an http.Handler. Return an error only before writing a response: if fn
// has already written a body, the error envelope is written after it. Headers
// set before returning an error (e.g. Retry-After on a 429) are kept.
func (wr *Writer) Handler(fn HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if err := fn(w, r); err != nil {
			_ = wr.WriteError(w, r, err)
		}
	}
}

func (wr *Writer) resolve(err error) *ErrorBody {
	if eb := findErrorBody(err); eb != nil {
		return eb
	}
	if err == nil {
		return FromError(nil)
	}
	for _, m := range wr.mappers {
		if mapped := m(err); mapped != nil {
			if mapped.cause == nil {
				mapped = mapped.WithCause(err)
			}
			return mapped
		}
	}
	return FromError(err)
}

// defaultLogger logs a 5xx with slog. It skips context.Canceled: the client has
// already gone, so it is not a server fault and would only add noise. A custom
// Logger still sees every 5xx and can make its own call.
func defaultLogger(r *http.Request, err error, eb *ErrorBody) {
	if errors.Is(err, context.Canceled) {
		return
	}
	attrs := []any{"code", eb.Code, "status", eb.HTTPStatus(), "error", err}
	if eb.RequestID != "" {
		attrs = append(attrs, "request_id", eb.RequestID)
	}
	if r != nil {
		attrs = append(attrs, "method", r.Method, "path", r.URL.Path)
	}
	var pe *panicError
	if errors.As(err, &pe) {
		attrs = append(attrs, "stack", string(pe.stack))
	}
	slog.Error("apiresponse: server error", attrs...)
}

func marshal(v any) ([]byte, error) {
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	if err := enc.Encode(v); err != nil {
		return nil, err
	}
	return bytes.TrimRight(buf.Bytes(), "\n"), nil
}
