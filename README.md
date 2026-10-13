# apiresponse

Framework-agnostic JSON response envelope for Go HTTP APIs. Standard library
only. Needs Go 1.22+.

```bash
go get github.com/catstackdev/apiresponse@v0.1.0
```

Before the repo is pushed (or while it is private), use a local checkout from
the consuming project instead:

```bash
go mod edit -replace github.com/catstackdev/apiresponse=../apiresponse
# private remote: export GOPRIVATE=github.com/catstackdev/*
```

## Response shapes

The HTTP status code says success or failure; the body never repeats it.

```jsonc
// Success
{
  "data": {
    "id": 1,
    "name": "Ada"
  }
}

// List (cursor pagination)
{
  "data": [],
  "pagination": {
    "limit": 20,
    "has_more": true,
    "next_cursor": "abc"
  }
}

// List (offset pagination)
{
  "data": [],
  "pagination": {
    "limit": 10,
    "has_more": false,
    "page": 1,
    "total": 0
  }
}

// Error
{
  "error": {
    "code": "validation_failed",
    "message": "validation failed",
    "details": [
      {
        "field": "email",
        "message": "must be valid"
      }
    ],
    "request_id": "r-123"
  }
}
```

## For API consumers (frontend, mobile, other services)

Works the same from TypeScript, Go, or anything that reads JSON.

**How to read a response**

1. Look at the HTTP status first. 2xx/3xx is success, 4xx/5xx is failure.
2. On success, read `data` (and `pagination` for lists). On failure, read
   `error`. The two never appear together, and a failure never has `data`.
3. Branch on `error.code`, never on `error.message`. Codes are stable; messages
   are for humans and may change or be translated.

**Error codes**

| `code`                   | HTTP | Meaning                         | What the client should do                  |
| ------------------------ | ---- | ------------------------------- | ------------------------------------------ |
| `bad_request`            | 400  | Malformed request               | Fix the request; do not retry as is        |
| `unauthorized`           | 401  | Missing or invalid credentials  | Log in or refresh the token, then retry    |
| `forbidden`              | 403  | Authenticated but not allowed   | Show "no permission"; do not retry         |
| `not_found`              | 404  | Resource does not exist         | Show not-found state                       |
| `method_not_allowed`     | 405  | Wrong HTTP method               | Client bug; fix the call                   |
| `conflict`               | 409  | State conflict (e.g. duplicate) | Reload and let the user resolve            |
| `payload_too_large`      | 413  | Body over the size limit        | Send less data                             |
| `unsupported_media_type` | 415  | Wrong `Content-Type`            | Send `application/json`                    |
| `validation_failed`      | 422  | Field errors in `details`       | Show each `details[].message` at its field |
| `rate_limited`           | 429  | Too many requests               | Back off, then retry                       |
| `internal_error`         | 500  | Server bug                      | Show generic error; retry is optional      |
| `service_unavailable`    | 503  | Temporarily down                | Retry with backoff                         |
| `deadline_exceeded`      | 504  | Server timed out                | Retry with backoff                         |

Your own services can add codes with `New(status, "my_code", msg)`. Document
them for your clients alongside this table.

**Things worth knowing**

- `details` appears only on `validation_failed`. `details[].field` may be empty
  when the problem is not tied to one field.
- `request_id`, when present, is what to quote in a bug report so the server
  logs can be found. Show it in "something went wrong" screens.
- 5xx messages are deliberately generic (`internal server error`). Internal
  causes are never sent to the client.
- 204 and 304 responses have no body. Do not call `.json()` on them.
- `data` can legitimately be `0`, `""` or `false`. Check for `null`/missing, not
  falsiness. Lists are always `[]`, never `null`.
- Cursor pagination: pass `next_cursor` back unchanged and stop when
  `has_more` is `false`. Treat the cursor as opaque. Offset pagination returns
  `page` and `total`.
- Not every response comes from this library. A proxy, gateway or router can
  return plain text or HTML (for example a 502 from a load balancer). Check
  `Content-Type: application/json` before parsing, and fall back to the status
  code.
- Field names are `snake_case` (`has_more`, `next_cursor`, `request_id`).

**TypeScript types**

```ts
type Pagination = {
  limit: number;
  has_more: boolean;
  next_cursor?: string; // cursor style
  page?: number; // offset style
  total?: number; // offset style
};

type Success<T> = { data: T; pagination?: Pagination };

type ApiError = {
  code: string; // switch on this
  message: string;
  details?: { field?: string; message: string }[];
  request_id?: string;
};

type Failure = { error: ApiError };
```

**Go client**

