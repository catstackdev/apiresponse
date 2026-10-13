package apiresponse_test

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/catstackdev/apiresponse"
)

type createUser struct {
	Name string `json:"name"`
	Age  int    `json:"age"`
}

func decodeReq(t *testing.T, contentType, body string, limit int64) (createUser, error) {
	t.Helper()
	r := httptest.NewRequest(http.MethodPost, "/", strings.NewReader(body))
	if contentType != "" {
		r.Header.Set("Content-Type", contentType)
	}
	w := httptest.NewRecorder()
	if limit > 0 {
		return apiresponse.DecodeLimit[createUser](w, r, limit)
	}
	return apiresponse.Decode[createUser](w, r)
}

func TestDecode(t *testing.T) {
	tests := []struct {
		name, contentType, body string
		limit                   int64
		wantCode                string // empty means success
		wantStatus              int
	}{
		{"ok", "application/json", `{"name":"Ada","age":36}`, 0, "", 0},
		{"ok with charset", "application/json; charset=utf-8", `{"name":"Ada"}`, 0, "", 0},
		{"ok without content type", "", `{"name":"Ada"}`, 0, "", 0},
		{"wrong content type", "text/plain", `{}`, 0, apiresponse.CodeUnsupportedMediaType, 415},
		{"bad content type", "///", `{}`, 0, apiresponse.CodeUnsupportedMediaType, 415},
		{"empty body", "application/json", ``, 0, apiresponse.CodeBadRequest, 400},
		{"malformed", "application/json", `{"name":`, 0, apiresponse.CodeBadRequest, 400},
		{"syntax error", "application/json", `{name}`, 0, apiresponse.CodeBadRequest, 400},
		{"wrong type", "application/json", `{"age":"old"}`, 0, apiresponse.CodeBadRequest, 400},
		{"unknown field", "application/json", `{"nope":1}`, 0, apiresponse.CodeBadRequest, 400},
		{"two values", "application/json", `{} {}`, 0, apiresponse.CodeBadRequest, 400},
		{"trailing garbage", "application/json", `{} x`, 0, apiresponse.CodeBadRequest, 400},
		{"too large", "application/json", `{"name":"` + strings.Repeat("a", 100) + `"}`, 16, apiresponse.CodePayloadTooLarge, 413},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			_, err := decodeReq(t, tc.contentType, tc.body, tc.limit)
			if tc.wantCode == "" {
				if err != nil {
					t.Fatalf("unexpected error: %v", err)
				}
				return
			}
			if !apiresponse.IsCode(err, tc.wantCode) {
				t.Fatalf("err = %v, want code %s", err, tc.wantCode)
			}
			if got := apiresponse.FromError(err).HTTPStatus(); got != tc.wantStatus {
				t.Fatalf("status = %d, want %d", got, tc.wantStatus)
			}
		})
	}
}

func TestDecode_ReturnsValue(t *testing.T) {
	got, err := decodeReq(t, "application/json", `{"name":"Ada","age":36}`, 0)
	if err != nil || got != (createUser{Name: "Ada", Age: 36}) {
		t.Fatalf("got %+v, err %v", got, err)
	}
}
