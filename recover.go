package apiresponse

import (
	"fmt"
	"net/http"
	"runtime/debug"
)

// Recover is middleware that turns a panic in next into the standard 500 error
// envelope, logging the panic value and stack through the Logger. It re-panics
// http.ErrAbortHandler, as net/http expects. If the handler already started
// writing the response, the status cannot be changed.
func (wr *Writer) Recover(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer func() {
			v := recover()
			if v == nil {
				return
			}
			if v == http.ErrAbortHandler {
				panic(v)
			}
			_ = wr.WriteError(w, r, &panicError{value: v, stack: debug.Stack()})
		}()
		next.ServeHTTP(w, r)
	})
}

// panicError carries a recovered panic value. The stack is kept out of the
// message so it can be logged as its own attribute.
type panicError struct {
	value any
	stack []byte
}

func (p *panicError) Error() string { return fmt.Sprintf("panic: %v", p.value) }
