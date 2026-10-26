package apiresponse

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"strconv"
	"strings"
	"syscall"
	"time"
)

// ErrorMapper converts a domain error into an *ErrorBody. Returning nil means
// "not mine" and lets the next mapper (or the generic 500 fallback) handle it.
type ErrorMapper func(err error) *ErrorBody

// Logger is called for every response with a 5xx status. r is nil when the
// caller used WriteError without a request. err is the original error, so the
// real cause is visible even though the client only sees a generic message.
type Logger func(r *http.Request, err error, eb *ErrorBody)

// Writer writes envelopes with configurable error mapping and logging. Create
// one with NewWriter: the zero value works but has no mappers, request IDs or
// logging (NewWriter enables the default slog logger). A Writer is safe for
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
	eb := wr.prepare(r, err)
	if eb.retryAfter > 0 {
		w.Header().Set("Retry-After", retryAfterSeconds(eb.retryAfter))
	}
	return wr.Write(w, eb.HTTPStatus(), ErrorResponse{Error: eb})
}

// prepare resolves err, fills in the request ID and logs. It is shared by
// WriteError and by the paths that must log but cannot write (the response has
// already started).
func (wr *Writer) prepare(r *http.Request, err error) *ErrorBody {
	eb := wr.resolve(err)
	if eb.RequestID == "" && wr.requestID != nil && r != nil {
		if id := wr.requestID(r); id != "" {
			eb = eb.WithRequestID(id)
		}
	}
	if eb.HTTPStatus() >= http.StatusInternalServerError && wr.logger != nil {
		wr.logger(r, err, eb)
	}
	return eb
}

// Handler adapts a HandlerFunc to http.HandlerFunc, writing any returned error
// with WriteError. The result works with net/http, chi, and anything else that
// accepts an http.Handler. If fn already started the response, the returned
// error cannot be sent: it is passed to the Logger (when its status qualifies)
// and no second envelope is appended; a broken-pipe or connection-reset error
// is dropped entirely, since the client is gone. Headers set before returning
// an error are kept, except Content-Length and Content-Disposition, which
// describe a body that was never sent and would corrupt the error envelope.
func (wr *Writer) Handler(fn HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		tw := &trackingWriter{ResponseWriter: w}
		err := fn(tw, r)
		if err == nil {
			return
		}
		if tw.wrote {
			if !clientGone(err) {
				_ = wr.prepare(r, err)
			}
			return
		}
		clearEntityHeaders(w.Header())
		_ = wr.WriteError(w, r, err)
	}
}

// NotFoundHandler answers with a 404 error envelope. Register it where your
// router lets you replace the default plain-text 404, e.g. chi's r.NotFound or
// a catch-all "/" pattern on net/http.
func (wr *Writer) NotFoundHandler() http.HandlerFunc {
	return wr.Handler(func(http.ResponseWriter, *http.Request) error {
		return NotFound("resource not found")
	})
}

// MethodNotAllowedHandler answers with a 405 error envelope and an Allow header
// listing the given methods, e.g. MethodNotAllowedHandler("GET", "POST").
func (wr *Writer) MethodNotAllowedHandler(allow ...string) http.HandlerFunc {
	allowed := strings.Join(allow, ", ")
	return wr.Handler(func(w http.ResponseWriter, _ *http.Request) error {
		if allowed != "" {
			w.Header().Set("Allow", allowed)
		}
		return MethodNotAllowed("method not allowed")
	})
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

// retryAfterSeconds renders d as whole seconds, rounded up (written without
// adding to d, which could overflow for huge durations).
func retryAfterSeconds(d time.Duration) string {
	secs := int64(d / time.Second)
	if d%time.Second != 0 {
		secs++
	}
	return strconv.FormatInt(secs, 10)
}

// clearEntityHeaders drops headers that describe a response body the handler
// never finished sending. Content-Encoding is kept: a compression middleware
// owns it and will still encode the error envelope.
func clearEntityHeaders(h http.Header) {
	h.Del("Content-Length")
	h.Del("Content-Disposition")
}

// clientGone reports a write error that means the client went away mid-response.
// It is only used once the response has started, where such an error comes from
// our own write; the same errno from a database or upstream call is a real
// server fault and must still be logged.
func clientGone(err error) bool {
	return errors.Is(err, syscall.EPIPE) || errors.Is(err, syscall.ECONNRESET)
}

// defaultLogger logs a 5xx with slog. It skips context.Canceled: the client has
// already gone, so it is not a server fault and would only add noise. A custom
// Logger still sees every 5xx and can make its own call.
func defaultLogger(r *http.Request, err error, eb *ErrorBody) {
	if errors.Is(err, context.Canceled) {
		return
	}
	attrs := []any{"code", eb.Code, "status", eb.HTTPStatus()}
	if err != nil {
		attrs = append(attrs, "error", err)
	}
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
