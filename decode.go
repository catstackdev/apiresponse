package apiresponse

import (
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
// 415 for a non-JSON Content-Type, 413 for an oversized body, 400 for anything
// malformed. A missing Content-Type is accepted.
func Decode[T any](w http.ResponseWriter, r *http.Request) (T, error) {
	return DecodeLimit[T](w, r, DefaultMaxBodyBytes)
}

// DecodeLimit is [Decode] with a custom body limit in bytes.
func DecodeLimit[T any](w http.ResponseWriter, r *http.Request, maxBytes int64) (T, error) {
	var v T
	if ct := r.Header.Get("Content-Type"); ct != "" {
		if mt, _, err := mime.ParseMediaType(ct); err != nil || mt != "application/json" {
			return v, UnsupportedMediaType("Content-Type must be application/json")
		}
	}
	dec := json.NewDecoder(http.MaxBytesReader(w, r.Body, maxBytes))
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
