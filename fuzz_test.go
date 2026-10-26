package apiresponse_test

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/catstackdev/apiresponse"
)

func FuzzDecode(f *testing.F) {
	for _, seed := range []string{``, `null`, `{}`, `{"name":"Ada","age":1}`, `{"name":1}`, `{"x":1}`, `{} {}`, `[`, "\xff\xfe"} {
		f.Add(seed)
	}
	f.Fuzz(func(t *testing.T, body string) {
		r := httptest.NewRequest(http.MethodPost, "/", strings.NewReader(body))
		_, err := apiresponse.Decode[createUser](httptest.NewRecorder(), r)
		if err == nil {
			return
		}
		eb, ok := err.(*apiresponse.ErrorBody)
		if !ok || eb == nil {
			t.Fatalf("error %T is not *ErrorBody", err)
		}
		if s := eb.HTTPStatus(); s < 400 || s > 499 {
			t.Fatalf("status %d outside 4xx for client input %q", s, body)
		}
	})
}

func FuzzOffsetPage(f *testing.F) {
	f.Add(1, 10, int64(100))
	f.Add(-1, -1, int64(-1))
	f.Add(1<<62, 1, int64(1<<62))
	f.Fuzz(func(t *testing.T, page, limit int, total int64) {
		p := apiresponse.OffsetPage(page, limit, total)
		if *p.Page < 1 || p.Limit < 0 || *p.Total < 0 {
			t.Fatalf("unclamped result %+v", p)
		}
		if limit <= 0 && p.HasMore {
			t.Fatal("HasMore with no limit")
		}
	})
}
