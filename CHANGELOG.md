# Changelog

Notable changes per tagged release.

## Unreleased

- Breaking: package-level `WriteError` now takes the request:
  `WriteError(w, r, err)` (`r` may be nil), matching `Writer.WriteError`.
- Add `ErrorBody.WithRetryAfter` (sends a `Retry-After` header).
- Add `X-Content-Type-Options: nosniff` to every JSON response.
- `Decode` treats a nil `r.Body` as empty instead of panicking.
- `CursorPage` clamps a negative limit to 0, like `OffsetPage`.
- `Handler` and `Recover` drop stale `Content-Length` and `Content-Disposition`
  before writing an error envelope.
- `Handler` drops a broken-pipe or connection-reset error returned after the
  response started (client gone) instead of logging it as a server error.
- Default logger no longer prints `error=<nil>`; the `Writer` doc now says the
  zero value works (without logging).
- Add fuzz tests for `Decode` and `OffsetPage`, and runnable examples.

## v0.1.3

- Fix: `trackingWriter` keeps `http.ServeContent` on the sendfile path and
  counts only real output (a panicking `WriteHeader`, unsupported `Flush` or
  failed `Hijack` no longer mark the response as started).
- Fix: lint cleanup on late-error paths.

## v0.1.2

- `Decode` accepts `application/*+json` media types.
- `*ErrorBody` methods are nil-safe.
- Default 5xx log includes `request_id` and skips `context.Canceled`.
- `Detail.Code` for machine-readable field reasons.
- `NotFoundHandler` / `MethodNotAllowedHandler` return JSON envelopes.
- `Handler` and `Recover` no longer append an error envelope after output has
  started; the error or panic is logged instead.

## v0.1.1

- `Decode` hides raw read errors, rejects `null` bodies and sets
  `Connection: close` on HTTP/1 413 responses.
- `OffsetPage` computes the page count without overflow.
- `Write` rejects statuses outside 100-999 with a clean 500.
- Error copies no longer share `Details` slices.

## v0.1.0

- Initial release: `Response`, pagination helpers, `ErrorBody` with per-status
  constructors, `Writer` with error mappers, logger and request ID option,
  `Recover` middleware.
- Sentinels are distinguished by code and message; `ListWithSummary`, `Decode`.