```go
var env struct {
    Data  json.RawMessage        `json:"data"`
    Error *apiresponse.ErrorBody `json:"error"`
}
if err := json.NewDecoder(resp.Body).Decode(&env); err != nil { /* not an envelope */ }
if env.Error != nil && env.Error.Code == apiresponse.CodeNotFound { /* ... */ }
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

| Need            | Use                                                                                                                                                                                                                                                                |
| --------------- | ------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------ |
| Single item     | `OK(v)`                                                                                                                                                                                                                                                            |
| List            | `List(items, CursorPage(limit, next))` or `OffsetPage(page, limit, total)`                                                                                                                                                                                         |
| No content      | `Write(w, 204, nil)`                                                                                                                                                                                                                                               |
| Errors          | `BadRequest`, `Unauthorized`, `Forbidden`, `NotFound`, `Conflict`, `Validation(details...)`, `RateLimited`, `Internal`, `MethodNotAllowed`, `UnsupportedMediaType`, `PayloadTooLarge`, `ServiceUnavailable`, `DeadlineExceeded` (504), or `New(status, code, msg)` |
| Enrich an error | `.WithDetails(...)`, `.WithRequestID(id)`, `.WithCause(err)`, `.WithStatus(n)` (each returns a copy)                                                                                                                                                               |
| Any error       | `WriteError(w, err)`                                                                                                                                                                                                                                               |

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

Optional writer options:

```go
apiresponse.WithErrorMapper(apiresponse.ContextErrors) // context.DeadlineExceeded -> 504
apiresponse.WithRequestIDFunc(func(r *http.Request) string {
    return r.Header.Get("X-Request-ID") // filled into error bodies automatically
})
```

Error resolution order: an `*ErrorBody` already in the error chain, then mappers
in registration order, then a generic 500.

- **Logging:** only 5xx responses are logged (default: `slog`). The logger gets
  the original error, so the real cause is visible. `WithLogger(nil)` disables
  it. `r` is nil when `WriteError` is called without a request.
- **Panics:** `Recover` returns the standard 500 envelope and logs the panic
  with its stack as a separate `stack` attribute (the error message is just
  `panic: <value>`). It re-panics `http.ErrAbortHandler`. If the handler already
  started writing, the status cannot be changed.

## Behavior notes

- Unknown errors never leak: the client sees `internal server error`. A
  typed-nil `*ErrorBody` is treated as an unknown error (generic 500).
- `Data` is a pointer, so `0`, `""` and `false` are still serialized; a nil
  slice in `List` becomes `[]`.
- JSON is written without HTML escaping.
- `Write` marshals before sending headers, so an encode failure is a clean 500.
  1xx, 204 and 304 responses carry no body.
- Sentinels match by code: `errors.Is(err, ErrUserNotFound)` stays true after
  `With*` copies. `apiresponse.IsCode(err, apiresponse.CodeNotFound)` checks
  just the code.
- `With*` methods copy, so package-level sentinel errors are safe:
  `var ErrUserNotFound = apiresponse.NotFound("user not found")`.

## Claude Code skill

This repo ships a skill at `.claude/skills/use-apiresponse/SKILL.md`. It teaches
Claude Code to use this library (envelope, errors, pagination, mappers,
pitfalls) instead of writing ad-hoc JSON responses. Install it into the project
that uses apiresponse, or globally for all your projects.

**Option 1: via Go (matches the version you installed)**

```bash
go get github.com/catstackdev/apiresponse@v0.1.0
SRC="$(go list -m -f '{{.Dir}}' github.com/catstackdev/apiresponse)/.claude/skills/use-apiresponse"

# This project only
mkdir -p .claude/skills
cp -r "$SRC" .claude/skills/
chmod -R u+w .claude/skills/use-apiresponse

# Or global (every project)
mkdir -p ~/.claude/skills
cp -r "$SRC" ~/.claude/skills/
chmod -R u+w ~/.claude/skills/use-apiresponse
```

The `chmod` is needed because the Go module cache is read-only. Re-run after
upgrading the library so the skill does not go stale.

**Option 2: via the GitHub CLI**

```bash
# This project only
gh skill install catstackdev/apiresponse use-apiresponse \
  --allow-hidden-dirs --agent claude-code

# Or global (every project)
gh skill install catstackdev/apiresponse use-apiresponse \
  --allow-hidden-dirs --agent claude-code --scope user

# Pin a version, and update later
gh skill install catstackdev/apiresponse use-apiresponse@v0.1.0 \
  --allow-hidden-dirs --agent claude-code
gh skill update
```

`--allow-hidden-dirs` is required because the skill lives under `.claude/`.
`gh` prints a warning about hidden directories; that is expected here.

**Using it:** ask things like "add a GET /orders endpoint with cursor
pagination" and Claude picks the skill up from its description. Inside this repo
it loads automatically.

## Development

```bash
go vet ./...
go test -race -cover ./...
```
