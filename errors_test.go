package apiresponse_test

import (
	"errors"
	"fmt"
	"net/http"
	"testing"

	"github.com/catstackdev/apiresponse"
)

func TestConstructors_StatusAndCode(t *testing.T) {
	cases := []struct {
		err    *apiresponse.ErrorBody
		status int
		code   string
	}{
		{apiresponse.BadRequest("x"), 400, apiresponse.CodeBadRequest},
		{apiresponse.Unauthorized("x"), 401, apiresponse.CodeUnauthorized},
		{apiresponse.Forbidden("x"), 403, apiresponse.CodeForbidden},
		{apiresponse.NotFound("x"), 404, apiresponse.CodeNotFound},
		{apiresponse.Conflict("x"), 409, apiresponse.CodeConflict},
		{apiresponse.Validation(), 422, apiresponse.CodeValidation},
		{apiresponse.RateLimited("x"), 429, apiresponse.CodeRateLimited},
		{apiresponse.Internal("x"), 500, apiresponse.CodeInternal},
	}
	for _, c := range cases {
		if c.err.HTTPStatus() != c.status || c.err.Code != c.code {
			t.Errorf("%s: got %d/%s", c.code, c.err.HTTPStatus(), c.err.Code)
		}
	}
}

func TestWith_DoesNotMutateReceiver(t *testing.T) {
	base := apiresponse.NotFound("user not found")
	_ = base.WithRequestID("r1").WithDetails(apiresponse.Detail{Message: "m"}).WithStatus(410)
	if base.RequestID != "" || base.Details != nil || base.HTTPStatus() != http.StatusNotFound {
		t.Fatalf("receiver mutated: %+v", base)
	}
}

func TestFromError(t *testing.T) {
	t.Run("wrapped ErrorBody is found", func(t *testing.T) {
		wrapped := fmt.Errorf("ctx: %w", apiresponse.NotFound("x"))
		if got := apiresponse.FromError(wrapped); got.Code != apiresponse.CodeNotFound {
			t.Fatalf("got %s", got.Code)
		}
	})
	t.Run("plain error becomes generic 500 keeping cause", func(t *testing.T) {
		cause := errors.New("db password leaked")
		got := apiresponse.FromError(cause)
		if got.HTTPStatus() != 500 || got.Message == cause.Error() {
			t.Fatalf("leaked or wrong status: %+v", got)
		}
		if !errors.Is(got, cause) {
			t.Fatal("cause not unwrappable")
		}
	})
}

func TestErrorBody_ErrorString(t *testing.T) {
	e := apiresponse.NotFound("gone")
	if e.Error() != "not_found: gone" {
		t.Fatalf("got %q", e.Error())
	}
	if got := e.WithCause(errors.New("db")).Error(); got != "not_found: gone: db" {
		t.Fatalf("got %q", got)
	}
}

func TestErrorBody_ZeroValueStatusIs500(t *testing.T) {
	if got := (&apiresponse.ErrorBody{Code: "x"}).HTTPStatus(); got != 500 {
		t.Fatalf("got %d", got)
	}
}

func TestErrorBody_WithStatusOverrides(t *testing.T) {
	if got := apiresponse.NotFound("x").WithStatus(410).HTTPStatus(); got != 410 {
		t.Fatalf("got %d", got)
	}
}

func TestErrorBody_JSONHidesInternals(t *testing.T) {
	e := apiresponse.Validation(apiresponse.Detail{Field: "email", Message: "bad"}).
		WithRequestID("r1").WithCause(errors.New("secret"))
	got := marshal(t, apiresponse.ErrorResponse{Error: e})
	want := `{"error":{"code":"validation_failed","message":"validation failed","details":[{"field":"email","message":"bad"}],"request_id":"r1"}}`
	if got != want {
		t.Fatalf("got %s", got)
	}
}

func TestDetail_FieldOmittedWhenEmpty(t *testing.T) {
	if got := marshal(t, apiresponse.Detail{Message: "m"}); got != `{"message":"m"}` {
		t.Fatalf("got %s", got)
	}
}

func TestFromError_Nil(t *testing.T) {
	if got := apiresponse.FromError(nil); got.HTTPStatus() != 500 {
		t.Fatalf("got %d", got.HTTPStatus())
	}
}
