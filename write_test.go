package apiresponse_test

import (
	"bytes"
	"errors"
	"fmt"
	"log/slog"
	"math"
	"net/http"
	"net/http/httptest"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/catstackdev/apiresponse"
)

func TestWrite_Success(t *testing.T) {
	rec := httptest.NewRecorder()
	if err := apiresponse.Write(rec, http.StatusCreated, apiresponse.OK("hi")); err != nil {
		t.Fatal(err)
	}
	if rec.Code != 201 || rec.Header().Get("Content-Type") != "application/json; charset=utf-8" {
		t.Fatalf("code=%d ct=%q", rec.Code, rec.Header().Get("Content-Type"))
	}
	if rec.Body.String() != `{"data":"hi"}` {
		t.Fatalf("body %s", rec.Body)
	}
}

func TestWrite_NoContentHasNoBody(t *testing.T) {
	rec := httptest.NewRecorder()
	_ = apiresponse.Write(rec, http.StatusNoContent, nil)
	if rec.Code != 204 || rec.Body.Len() != 0 {
		t.Fatalf("code=%d body=%q", rec.Code, rec.Body)
	}
}

func TestWrite_MarshalFailureYields500(t *testing.T) {
	rec := httptest.NewRecorder()
	err := apiresponse.Write(rec, 200, apiresponse.OK(make(chan int)))
	if err == nil || rec.Code != 500 || !strings.Contains(rec.Body.String(), "internal_error") {
		t.Fatalf("err=%v code=%d body=%s", err, rec.Code, rec.Body)
	}
}

func TestWriteError(t *testing.T) {
	rec := httptest.NewRecorder()
	_ = apiresponse.WriteError(rec, nil, apiresponse.NotFound("user not found"))
	want := `{"error":{"code":"not_found","message":"user not found"}}`
	if rec.Code != 404 || rec.Body.String() != want {
		t.Fatalf("code=%d body=%s", rec.Code, rec.Body)
	}
}

func TestHandler_MapsReturnedErrors(t *testing.T) {
	h := apiresponse.Handler(func(w http.ResponseWriter, r *http.Request) error {
		return errors.New("boom")
	})
	rec := httptest.NewRecorder()
	h(rec, httptest.NewRequest("GET", "/", nil))
	if rec.Code != 500 || strings.Contains(rec.Body.String(), "boom") {
		t.Fatalf("code=%d body=%s", rec.Code, rec.Body)
	}
}

func TestWrite_DoesNotEscapeHTML(t *testing.T) {
	rec := httptest.NewRecorder()
	_ = apiresponse.Write(rec, 200, apiresponse.OK("a<b>&c"))
	if rec.Body.String() != `{"data":"a<b>&c"}` {
		t.Fatalf("body %s", rec.Body)
	}
}

type failingWriter struct{ header http.Header }

func (f *failingWriter) Header() http.Header       { return f.header }
func (f *failingWriter) WriteHeader(int)           {}
func (f *failingWriter) Write([]byte) (int, error) { return 0, errors.New("closed") }

func TestWrite_ReturnsWriteError(t *testing.T) {
	err := apiresponse.Write(&failingWriter{header: http.Header{}}, 200, apiresponse.OK(1))
	if err == nil {
		t.Fatal("expected write error to be returned")
	}
}

func TestWrite_NotModifiedHasNoBody(t *testing.T) {
	rec := httptest.NewRecorder()
	_ = apiresponse.Write(rec, http.StatusNotModified, apiresponse.OK(1))
	if rec.Code != 304 || rec.Body.Len() != 0 {
		t.Fatalf("code=%d body=%q", rec.Code, rec.Body)
	}
}

func TestWriteError_NilErrorIs500(t *testing.T) {
	rec := httptest.NewRecorder()
	_ = apiresponse.NewWriter(apiresponse.WithLogger(nil)).WriteError(rec, nil, nil)
	if rec.Code != 500 {
		t.Fatalf("code=%d", rec.Code)
	}
}

func TestHandler_SuccessWritesNothingExtra(t *testing.T) {
	h := apiresponse.Handler(func(w http.ResponseWriter, r *http.Request) error {
		return apiresponse.Write(w, 200, apiresponse.OK("ok"))
	})
	rec := httptest.NewRecorder()
	h(rec, httptest.NewRequest("GET", "/", nil))
	if rec.Code != 200 || rec.Body.String() != `{"data":"ok"}` {
		t.Fatalf("code=%d body=%s", rec.Code, rec.Body)
	}
}

