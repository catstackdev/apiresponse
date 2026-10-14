package apiresponse_test

import (
	"context"
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

func TestNewConstructors_StatusAndCode(t *testing.T) {
	cases := []struct {
		err    *apiresponse.ErrorBody
		status int
		code   string
	}{
		{apiresponse.MethodNotAllowed("x"), 405, apiresponse.CodeMethodNotAllowed},
		{apiresponse.UnsupportedMediaType("x"), 415, apiresponse.CodeUnsupportedMediaType},
		{apiresponse.PayloadTooLarge("x"), 413, apiresponse.CodePayloadTooLarge},
		{apiresponse.ServiceUnavailable("x"), 503, apiresponse.CodeServiceUnavailable},
		{apiresponse.DeadlineExceeded("x"), 504, apiresponse.CodeDeadlineExceeded},
	}
	for _, c := range cases {
		if c.err.HTTPStatus() != c.status || c.err.Code != c.code {
			t.Errorf("%s: got %d/%s", c.code, c.err.HTTPStatus(), c.err.Code)
		}
	}
}

var errSentinel = apiresponse.NotFound("user not found")

func TestIs_SurvivesWithCopies(t *testing.T) {
	copied := fmt.Errorf("ctx: %w", errSentinel.WithRequestID("r1").WithCause(errors.New("db")))
	if !errors.Is(copied, errSentinel) {
		t.Fatal("errors.Is must match sentinel after copies")
	}
	if errors.Is(copied, apiresponse.Conflict("x")) {
		t.Fatal("different code must not match")
	}
	if errors.Is(errors.New("plain"), errSentinel) {
		t.Fatal("plain error must not match")
	}
}

func TestIsCode(t *testing.T) {
	err := fmt.Errorf("wrap: %w", apiresponse.Conflict("dup"))
	if !apiresponse.IsCode(err, apiresponse.CodeConflict) || apiresponse.IsCode(err, apiresponse.CodeNotFound) {
		t.Fatal("IsCode mismatch")
	}
	if apiresponse.IsCode(nil, apiresponse.CodeConflict) || apiresponse.IsCode(errors.New("x"), apiresponse.CodeConflict) {
		t.Fatal("non-ErrorBody must not match")
	}
}

func TestContextErrors(t *testing.T) {
	got := apiresponse.ContextErrors(fmt.Errorf("query: %w", context.DeadlineExceeded))
	if got == nil || got.HTTPStatus() != 504 {
		t.Fatalf("deadline: %+v", got)
	}
	if apiresponse.ContextErrors(context.Canceled) != nil || apiresponse.ContextErrors(errors.New("x")) != nil {
		t.Fatal("only DeadlineExceeded should map")
	}
}

func TestFromError_TypedNilBodyBecomes500(t *testing.T) {
	var nilBody *apiresponse.ErrorBody
	var err error = nilBody
	if got := apiresponse.FromError(err).HTTPStatus(); got != 500 {
		t.Fatalf("status = %d, want 500", got)
	}
	if apiresponse.IsCode(err, "INTERNAL") {
		t.Fatal("typed nil must not match a code")
	}
}

func TestIs_DistinguishesSentinelsSharingACode(t *testing.T) {
	errUser := apiresponse.NotFound("user not found")
	errProduct := apiresponse.NotFound("product not found")
	if errors.Is(errProduct, errUser) {
		t.Fatal("sentinels with different messages must not match")
	}
	if !apiresponse.IsCode(errProduct, apiresponse.CodeNotFound) {
		t.Fatal("IsCode must still match on code alone")
	}
}

func TestDetails_AreCopied(t *testing.T) {
	in := []apiresponse.Detail{{Field: "a", Message: "bad"}}
	v := apiresponse.Validation(in...)
	w := apiresponse.BadRequest("x").WithDetails(in...)
	in[0].Field = "mutated"
	if v.Details[0].Field != "a" || w.Details[0].Field != "a" {
		t.Fatal("caller mutation leaked into the error")
	}
}

func TestWith_DoesNotShareDetails(t *testing.T) {
	base := apiresponse.Validation(apiresponse.Detail{Field: "a", Message: "bad"})
	for name, c := range map[string]*apiresponse.ErrorBody{
		"WithCause":     base.WithCause(errors.New("x")),
		"WithRequestID": base.WithRequestID("id"),
		"WithStatus":    base.WithStatus(400),
	} {
		c.Details[0].Message = "mutated"
		if base.Details[0].Message != "bad" {
			t.Errorf("%s: mutating the copy changed the original", name)
		}
	}
}

func TestWith_PreservesMessage(t *testing.T) {
	base := apiresponse.NotFound("user not found")
	for name, c := range map[string]*apiresponse.ErrorBody{
		"WithCause":     base.WithCause(errors.New("x")),
		"WithDetails":   base.WithDetails(apiresponse.Detail{Field: "f", Message: "m"}),
		"WithRequestID": base.WithRequestID("id"),
		"WithStatus":    base.WithStatus(410),
	} {
		if c.Message != base.Message || !errors.Is(c, base) {
			t.Errorf("%s: Message changed or errors.Is(copy, base) is false", name)
		}
	}
}

func TestErrorBody_NilReceiverIsSafe(t *testing.T) {
	var e *apiresponse.ErrorBody
	if e.Error() != "<nil>" || e.HTTPStatus() != 500 || e.Unwrap() != nil || e.Is(apiresponse.NotFound("x")) {
		t.Fatal("nil receiver methods must not panic or report a match")
	}
	for name, c := range map[string]*apiresponse.ErrorBody{
		"WithCause":     e.WithCause(errors.New("x")),
		"WithDetails":   e.WithDetails(),
		"WithRequestID": e.WithRequestID("id"),
		"WithStatus":    e.WithStatus(400),
	} {
		if c == nil || c.Code != apiresponse.CodeInternal {
			t.Errorf("%s on nil: got %+v, want a generic internal error", name, c)
		}
	}
}
