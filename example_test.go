package apiresponse_test

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"time"

	"github.com/catstackdev/apiresponse"
)

type user struct {
	ID   int    `json:"id"`
	Name string `json:"name"`
}

func Example_handler() {
	// Works the same with net/http, chi, or any http.Handler router.
	h := apiresponse.Handler(func(w http.ResponseWriter, r *http.Request) error {
		if r.URL.Query().Get("id") == "" {
			return apiresponse.NotFound("user not found")
		}
		return apiresponse.Write(w, http.StatusOK, apiresponse.OK(user{ID: 1, Name: "Ada"}))
	})

	rec := httptest.NewRecorder()
	h(rec, httptest.NewRequest("GET", "/users", nil))
	fmt.Println(rec.Code, rec.Body.String())

	rec = httptest.NewRecorder()
	h(rec, httptest.NewRequest("GET", "/users?id=1", nil))
	fmt.Println(rec.Code, rec.Body.String())
	// Output:
	// 404 {"error":{"code":"not_found","message":"user not found"}}
	// 200 {"data":{"id":1,"name":"Ada"}}
}

func ExampleList() {
	next := "cursor-2"
	resp := apiresponse.List([]user{{1, "Ada"}}, apiresponse.CursorPage(1, &next))
	rec := httptest.NewRecorder()
	_ = apiresponse.Write(rec, http.StatusOK, resp)
	fmt.Println(rec.Body.String())
	// Output:
	// {"data":[{"id":1,"name":"Ada"}],"pagination":{"limit":1,"has_more":true,"next_cursor":"cursor-2"}}
}

func ExampleValidation() {
	err := apiresponse.Validation(apiresponse.Detail{Field: "email", Message: "must be valid"})
	rec := httptest.NewRecorder()
	_ = apiresponse.WriteError(rec, nil, err)
	fmt.Println(rec.Code, rec.Body.String())
	// Output:
	// 422 {"error":{"code":"validation_failed","message":"validation failed","details":[{"field":"email","message":"must be valid"}]}}
}

func ExampleDecode() {
	h := apiresponse.Handler(func(w http.ResponseWriter, r *http.Request) error {
		in, err := apiresponse.Decode[user](w, r)
		if err != nil {
			return err
		}
		return apiresponse.Write(w, http.StatusCreated, apiresponse.OK(in))
	})

	rec := httptest.NewRecorder()
	req := httptest.NewRequest("POST", "/users", strings.NewReader(`{"id":2,"name":"Grace"}`))
	req.Header.Set("Content-Type", "application/json")
	h(rec, req)
	fmt.Println(rec.Code, rec.Body.String())

	rec = httptest.NewRecorder()
	h(rec, httptest.NewRequest("POST", "/users", strings.NewReader(`{"nope":1}`)))
	fmt.Println(rec.Code, rec.Body.String())
	// Output:
	// 201 {"data":{"id":2,"name":"Grace"}}
	// 400 {"error":{"code":"bad_request","message":"json: unknown field \"nope\""}}
}

func ExampleNewWriter() {
	errNoRows := errors.New("no rows")
	wr := apiresponse.NewWriter(
		apiresponse.WithErrorMapper(func(err error) *apiresponse.ErrorBody {
			if errors.Is(err, errNoRows) {
				return apiresponse.NotFound("user not found")
			}
			return nil
		}),
		apiresponse.WithRequestIDFunc(func(r *http.Request) string { return r.Header.Get("X-Request-ID") }),
	)

	rec := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/users/9", nil)
	req.Header.Set("X-Request-ID", "r-1")
	_ = wr.WriteError(rec, req, fmt.Errorf("load user: %w", errNoRows))
	fmt.Println(rec.Code, rec.Body.String())
	// Output:
	// 404 {"error":{"code":"not_found","message":"user not found","request_id":"r-1"}}
}

func ExampleWriter_Recover() {
	wr := apiresponse.NewWriter(apiresponse.WithLogger(nil))
	h := wr.Recover(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		panic("boom")
	}))

	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest("GET", "/", nil))
	fmt.Println(rec.Code, rec.Body.String())
	// Output:
	// 500 {"error":{"code":"internal_error","message":"internal server error"}}
}

func ExampleErrorBody_WithRetryAfter() {
	h := apiresponse.Handler(func(http.ResponseWriter, *http.Request) error {
		return apiresponse.RateLimited("slow down").WithRetryAfter(30 * time.Second)
	})

	rec := httptest.NewRecorder()
	h(rec, httptest.NewRequest("GET", "/", nil))
	fmt.Println(rec.Code, rec.Header().Get("Retry-After"), rec.Body.String())
	// Output:
	// 429 30 {"error":{"code":"rate_limited","message":"slow down"}}
}

func ExampleWithLogger() {
	wr := apiresponse.NewWriter(
		apiresponse.WithLogger(func(r *http.Request, err error, eb *apiresponse.ErrorBody) {
			path := "" // r is nil when WriteError was called without a request
			if r != nil {
				path = r.URL.Path
			}
			fmt.Println("logged:", eb.Code, path, err)
		}),
	)

	rec := httptest.NewRecorder()
	_ = wr.WriteError(rec, httptest.NewRequest("GET", "/orders", nil), errors.New("db down"))
	_ = wr.WriteError(rec, nil, errors.New("db down"))
	// Output:
	// logged: internal_error /orders db down
	// logged: internal_error  db down
}

// A Go client reads the same envelope with ErrorBody.
func Example_goClient() {
	body := `{"error":{"code":"not_found","message":"user not found"}}`

	var env struct {
		Data  json.RawMessage        `json:"data"`
		Error *apiresponse.ErrorBody `json:"error"`
	}
	if err := json.NewDecoder(strings.NewReader(body)).Decode(&env); err != nil {
		fmt.Println("not an envelope:", err)
		return
	}
	if env.Error != nil && env.Error.Code == apiresponse.CodeNotFound {
		fmt.Println("missing:", env.Error.Message)
	}
	// Output:
	// missing: user not found
}