func TestWrite_InvalidStatusYields500(t *testing.T) {
	for _, status := range []int{0, 99, 1000} {
		rec := httptest.NewRecorder()
		if err := apiresponse.Write(rec, status, apiresponse.OK("x")); err == nil {
			t.Fatalf("status %d: want an error", status)
		}
		if rec.Code != http.StatusInternalServerError {
			t.Fatalf("status %d: got %d, want 500", status, rec.Code)
		}
		if !strings.Contains(rec.Body.String(), "internal_error") {
			t.Fatalf("status %d: body = %q, want the internal error envelope", status, rec.Body.String())
		}
	}
}

func TestWrite_SetsNosniff(t *testing.T) {
	rec := httptest.NewRecorder()
	_ = apiresponse.Write(rec, http.StatusOK, apiresponse.OK(1))
	if got := rec.Header().Get("X-Content-Type-Options"); got != "nosniff" {
		t.Fatalf("X-Content-Type-Options = %q", got)
	}
}

func TestWriteError_LogsRequestPath(t *testing.T) {
	var buf bytes.Buffer
	old := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(&buf, nil)))
	t.Cleanup(func() { slog.SetDefault(old) })

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/boom", nil)
	_ = apiresponse.WriteError(rec, req, errors.New("db down"))
	if rec.Code != 500 || !strings.Contains(buf.String(), "path=/boom") {
		t.Fatalf("code=%d log=%q", rec.Code, buf.String())
	}
}

func TestWriteError_RetryAfter(t *testing.T) {
	tests := []struct {
		name string
		d    time.Duration
		want string
	}{
		{"whole seconds", 30 * time.Second, "30"},
		{"rounded up", 1500 * time.Millisecond, "2"},
		{"huge does not overflow", math.MaxInt64, "9223372037"},
		{"zero sends nothing", 0, ""},
		{"negative sends nothing", -time.Second, ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rec := httptest.NewRecorder()
			_ = apiresponse.WriteError(rec, nil, apiresponse.RateLimited("slow down").WithRetryAfter(tt.d))
			if got := rec.Header().Get("Retry-After"); got != tt.want {
				t.Fatalf("Retry-After = %q, want %q", got, tt.want)
			}
			if strings.Contains(rec.Body.String(), "retry") {
				t.Fatalf("Retry-After leaked into body: %s", rec.Body)
			}
		})
	}
}

func TestWithRetryAfter_DoesNotMutateSentinel(t *testing.T) {
	base := apiresponse.RateLimited("slow down")
	_ = base.WithRetryAfter(time.Minute)
	rec := httptest.NewRecorder()
	_ = apiresponse.WriteError(rec, nil, base)
	if got := rec.Header().Get("Retry-After"); got != "" {
		t.Fatalf("sentinel was mutated: Retry-After = %q", got)
	}
}

func TestHandler_DropsStaleEntityHeadersOnError(t *testing.T) {
	h := apiresponse.Handler(func(w http.ResponseWriter, _ *http.Request) error {
		w.Header().Set("Content-Length", "999")
		w.Header().Set("Content-Disposition", `attachment; filename="r.csv"`)
		w.Header().Set("X-Keep", "yes")
		return apiresponse.NotFound("gone")
	})
	rec := httptest.NewRecorder()
	h(rec, httptest.NewRequest(http.MethodGet, "/", nil))
	hdr := rec.Header()
	if hdr.Get("Content-Length") != "" || hdr.Get("Content-Disposition") != "" || hdr.Get("X-Keep") != "yes" {
		t.Fatalf("headers = %v", hdr)
	}
}

func TestRecover_DropsStaleEntityHeaders(t *testing.T) {
	h := apiresponse.Recover(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Length", "999")
		panic("boom")
	}))
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/", nil))
	if rec.Code != 500 || rec.Header().Get("Content-Length") != "" {
		t.Fatalf("code=%d headers=%v", rec.Code, rec.Header())
	}
}

func TestHandler_LateClientGoneErrorIsNotLogged(t *testing.T) {
	for _, err := range []error{syscall.EPIPE, fmt.Errorf("write: %w", syscall.ECONNRESET)} {
		buf := captureSlog(t)
		h := apiresponse.Handler(func(w http.ResponseWriter, _ *http.Request) error {
			_, _ = w.Write([]byte("partial"))
			return err
		})
		h(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/", nil))
		if buf.Len() != 0 {
			t.Fatalf("logged a client disconnect (%v): %q", err, buf.String())
		}
	}
}

func TestWriteError_UpstreamConnectionResetIsStillLogged(t *testing.T) {
	buf := captureSlog(t)
	rec := httptest.NewRecorder()
	_ = apiresponse.WriteError(rec, nil, fmt.Errorf("query: %w", syscall.ECONNRESET))
	if rec.Code != 500 || buf.Len() == 0 {
		t.Fatalf("code=%d log=%q: a DB connection reset must be logged", rec.Code, buf.String())
	}
}
