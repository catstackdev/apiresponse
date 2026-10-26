package apiresponse_test

import (
	"context"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/http/httptrace"
	"strings"
	"testing"
	"time"

	"github.com/catstackdev/apiresponse"
)

// newProtoServer starts a real server speaking HTTP/1.1 (TLS off) or HTTP/2
// (TLS + ALPN), so these tests exercise net/http's own ResponseWriters rather
// than httptest.ResponseRecorder.
func newProtoServer(t *testing.T, h http.Handler, http2 bool) *httptest.Server {
	t.Helper()
	// httptest panics when it cannot listen; skip instead (e.g. sandboxed runs).
	if l, err := net.Listen("tcp", "127.0.0.1:0"); err != nil {
		t.Skipf("cannot bind a loopback port: %v", err)
	} else {
		_ = l.Close()
	}
	srv := httptest.NewUnstartedServer(h)
	if http2 {
		srv.EnableHTTP2 = true
		srv.StartTLS()
	} else {
		srv.Start()
	}
	t.Cleanup(srv.Close)
	return srv
}

// post sends body and reports whether the connection was reused from an earlier
// request on the same client.
func post(t *testing.T, c *http.Client, url, body string) (*http.Response, bool) {
	t.Helper()
	var reused bool
	trace := &httptrace.ClientTrace{GotConn: func(i httptrace.GotConnInfo) { reused = i.Reused }}
	req, err := http.NewRequestWithContext(httptrace.WithClientTrace(context.Background(), trace), http.MethodPost, url, strings.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := c.Do(req)
	if err != nil {
		t.Fatalf("POST: %v", err)
	}
	t.Cleanup(func() { _ = resp.Body.Close() })
	_, _ = io.Copy(io.Discard, resp.Body)
	return resp, reused
}

func TestProtocols_OversizedBody(t *testing.T) {
	wr := apiresponse.NewWriter(apiresponse.WithLogger(nil))
	h := wr.Recover(wr.Handler(func(w http.ResponseWriter, r *http.Request) error {
		_, err := apiresponse.DecodeLimit[createUser](w, r, 16)
		return err
	}))
	big := `{"name":"` + strings.Repeat("a", 4096) + `"}`

	for _, tc := range []struct {
		name       string
		http2      bool
		wantMajor  int
		wantClose  bool // response announces the connection will close
		wantReused bool // a follow-up request reuses the connection
	}{
		{"HTTP/1.1 closes the connection", false, 1, true, false},
		{"HTTP/2 keeps the connection", true, 2, false, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			srv := newProtoServer(t, h, tc.http2)
			c := srv.Client()

			resp, _ := post(t, c, srv.URL, big)
			if resp.ProtoMajor != tc.wantMajor {
				t.Fatalf("proto = %s, want major %d", resp.Proto, tc.wantMajor)
			}
			if resp.StatusCode != http.StatusRequestEntityTooLarge {
				t.Fatalf("status = %d, want 413", resp.StatusCode)
			}
			if resp.Close != tc.wantClose {
				t.Fatalf("resp.Close = %v, want %v", resp.Close, tc.wantClose)
			}

			// A small valid body on the same client: does it ride the old connection?
			resp2, reused := post(t, c, srv.URL, `{"name":"ok"}`)
			if resp2.StatusCode != http.StatusOK { // Handler returned nil and wrote nothing
				t.Fatalf("follow-up status = %d, want 200", resp2.StatusCode)
			}
			if reused != tc.wantReused {
				t.Fatalf("follow-up reused connection = %v, want %v", reused, tc.wantReused)
			}
		})
	}
}

func TestProtocols_ServeContentThroughRecover(t *testing.T) {
	const payload = "file-bytes-file-bytes"
	h := apiresponse.Recover(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.ServeContent(w, r, "f.txt", time.Time{}, strings.NewReader(payload))
	}))
	for _, http2 := range []bool{false, true} {
		name := map[bool]string{false: "HTTP/1.1", true: "HTTP/2"}[http2]
		t.Run(name, func(t *testing.T) {
			srv := newProtoServer(t, h, http2)
			resp, err := srv.Client().Get(srv.URL)
			if err != nil {
				t.Fatal(err)
			}
			defer func() { _ = resp.Body.Close() }()
			got, _ := io.ReadAll(resp.Body)
			if string(got) != payload || resp.StatusCode != http.StatusOK {
				t.Fatalf("status = %d, body = %q", resp.StatusCode, got)
			}
		})
	}
}
