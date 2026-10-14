# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with
code in this repository.

## What this is

`github.com/catstackdev/apiresponse`: a reusable, framework-agnostic JSON
response envelope library for Go HTTP APIs, meant to be imported by other
projects. Standard library only, Go 1.22+. Keep it that way: no framework
dependencies in the root module.

## Commands

```bash
go build ./...
go vet ./...
gofmt -l .                                   # should print nothing
go test -race -cover ./...
go test -run TestWriteError ./...            # single test
go test -run Example ./...                   # runnable examples (output is asserted)
```

## Architecture

One package (`apiresponse`), split by file, not by subpackage. Errors
deliberately live in the same package as responses because `WriteError`,
`Handler` and `ErrorResponse` all depend on `*ErrorBody`.

- `response.go`: success envelope `Response[T]` (`Data *T`, optional
  `Pagination`, optional `Summary`) and constructors `OK`, `List`,
  `ListWithSummary`, `CursorPage`, `OffsetPage`.
- `errors.go`: `ErrorBody` (implements `error`), `ErrorResponse` envelope, code
  constants, constructors per HTTP status (adding a code means adding its
  constructor, a test row, and the README table), `FromError`, `IsCode`, and the
  opt-in `ContextErrors` mapper (`DeadlineExceeded` -> 504).
- `writer.go`: `Writer` (options `WithErrorMapper`, `WithLogger`,
  `WithRequestIDFunc`) holds the real logic: `Write`, `WriteError`, `Handler`.
  Error resolution order: existing `*ErrorBody` in the chain, then mappers, then
  generic 500.
- `decode.go`: `Decode[T]` / `DecodeLimit[T]` request-body helpers (size limit,
  unknown fields rejected, single JSON value). Failures are `*ErrorBody` (400,
  413, 415) ready for `Handler`.
- `recover.go`: `Writer.Recover` panic middleware (re-panics
  `http.ErrAbortHandler`).
- `tracker.go`: `trackingWriter`, the `ResponseWriter` wrapper used by `Handler`
  and `Recover` to know whether a response already started. It must keep
  forwarding `Flush`/`Hijack` and `Unwrap` (websocket upgraders assert
  `http.Hijacker`).
- `write.go`: package-level `Write`, `WriteError`, `Handler`, `Recover`,
  `NotFoundHandler`, `MethodNotAllowedHandler` delegating to a default `Writer`.
  No global mutable state; customization goes through `NewWriter`.

Behaviors that span files and are easy to break:

- **No `status` field in the body.** The HTTP status code is the source of
  truth. Success and error are separate envelopes (`{"data":...}` vs
  `{"error":...}`), so an error never carries `"data": null`.
- **`Data` is `*T`** so zero values (`0`, `""`, `false`) still serialize. `List`
  converts a nil slice to `[]`.
- **`ErrorBody` HTTP status and cause are unexported and `json:"-"`.** They
  reach callers only through `HTTPStatus()` and `Unwrap()`. `With*` methods copy
  rather than mutate, so package-level sentinel errors stay safe. Preserve this.
- **`FromError` never leaks internals.** Non-`ErrorBody` errors become a generic
  500; the original is kept only as the cause (for logging, `errors.Is/As`).
- **`Write` marshals before `WriteHeader`**, so an encode failure produces a
  clean 500 (`internalBody`) rather than a half-written response. 1xx/204/304
  write no body.
- **`Write` rejects statuses outside 100-999** with a clean 500 and a returned
  error (net/http would panic). Nothing logs it.
- **`Decode` error messages never leak internals.** Only unknown-field errors
  (detected by the `json: unknown field ` prefix, since encoding/json has no
  typed error) and per-field type errors echo client input; everything else is a
  fixed message. A `null` body is a 400, `maxBytes <= 0` means the default
  limit, and a body read error is a 400.
- **`Decode` accepts `application/json` and `application/*+json`**; other media
  types are 415.
- **`*ErrorBody` methods are nil-safe** (`Error` is `<nil>`, `HTTPStatus` 500,
  `With*` on nil yield a generic internal error).
- **No second envelope after output started.** `Handler` and `Recover` log the
  late error/panic (via `Writer.prepare`) but do not write. 1xx other than 101
  do not count as started.
- **`Detail.Code`** is an optional machine-readable reason (`omitempty`).
  `MethodNotAllowedHandler` sets `Allow` itself before returning the error.
- **`With*` methods clone `Details`** (via `clone()`), and `Validation` /
  `WithDetails` copy the caller's slice, so copies never share it.
- **`Writer.resolve` skips mappers for a nil error** (generic 500), so mappers
  never see nil.
- **`OffsetPage` computes the page count by division**, never by multiplying
  page by limit, to avoid int64 overflow.
- **Typed-nil `*ErrorBody` is ignored.** `findErrorBody` (errors.go) is the one
  place that searches an error chain; `FromError`, `IsCode` and `Writer.resolve`
  all use it so a nil `*ErrorBody` never gets dereferenced.
- **Panics:** `Recover` wraps the value in an unexported `panicError` (message
  `panic: <v>`, stack kept separately); the default logger emits the stack as a
  `stack` attr. Custom `Logger`s only get `panic: <v>` as the error (the type is
  unexported), so the stack is default-logger-only by design.
- **5xx logging:** the Logger fires only for 5xx and receives the original error
  (mapped errors get it attached as cause). `r` may be nil when called via
  package-level `WriteError`. The default logger adds `request_id` when set and
  skips `context.Canceled` (client gone); custom Loggers still see everything.
- JSON is encoded with HTML escaping off and the trailing newline trimmed.
- `Pagination` serves both cursor and offset styles; unused fields are
  `omitempty`.

## Conventions

- Tests are in `package apiresponse_test` (public API only). `example_test.go`
  uses `// Output:` blocks, so changing JSON field names or order breaks them
  intentionally.
- Framework support (Gin, Echo, Fiber) is documented in the README, not
  implemented: those frameworks call `c.JSON(status, apiresponse.OK(data))`
  directly. If adapters are ever added, put them in a separate Go module (e.g.
  `ginresp/` with its own `go.mod`) so the core stays dependency-free.

- CI is `.github/workflows/ci.yml` (vet, `go test -race`, golangci-lint); not
  verified locally.
- `.claude/skills/use-apiresponse/SKILL.md` is the usage guide for consumers of
  this library. Update it (and the README) whenever the public API or
  error-resolution behavior changes.
- `ErrorBody.Is` matches by `Code` AND `Message`, so `errors.Is` works across
  `With*` copies while sentinels sharing a code (`ErrUserNotFound`,
  `ErrOrderNotFound`) stay distinct. Never change a sentinel's message in a
  `With*` method. `IsCode` is the code-only check. Request IDs from
  `WithRequestIDFunc` are applied on a copy in `Writer.WriteError`, never on the
  shared error.
