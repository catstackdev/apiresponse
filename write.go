package apiresponse

import "net/http"

// HandlerFunc is an http handler that can return an error.
type HandlerFunc func(w http.ResponseWriter, r *http.Request) error

// Write writes v as JSON using the default Writer. See [Writer.Write].
func Write(w http.ResponseWriter, status int, v any) error {
	return defaultWriter.Write(w, status, v)
}

// WriteError writes err as an error envelope using the default Writer, which
// has no custom mappers. Use a [Writer] for mapping or a request-aware Logger.
func WriteError(w http.ResponseWriter, err error) error {
	return defaultWriter.WriteError(w, nil, err)
}

// Handler adapts fn using the default Writer. See [Writer.Handler].
func Handler(fn HandlerFunc) http.HandlerFunc {
	return defaultWriter.Handler(fn)
}

// Recover is panic-recovery middleware using the default Writer. See [Writer.Recover].
func Recover(next http.Handler) http.Handler {
	return defaultWriter.Recover(next)
}

// NotFoundHandler is [Writer.NotFoundHandler] using the default Writer.
func NotFoundHandler() http.HandlerFunc { return defaultWriter.NotFoundHandler() }

// MethodNotAllowedHandler is [Writer.MethodNotAllowedHandler] using the default
// Writer.
func MethodNotAllowedHandler(allow ...string) http.HandlerFunc {
	return defaultWriter.MethodNotAllowedHandler(allow...)
}

var internalBody = []byte(`{"error":{"code":"internal_error","message":"internal server error"}}`)

func writeRaw(w http.ResponseWriter, status int, body []byte) error {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_, err := w.Write(body)
	return err
}

func bodyAllowed(status int) bool {
	return status >= 200 && status != http.StatusNoContent && status != http.StatusNotModified
}
