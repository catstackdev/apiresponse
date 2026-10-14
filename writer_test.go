package apiresponse_test

import (
	"bufio"
	"bytes"
	"context"
	"errors"
	"log/slog"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/catstackdev/apiresponse"
)

var errNoRows = errors.New("no rows")

type logCall struct {
	r   *http.Request
	err error
	eb  *apiresponse.ErrorBody
}

func newTestWriter(calls *[]logCall) *apiresponse.Writer {
	return apiresponse.NewWriter(
		apiresponse.WithErrorMapper(func(err error) *apiresponse.ErrorBody {
			if errors.Is(err, errNoRows) {
				return apiresponse.NotFound("not found")
			}
			return nil
		}),
		apiresponse.WithLogger(func(r *http.Request, err error, eb *apiresponse.ErrorBody) {
			*calls = append(*calls, logCall{r, err, eb})
		}),
	)
}

func TestWriter_MapperTranslatesDomainError(t *testing.T) {
	var calls []logCall
	rec := httptest.NewRecorder()
	_ = newTestWriter(&calls).WriteError(rec, nil, errNoRows)
	if rec.Code != 404 || !strings.Contains(rec.Body.String(), "not_found") {
		t.Fatalf("code=%d body=%s", rec.Code, rec.Body)
	}
	if len(calls) != 0 {
		t.Fatal("4xx must not be logged")
	}
}

func TestWriter_ExistingErrorBodyBeatsMappers(t *testing.T) {
	var calls []logCall
	rec := httptest.NewRecorder()
	_ = newTestWriter(&calls).WriteError(rec, nil, apiresponse.Conflict("dup"))
	if rec.Code != 409 {
		t.Fatalf("code=%d", rec.Code)
	}
}

func TestWriter_LogsUnmappedErrorWithOriginalCause(t *testing.T) {
	var calls []logCall
	cause := errors.New("db down")
	req := httptest.NewRequest("GET", "/x", nil)
	rec := httptest.NewRecorder()
	_ = newTestWriter(&calls).WriteError(rec, req, cause)

	if rec.Code != 500 || strings.Contains(rec.Body.String(), "db down") {
		t.Fatalf("code=%d body=%s", rec.Code, rec.Body)
	}
	if len(calls) != 1 || calls[0].err != cause || calls[0].r != req {
		t.Fatalf("logger calls: %+v", calls)
	}
}

func TestWriter_MappedErrorKeepsCause(t *testing.T) {
	cause := errors.New("orig")
	var got *apiresponse.ErrorBody
	wr := apiresponse.NewWriter(
		apiresponse.WithErrorMapper(func(error) *apiresponse.ErrorBody { return apiresponse.Internal("m") }),
		apiresponse.WithLogger(func(_ *http.Request, _ error, eb *apiresponse.ErrorBody) { got = eb }),
	)
	_ = wr.WriteError(httptest.NewRecorder(), nil, cause)
	if !errors.Is(got, cause) {
		t.Fatal("mapped error lost its cause")
	}
}

func TestRecover_TurnsPanicInto500(t *testing.T) {
	var calls []logCall
	h := newTestWriter(&calls).Recover(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		panic("kaboom")
	}))
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest("GET", "/", nil))

	if rec.Code != 500 || strings.Contains(rec.Body.String(), "kaboom") {
		t.Fatalf("code=%d body=%s", rec.Code, rec.Body)
	}
	if len(calls) != 1 || !strings.Contains(calls[0].err.Error(), "kaboom") {
		t.Fatalf("panic not logged: %+v", calls)
	}
}

func TestRecover_RepanicsAbortHandler(t *testing.T) {
	h := apiresponse.Recover(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		panic(http.ErrAbortHandler)
	}))
	defer func() {
		if recover() != http.ErrAbortHandler {
			t.Fatal("ErrAbortHandler must propagate")
		}
	}()
	h.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest("GET", "/", nil))
}

func TestWriter_MappersRunInOrderFirstWins(t *testing.T) {
	wr := apiresponse.NewWriter(
		apiresponse.WithLogger(nil),
		apiresponse.WithErrorMapper(func(error) *apiresponse.ErrorBody { return nil }),
		apiresponse.WithErrorMapper(func(error) *apiresponse.ErrorBody { return apiresponse.Forbidden("first") }),
		apiresponse.WithErrorMapper(func(error) *apiresponse.ErrorBody { return apiresponse.Conflict("second") }),
	)
	rec := httptest.NewRecorder()
	_ = wr.WriteError(rec, nil, errors.New("x"))
	if rec.Code != 403 {
		t.Fatalf("code=%d", rec.Code)
	}
}

func TestWriter_DefaultLoggerHandlesNilRequest(t *testing.T) {
	// Default logger (slog) must not panic with a nil request or on 5xx.
	rec := httptest.NewRecorder()
	_ = apiresponse.WriteError(rec, errors.New("boom"))
	if rec.Code != 500 {
		t.Fatalf("code=%d", rec.Code)
	}
}

