package apiresponse_test

import (
	"fmt"
	"net/http"
	"net/http/httptest"

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
	_ = apiresponse.WriteError(rec, err)
	fmt.Println(rec.Code, rec.Body.String())
	// Output:
	// 422 {"error":{"code":"validation_failed","message":"validation failed","details":[{"field":"email","message":"must be valid"}]}}
}
