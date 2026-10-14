package apiresponse

import (
	"bufio"
	"net"
	"net/http"
)

// trackingWriter records whether a final response has started, so Handler and
// Recover can avoid appending an error envelope to a half-written response.
// It forwards Flush and Hijack (so type assertions such as w.(http.Hijacker)
// in websocket upgraders keep working) and exposes the wrapped writer through
// Unwrap for http.ResponseController.
type trackingWriter struct {
	http.ResponseWriter
	wrote bool
}

func (t *trackingWriter) WriteHeader(code int) {
	// 1xx (except 101) are interim responses; the final one is still to come.
	if code < 100 || code >= 200 || code == http.StatusSwitchingProtocols {
		t.wrote = true
	}
	t.ResponseWriter.WriteHeader(code)
}

func (t *trackingWriter) Write(b []byte) (int, error) {
	t.wrote = true
	return t.ResponseWriter.Write(b)
}

func (t *trackingWriter) Flush() {
	t.wrote = true
	_ = http.NewResponseController(t.ResponseWriter).Flush()
}

func (t *trackingWriter) Hijack() (net.Conn, *bufio.ReadWriter, error) {
	t.wrote = true
	return http.NewResponseController(t.ResponseWriter).Hijack()
}

func (t *trackingWriter) Unwrap() http.ResponseWriter { return t.ResponseWriter }