func TestWriter_ConcurrentUse(t *testing.T) {
	wr := apiresponse.NewWriter(apiresponse.WithLogger(nil))
	var wg sync.WaitGroup
	for i := 0; i < 50; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			rec := httptest.NewRecorder()
			_ = wr.WriteError(rec, nil, apiresponse.NotFound("x"))
		}()
	}
	wg.Wait()
}

func TestRecover_PassesThroughWhenNoPanic(t *testing.T) {
	h := apiresponse.Recover(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusTeapot)
	}))
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest("GET", "/", nil))
	if rec.Code != http.StatusTeapot {
		t.Fatalf("code=%d", rec.Code)
	}
}

func TestWriter_RequestIDFilledIn(t *testing.T) {
	wr := apiresponse.NewWriter(
		apiresponse.WithLogger(nil),
		apiresponse.WithRequestIDFunc(func(r *http.Request) string { return r.Header.Get("X-Request-ID") }),
	)
	req := httptest.NewRequest("GET", "/", nil)
	req.Header.Set("X-Request-ID", "req_abc123")

	shared := apiresponse.NotFound("x")
	rec := httptest.NewRecorder()
	_ = wr.WriteError(rec, req, shared)
	if !strings.Contains(rec.Body.String(), `"request_id":"req_abc123"`) {
		t.Fatalf("body %s", rec.Body)
	}
	if shared.RequestID != "" {
		t.Fatal("shared sentinel was mutated")
	}
}

func TestWriter_RequestIDNotOverwritten(t *testing.T) {
	wr := apiresponse.NewWriter(
		apiresponse.WithRequestIDFunc(func(*http.Request) string { return "auto" }),
	)
	rec := httptest.NewRecorder()
	_ = wr.WriteError(rec, httptest.NewRequest("GET", "/", nil), apiresponse.NotFound("x").WithRequestID("manual"))
	if !strings.Contains(rec.Body.String(), `"request_id":"manual"`) {
		t.Fatalf("body %s", rec.Body)
	}
}

func TestWriter_RequestIDSkippedWithoutRequestOrID(t *testing.T) {
	wr := apiresponse.NewWriter(
		apiresponse.WithRequestIDFunc(func(*http.Request) string { return "" }),
	)
	rec := httptest.NewRecorder()
	_ = wr.WriteError(rec, nil, apiresponse.NotFound("x"))
	_ = wr.WriteError(rec, httptest.NewRequest("GET", "/", nil), apiresponse.NotFound("x"))
	if strings.Contains(rec.Body.String(), "request_id") {
		t.Fatalf("body %s", rec.Body)
	}
}

func TestRecover_DefaultLoggerHandlesPanicStack(t *testing.T) {
	h := apiresponse.Recover(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		panic("kaboom")
	}))
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/", nil))
	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want 500", rec.Code)
	}
}

func TestWriter_NilErrorSkipsMappers(t *testing.T) {
	wr := apiresponse.NewWriter(
		apiresponse.WithLogger(nil),
		apiresponse.WithErrorMapper(func(err error) *apiresponse.ErrorBody {
			return apiresponse.NotFound(err.Error()) // panics on nil
		}),
	)
	rec := httptest.NewRecorder()
	_ = wr.WriteError(rec, nil, nil)
	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want 500", rec.Code)
	}
}

// captureSlog redirects the default slog logger for one test.
func captureSlog(t *testing.T) *bytes.Buffer {
	t.Helper()
	var buf bytes.Buffer
	prev := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(&buf, nil)))
	t.Cleanup(func() { slog.SetDefault(prev) })
	return &buf
}

func TestDefaultLogger_IncludesRequestID(t *testing.T) {
	buf := captureSlog(t)
	wr := apiresponse.NewWriter(
		apiresponse.WithRequestIDFunc(func(r *http.Request) string { return "req_42" }),
	)
	_ = wr.WriteError(httptest.NewRecorder(), httptest.NewRequest("GET", "/x", nil), errNoRows)
	if got := buf.String(); !strings.Contains(got, "request_id=req_42") {
		t.Fatalf("log = %q, want request_id", got)
	}
}

func TestDefaultLogger_SkipsContextCanceled(t *testing.T) {
	buf := captureSlog(t)
	rec := httptest.NewRecorder()
	_ = apiresponse.WriteError(rec, context.Canceled)
	if buf.Len() != 0 {
		t.Fatalf("logged a client cancel: %q", buf.String())
	}
	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want 500", rec.Code)
	}
	_ = apiresponse.WriteError(httptest.NewRecorder(), errNoRows)
	if buf.Len() == 0 {
		t.Fatal("a real 5xx must still be logged")
	}
}

func TestCustomLogger_StillSeesContextCanceled(t *testing.T) {
	var calls []logCall
	wr := newTestWriter(&calls)
	_ = wr.WriteError(httptest.NewRecorder(), nil, context.Canceled)
	if len(calls) != 1 {
		t.Fatalf("custom logger calls = %d, want 1", len(calls))
	}
}

