package apiresponse_test

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

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
	_ = apiresponse.WriteError(rec, apiresponse.NotFound("user not found"))
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
