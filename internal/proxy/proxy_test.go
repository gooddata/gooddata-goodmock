// (C) 2026 GoodData Corporation
package proxy

import (
	"errors"
	"net"
	"testing"

	"github.com/valyala/fasthttp"
)

// TestProxyRequestDialAttempts pins the PROXY_DIAL_ATTEMPTS contract: the value
// is a budget of total dial attempts, initial dial included.
//
// The method is varied on purpose. fasthttp's own HostClient retries idempotent
// requests (GET/HEAD/PUT) internally up to DefaultMaxIdemponentCallAttempts, so
// this also guards against that stacking on top of our loop and multiplying the
// dial count.
func TestProxyRequestDialAttempts(t *testing.T) {
	tests := []struct {
		name      string
		method    string
		env       string
		wantDials int
	}{
		{name: "default budget, POST", method: "POST", env: "", wantDials: 3},
		{name: "default budget, GET", method: "GET", env: "", wantDials: 3},
		{name: "retrying disabled, POST", method: "POST", env: "1", wantDials: 1},
		{name: "retrying disabled, GET", method: "GET", env: "1", wantDials: 1},
		{name: "explicit budget, POST", method: "POST", env: "4", wantDials: 4},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Setenv("PROXY_DIAL_ATTEMPTS", tt.env)

			dials := 0
			client := &fasthttp.Client{
				Dial: func(_ string) (net.Conn, error) {
					dials++
					return nil, fasthttp.ErrDialTimeout
				},
			}

			ctx := &fasthttp.RequestCtx{}
			ctx.Request.Header.SetMethod(tt.method)
			ctx.Request.SetRequestURI("/api/thing")

			_, _, _, err := ProxyRequest(client, "http://upstream.invalid", ctx)
			if !errors.Is(err, fasthttp.ErrDialTimeout) {
				t.Fatalf("ProxyRequest() error = %v, want it to wrap fasthttp.ErrDialTimeout", err)
			}
			if dials != tt.wantDials {
				t.Errorf("dial count = %d, want %d", dials, tt.wantDials)
			}
		})
	}
}

// TestProxyRequestDoesNotRetryNonDialErrors makes sure the retry is scoped to
// dial failures. Once a connection exists we cannot know whether the upstream
// applied the request, so anything else must surface on the first attempt.
func TestProxyRequestDoesNotRetryNonDialErrors(t *testing.T) {
	t.Setenv("PROXY_DIAL_ATTEMPTS", "3")

	sentinel := errors.New("connection refused by test")
	dials := 0
	client := &fasthttp.Client{
		Dial: func(_ string) (net.Conn, error) {
			dials++
			return nil, sentinel
		},
	}

	ctx := &fasthttp.RequestCtx{}
	ctx.Request.Header.SetMethod("POST")
	ctx.Request.SetRequestURI("/api/thing")

	_, _, _, err := ProxyRequest(client, "http://upstream.invalid", ctx)
	if err == nil {
		t.Fatal("ProxyRequest() error = nil, want the dialer's error")
	}
	if errors.Is(err, fasthttp.ErrDialTimeout) {
		t.Fatalf("ProxyRequest() error = %v, should not be classified as a dial timeout", err)
	}
	if dials != 1 {
		t.Errorf("dial count = %d, want 1 — a non-dial-timeout error must not be retried", dials)
	}
}