func TestHandler_DoesNotAppendEnvelopeAfterBody(t *testing.T) {
	var calls []logCall
	wr := newTestWriter(&calls)
	h := wr.Handler(func(w http.ResponseWriter, r *http.Request) error {
		_ = apiresponse.Write(w, http.StatusOK, apiresponse.OK("partial"))
		return errors.New("late failure")
	})
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest("GET", "/", nil))
	if strings.Contains(rec.Body.String(), "error") {
		t.Fatalf("error envelope appended: %s", rec.Body)
	}
	if rec.Code != http.StatusOK || len(calls) != 1 {
		t.Fatalf("status = %d, log calls = %d; want 200 and the error logged once", rec.Code, len(calls))
	}
}

func TestHandler_InterimStatusDoesNotCountAsWritten(t *testing.T) {
	wr := apiresponse.NewWriter(apiresponse.WithLogger(nil))
	h := wr.Handler(func(w http.ResponseWriter, r *http.Request) error {
		w.WriteHeader(http.StatusEarlyHints)
		return apiresponse.NotFound("gone")
	})
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest("GET", "/", nil))
	// The recorder keeps only the first status, so assert on the envelope: an
	// interim 103 must not make Handler think the final response already went out.
	if !strings.Contains(rec.Body.String(), `"code":"not_found"`) {
		t.Fatalf("envelope not written after an interim status: %q", rec.Body)
	}
}

func TestRecover_DoesNotAppendEnvelopeAfterBody(t *testing.T) {
	wr := apiresponse.NewWriter(apiresponse.WithLogger(nil))
	h := wr.Recover(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte("partial"))
		panic("late")
	}))
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest("GET", "/", nil))
	if rec.Body.String() != "partial" {
		t.Fatalf("body = %q, want only the partial output", rec.Body)
	}
}

type hijackRecorder struct {
	*httptest.ResponseRecorder
	hijacked bool
}

func (h *hijackRecorder) Hijack() (net.Conn, *bufio.ReadWriter, error) {
	h.hijacked = true
	return nil, nil, nil
}

func TestWrappedWriterKeepsFlushAndHijack(t *testing.T) {
	rec := &hijackRecorder{ResponseRecorder: httptest.NewRecorder()}
	h := apiresponse.Recover(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if _, ok := w.(http.Flusher); !ok {
			t.Error("wrapped writer lost http.Flusher")
		}
		hj, ok := w.(http.Hijacker)
		if !ok {
			t.Fatal("wrapped writer lost http.Hijacker")
		}
		_, _, _ = hj.Hijack()
	}))
	h.ServeHTTP(rec, httptest.NewRequest("GET", "/", nil))
	if !rec.hijacked {
		t.Fatal("Hijack was not forwarded to the underlying writer")
	}
}

func TestNotFoundAndMethodNotAllowedHandlers(t *testing.T) {
	rec := httptest.NewRecorder()
	apiresponse.NotFoundHandler().ServeHTTP(rec, httptest.NewRequest("GET", "/nope", nil))
	if rec.Code != 404 || !strings.Contains(rec.Body.String(), `"code":"not_found"`) {
		t.Fatalf("404 handler: %d %s", rec.Code, rec.Body)
	}

	rec = httptest.NewRecorder()
	apiresponse.MethodNotAllowedHandler("GET", "POST").ServeHTTP(rec, httptest.NewRequest("DELETE", "/", nil))
	if rec.Code != 405 || rec.Header().Get("Allow") != "GET, POST" ||
		!strings.Contains(rec.Body.String(), `"code":"method_not_allowed"`) {
		t.Fatalf("405 handler: %d allow=%q %s", rec.Code, rec.Header().Get("Allow"), rec.Body)
	}

	rec = httptest.NewRecorder()
	apiresponse.MethodNotAllowedHandler().ServeHTTP(rec, httptest.NewRequest("DELETE", "/", nil))
	if _, ok := rec.Header()["Allow"]; ok {
		t.Fatal("no methods given, Allow must not be set")
	}
}

func TestWrappedWriter_FlushCountsAsWrittenAndUnwraps(t *testing.T) {
	wr := apiresponse.NewWriter(apiresponse.WithLogger(nil))
	rec := httptest.NewRecorder()
	h := wr.Handler(func(w http.ResponseWriter, r *http.Request) error {
		w.(http.Flusher).Flush()
		// ResponseController reaches the underlying writer through Unwrap; the
		// recorder has no deadlines, so ErrNotSupported proves we got there.
		if err := http.NewResponseController(w).SetWriteDeadline(time.Time{}); !errors.Is(err, http.ErrNotSupported) {
			t.Errorf("SetWriteDeadline err = %v, want ErrNotSupported from the wrapped writer", err)
		}
		return errors.New("after flush")
	})
	h.ServeHTTP(rec, httptest.NewRequest("GET", "/", nil))
	if !rec.Flushed || strings.Contains(rec.Body.String(), "error") {
		t.Fatalf("flushed = %v, body = %q; want flushed with no envelope appended", rec.Flushed, rec.Body)
	}
}
