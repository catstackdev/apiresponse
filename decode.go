package apiresponse

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime"
	"net/http"
	"strings"
)

// DefaultMaxBodyBytes is the request body limit used by [Decode].
const DefaultMaxBodyBytes int64 = 1 << 20

// Decode reads a JSON request body into T. The body is limited to
// [DefaultMaxBodyBytes], unknown fields are rejected, and exactly one JSON value
// is allowed. Failures are returned as *ErrorBody, ready for [Handler]:
// 415 for a non-JSON Content-Type (application/json or application/*+json), 413
// for an oversized body (on HTTP/1 also sets "Connection: close" on w), 400 for
// anything malformed, including a literal null body. A missing Content-Type is
// accepted.
func Decode[T any](w http.ResponseWriter, r *http.Request) (T, error) {
	return DecodeLimit[T](w, r, DefaultMaxBodyBytes)
}

// DecodeLimit is [Decode] with a custom body limit in bytes. A limit of zero or
// less falls back to [DefaultMaxBodyBytes].
func DecodeLimit[T any](w http.ResponseWriter, r *http.Request, maxBytes int64) (T, error) {
	var v T
	if ct := r.Header.Get("Content-Type"); ct != "" {
		if mt, _, err := mime.ParseMediaType(ct); err != nil || !isJSONMediaType(mt) {
			return v, UnsupportedMediaType("Content-Type must be JSON")
		}
	}
	if maxBytes <= 0 {
		maxBytes = DefaultMaxBodyBytes
	}
	body := r.Body
	if body == nil { // hand-built requests (e.g. in unit tests) may have no body
		body = http.NoBody
	}
	data, err := io.ReadAll(http.MaxBytesReader(w, body, maxBytes))
	if err != nil {
		var tooLarge *http.MaxBytesError
		if w != nil && r.ProtoMajor == 1 && errors.As(err, &tooLarge) {
			// MaxBytesReader normally flags this itself, but only when handed the
			// server's own ResponseWriter, not a wrapper such as Handler's. HTTP/1
			// only: the HTTP/2 server treats "Connection: close" as a request to
			// shut down the whole multiplexed connection.
			w.Header().Set("Connection", "close")
		}
		return v, decodeError(err)
	}
	if bytes.Equal(bytes.TrimSpace(data), []byte("null")) {
		return v, BadRequest("request body must not be null")
	}
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&v); err != nil {
		return v, decodeError(err)
	}
	if _, err := dec.Token(); !errors.Is(err, io.EOF) {
		if err == nil {
			return v, BadRequest("request body must contain a single JSON value")
		}
		return v, decodeError(err)
	}
	return v, nil
}

// isJSONMediaType reports whether mt is application/json or an application/*+json
// structured-syntax type such as application/problem+json.
func isJSONMediaType(mt string) bool {
	return mt == "application/json" ||
		(strings.HasPrefix(mt, "application/") && strings.HasSuffix(mt, "+json"))
}

func decodeError(err error) *ErrorBody {
	var (
		syntaxErr *json.SyntaxError
		typeErr   *json.UnmarshalTypeError
		tooLarge  *http.MaxBytesError
	)
	switch {
	case errors.As(err, &tooLarge):
		return PayloadTooLarge("request body too large").WithCause(err)
	case errors.Is(err, io.EOF):
		return BadRequest("request body is empty").WithCause(err)
	case errors.As(err, &typeErr):
		if typeErr.Field == "" { // top-level mismatch, e.g. a string where an object is expected
			return BadRequest("invalid request body").WithCause(err)
		}
		return BadRequest(fmt.Sprintf("invalid value for field %q", typeErr.Field)).WithCause(err)
	case errors.As(err, &syntaxErr), errors.Is(err, io.ErrUnexpectedEOF):
		return BadRequest("malformed JSON").WithCause(err)
	default:
		// encoding/json has no typed error for unknown fields; the message only
		// echoes the client's own field name, so it is safe to expose. Anything
		// else (e.g. a read error) gets a fixed message to avoid leaking internals.
		if msg := err.Error(); strings.HasPrefix(msg, "json: unknown field ") {
			return BadRequest(msg).WithCause(err)
		}
		return BadRequest("invalid request body").WithCause(err)
	}
}
