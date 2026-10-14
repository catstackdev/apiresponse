package apiresponse

import (
	"bufio"
	"errors"
	"io"
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
	// Mark after the call: net/http panics on an invalid code, and nothing has
	// been sent then, so Recover must still be able to answer with a 500.
	t.ResponseWriter.WriteHeader(code)
	// 1xx (except 101) are interim responses; the final one is still to come.
	if code < 100 || code >= 200 || code == http.StatusSwitchingProtocols {
		t.wrote = true
	}
}

func (t *trackingWriter) Write(b []byte) (int, error) {
	t.wrote = true
	return t.ResponseWriter.Write(b)
}

// ReadFrom keeps io.Copy, http.ServeContent and http.ServeFile on net/http's
// sendfile path: io.Copy uses the wrapped writer's own ReadFrom when it has one.
func (t *trackingWriter) ReadFrom(src io.Reader) (int64, error) {
	t.wrote = true
	return io.Copy(t.ResponseWriter, src)
}

func (t *trackingWriter) Flush() {
	// Only ErrNotSupported means nothing was sent: net/http commits the 200
	// header before it flushes, so any other failure (client gone, write error)
	// has already started the response.
	if err := http.NewResponseController(t.ResponseWriter).Flush(); !errors.Is(err, http.ErrNotSupported) {
		t.wrote = true
	}
}

func (t *trackingWriter) Hijack() (net.Conn, *bufio.ReadWriter, error) {
	conn, rw, err := http.NewResponseController(t.ResponseWriter).Hijack()
	if err == nil {
		t.wrote = true // a failed hijack (e.g. HTTP/2) leaves the response untouched
	}
	return conn, rw, err
}

func (t *trackingWriter) Unwrap() http.ResponseWriter { return t.ResponseWriter }
