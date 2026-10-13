// Package apiresponse provides a small, framework-agnostic JSON response
// envelope for HTTP APIs.
//
// Success responses look like:
//
//	{"data": {...}, "pagination": {...}}
//
// Error responses look like:
//
//	{"error": {"code": "not_found", "message": "user not found"}}
//
// The HTTP status code carries success/failure; the body never repeats it.
//
// The package depends only on the standard library. It works with net/http
// and chi directly via [Write] and [WriteError], and with Gin, Echo or Fiber
// by passing the same values to their own JSON methods.
package apiresponse
