package apiresponse

import (
	"fmt"
	"net/http"
	"runtime/debug"
)

// Recover is middleware that turns a panic in next into the standard 500 error
// envelope, logging the panic value and stack through the Logger. It re-panics
// http.ErrAbortHandler, as net/http expects. If the handler already started
// writing the response, the panic is logged but no envelope is appended. The
// recovered value is not unwrapped: even if the panic value is an error,
// errors.Is/As and mappers see only the opaque panic, which always becomes a
// generic 500.
func (wr *Writer) Recover(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		tw := &trackingWriter{ResponseWriter: w}
		defer func() {
			v := recover()
			if v == nil {
				return
			}
			if v == http.ErrAbortHandler {
				panic(v)
			}
			perr := &panicError{value: v, stack: debug.Stack()}
			if tw.wrote {
				_ = wr.prepare(r, perr) // too late to change the response; just log
				return
			}
			clearEntityHeaders(w.Header())
			_ = wr.WriteError(w, r, perr)
		}()
		next.ServeHTTP(tw, r)
	})
}

// panicError carries a recovered panic value. The stack is kept out of the
// message so it can be logged as its own attribute.
type panicError struct {
	value any
	stack []byte
}

func (p *panicError) Error() string { return fmt.Sprintf("panic: %v", p.value) }
