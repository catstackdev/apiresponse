---
name: use-apiresponse
description:
  Write Go HTTP handlers, error handling, pagination and middleware using the
  github.com/catstackdev/apiresponse library instead of ad-hoc JSON responses.
  Use when adding or changing an API endpoint, returning an error response,
  paginating a list, or wiring panic recovery in a Go service that imports
  apiresponse.
---

# Using apiresponse

Library: `github.com/catstackdev/apiresponse` (stdlib only, Go 1.22+). Read the
project README for the full API; this skill is the short version of how to use
it correctly.

## Rules

- Never hand-write response JSON or `map[string]any` envelopes. Use the library.
- The HTTP status code carries success/failure. Do not add a `status` field.
- Success: `apiresponse.OK(v)`. Lists: `apiresponse.List(items, page)`.
- Errors: return an `*ErrorBody` (`NotFound`, `Validation`, `Conflict`, ...).
  Never write `err.Error()` of an unknown error to the client; unknown errors
  already become a generic 500.
- Handlers return `error`; wrap with `Handler` (or a `Writer`'s `.Handler`).

## net/http and chi

```go
mux.HandleFunc("GET /users/{id}", apiresponse.Handler(func(w http.ResponseWriter, r *http.Request) error {
    u, err := svc.Find(r.Context(), r.PathValue("id"))
    if err != nil {
        return err
    }
    return apiresponse.Write(w, http.StatusOK, apiresponse.OK(u))
}))
http.ListenAndServe(":8080", apiresponse.Recover(mux))
```

chi: same handlers; `r.Use(apiresponse.Recover)`.

## Gin / Echo / Fiber

No adapter. Success: `c.JSON(200, apiresponse.OK(data))`. Errors:

```go
e := apiresponse.FromError(err)
c.JSON(e.HTTPStatus(), apiresponse.ErrorResponse{Error: e})
```

## Pagination

- Cursor:
  `apiresponse.List(items, apiresponse.CursorPage(limit, nextCursorPtr))` (nil
  next cursor = last page).
- Offset: `apiresponse.List(items, apiresponse.OffsetPage(page, limit, total))`
  (page is 1-based).

## Mapping domain errors and logging

Build one `Writer` at startup and reuse it; do not create one per request.

```go
w := apiresponse.NewWriter(
    apiresponse.WithErrorMapper(func(err error) *apiresponse.ErrorBody {
        if errors.Is(err, ErrNotFound) { return apiresponse.NotFound("not found") }
        return nil // not mine
    }),
    apiresponse.WithLogger(myLogger), // only 5xx are logged; nil disables
)
```

Resolution order: existing `*ErrorBody` in the chain, then mappers, then a
generic 500. Prefer a mapper over scattering `NotFound(...)` conversions across
the service layer.

Useful options: `WithErrorMapper(apiresponse.ContextErrors)` maps
`context.DeadlineExceeded` to a 504; `WithRequestIDFunc(fn)` fills `request_id`
into every error body from the request (header or context).

Extra constructors: `MethodNotAllowed`, `UnsupportedMediaType`,
`PayloadTooLarge`, `ServiceUnavailable`, `DeadlineExceeded`. Compare errors with
`errors.Is(err, sentinel)` (matches by code, survives `With*` copies) or
`apiresponse.IsCode(err, code)`.

## Pitfalls

- `With*` methods return copies; use the returned value. `e.WithRequestID(id)`
  alone does nothing.
- Sentinel errors (`var ErrX = apiresponse.NotFound(...)`) are safe to share.
- `WriteError(w, err)` passes a nil request to the logger; use
  `Writer.WriteError(w, r, err)` or `Handler` when the logger needs the path.
- `Recover` cannot change the status if the handler already started writing.
- Validation errors are 422 with `Details`; use `BadRequest` for malformed input
  that is not field-level (e.g. unparsable JSON).
