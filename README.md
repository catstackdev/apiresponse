# apiresponse

Framework-agnostic JSON response envelope for Go HTTP APIs. Standard library
only. Needs Go 1.22+.

```bash
go get github.com/catstackdev/apiresponse@v0.1.0
```

Before the repo is pushed (or while it is private), use a local checkout from the
consuming project instead:

```bash
go mod edit -replace github.com/catstackdev/apiresponse=../apiresponse
# private remote: export GOPRIVATE=github.com/catstackdev/*
```

## Response shapes

The HTTP status code says success or failure; the body never repeats it.

```jsonc
// success
{ "data": { "id": 1, "name": "Ada" } }

// list (cursor)
{ "data": [], "pagination": { "limit": 20, "has_more": true, "next_cursor": "abc" } }

// list (offset)
{ "data": [], "pagination": { "limit": 10, "has_more": false, "page": 1, "total": 0 } }

// error
{ "error": { "code": "validation_failed", "message": "validation failed",
             "details": [{ "field": "email", "message": "must be valid" }],
             "request_id": "r-123" } }
```

## Quick start (net/http)

```go
mux := http.NewServeMux()

mux.HandleFunc("GET /users/{id}", apiresponse.Handler(func(w http.ResponseWriter, r *http.Request) error {
    user, err := store.Find(r.PathValue("id"))
    if err != nil {
        return err // becomes an error envelope
    }
    return apiresponse.Write(w, http.StatusOK, apiresponse.OK(user))
}))

http.ListenAndServe(":8080", apiresponse.Recover(mux))
```

### Building blocks

| Need            | Use                                                                                                                                                 |
| --------------- | --------------------------------------------------------------------------------------------------------------------------------------------------- |
| Single item     | `OK(v)`                                                                                                                                             |
| List            | `List(items, CursorPage(limit, next))` or `OffsetPage(page, limit, total)`                                                                          |
| No content      | `Write(w, 204, nil)`                                                                                                                                |
| Errors          | `BadRequest`, `Unauthorized`, `Forbidden`, `NotFound`, `Conflict`, `Validation(details...)`, `RateLimited`, `Internal`, or `New(status, code, msg)` |
| Enrich an error | `.WithDetails(...)`, `.WithRequestID(id)`, `.WithCause(err)`, `.WithStatus(n)` (each returns a copy)                                                |
| Any error       | `WriteError(w, err)`                                                                                                                                |

`*ErrorBody` implements `error`, so handlers can return it directly. Code
constants (`CodeNotFound`, `CodeValidation`, ...) are what clients should switch
on, not the message.

## Frameworks

| Framework | Usage                                                |
| --------- | ---------------------------------------------------- |
| net/http  | `Write`, `WriteError`, `Handler`, `Recover` as above |
| chi       | same as net/http; `r.Use(apiresponse.Recover)`       |
| Gin       | `c.JSON(status, apiresponse.OK(data))`               |
| Echo      | `c.JSON(status, apiresponse.OK(data))`               |
| Fiber     | `c.Status(status).JSON(apiresponse.OK(data))`        |

For errors in Gin/Echo/Fiber:

```go
e := apiresponse.FromError(err)
c.JSON(e.HTTPStatus(), apiresponse.ErrorResponse{Error: e})
```

chi, Gin, Echo and Fiber are not dependencies of this module and are not
exercised by its tests (net/http is; chi works because it is an `http.Handler`
router).

## Custom error mapping, logging, panics

Package-level functions use a default `Writer`. Build your own to map domain
errors and control logging:

```go
w := apiresponse.NewWriter(
    apiresponse.WithErrorMapper(func(err error) *apiresponse.ErrorBody {
        if errors.Is(err, sql.ErrNoRows) {
            return apiresponse.NotFound("not found")
        }
        return nil // not mine, try the next mapper
    }),
    apiresponse.WithLogger(func(r *http.Request, err error, eb *apiresponse.ErrorBody) {
        slog.Error("server error", "path", r.URL.Path, "err", err)
    }),
)

mux.Handle("GET /users/{id}", w.Handler(getUser))
srv := http.Server{Handler: w.Recover(mux)}
```

Error resolution order: an `*ErrorBody` already in the error chain, then mappers
in registration order, then a generic 500.

- **Logging:** only 5xx responses are logged (default: `slog`). The logger gets
  the original error, so the real cause is visible. `WithLogger(nil)` disables
  it. `r` is nil when `WriteError` is called without a request.
- **Panics:** `Recover` returns the standard 500 envelope and logs the panic
  with its stack. It re-panics `http.ErrAbortHandler`. If the handler already
  started writing, the status cannot be changed.

## Behavior notes

- Unknown errors never leak: the client sees `internal server error`.
- `Data` is a pointer, so `0`, `""` and `false` are still serialized; a nil
  slice in `List` becomes `[]`.
- JSON is written without HTML escaping.
- `Write` marshals before sending headers, so an encode failure is a clean 500.
  1xx, 204 and 304 responses carry no body.
- `With*` methods copy, so package-level sentinel errors are safe:
  `var ErrUserNotFound = apiresponse.NotFound("user not found")`.

## Claude Code skill

This repo ships a project skill at `.claude/skills/use-apiresponse/SKILL.md`. It
teaches Claude Code to use this library (envelope, errors, pagination, mappers,
pitfalls) instead of writing ad-hoc JSON responses.

- **In this repo:** it loads automatically; nothing to do.
- **In a project that uses apiresponse:** copy the folder once.

  ```bash
  mkdir -p .claude/skills
  cp -r ~/work/personal-lib/apiresponse/.claude/skills/use-apiresponse .claude/skills/
  ```

  Then ask things like "add a GET /orders endpoint with cursor pagination" and
  Claude will pick the skill up from its description. Re-copy after changing the
  library API so the skill does not go stale.

## Development

```bash
go vet ./...
go test -race -cover ./...
```
