// (C) 2026 GoodData Corporation
package proxy

import (
	"errors"
	"net"
	"testing"

	"github.com/valyala/fasthttp"
)

// dialCountingClient returns a Client whose dialer always fails with err, plus a
// pointer to the dial counter, so a test can assert how many dials a single
// ProxyRequest performed for a given attempt budget.
func dialCountingClient(attempts int, err error) (*Client, *int) {
	dials := 0
	return &Client{
		HTTP: &fasthttp.Client{
			Dial: func(_ string) (net.Conn, error) {
				dials++
				return nil, err
			},
		},
		Attempts: attempts,
	}, &dials
}

// TestProxyRequestDialAttempts pins the Client.Attempts contract: the value is a
// budget of total dial attempts, initial dial included.
//
// The budget is set on the Client directly rather than through the environment.
// That it comes from PROXY_DIAL_ATTEMPTS is common.ProxyDialAttempts's contract
// (covered in common_test.go) and NewClient's (TestNewClientAttempts below);
// ProxyRequest only has to honour whatever budget it was handed.
//
// The method is varied on purpose. fasthttp's own HostClient retries idempotent
// requests (GET/HEAD/PUT) internally up to DefaultMaxIdemponentCallAttempts, so
// this also guards against that stacking on top of our loop and multiplying the
// dial count.
func TestProxyRequestDialAttempts(t *testing.T) {
	tests := []struct {
		name      string
		method    string
		attempts  int
		wantDials int
	}{
		{name: "default budget, POST", method: "POST", attempts: 3, wantDials: 3},
		{name: "default budget, GET", method: "GET", attempts: 3, wantDials: 3},
		{name: "retrying disabled, POST", method: "POST", attempts: 1, wantDials: 1},
		{name: "retrying disabled, GET", method: "GET", attempts: 1, wantDials: 1},
		{name: "explicit budget, POST", method: "POST", attempts: 4, wantDials: 4},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			client, dials := dialCountingClient(tt.attempts, fasthttp.ErrDialTimeout)

			ctx := &fasthttp.RequestCtx{}
			ctx.Request.Header.SetMethod(tt.method)
			ctx.Request.SetRequestURI("/api/thing")

			_, _, _, err := ProxyRequest(client, "http://upstream.invalid", ctx)
			if !errors.Is(err, fasthttp.ErrDialTimeout) {
				t.Fatalf("ProxyRequest() error = %v, want it to wrap fasthttp.ErrDialTimeout", err)
			}
			if *dials != tt.wantDials {
				t.Errorf("dial count = %d, want %d", *dials, tt.wantDials)
			}
		})
	}
}

// TestNewClientAttempts covers the wiring the table above no longer exercises:
// NewClient resolves PROXY_DIAL_ATTEMPTS once, at construction. That is what
// keeps an invalid value fatal at startup instead of on the first proxied
// request, and what keeps the attempt budget on the same snapshot as the dial
// timeout baked into the dialer.
func TestNewClientAttempts(t *testing.T) {
	tests := []struct {
		name string
		env  string
		want int
	}{
		{name: "default when unset", env: "", want: 3},
		{name: "value from environment", env: "5", want: 5},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Setenv("PROXY_DIAL_ATTEMPTS", tt.env)

			if got := NewClient().Attempts; got != tt.want {
				t.Errorf("NewClient().Attempts = %d, want %d", got, tt.want)
			}
		})
	}
}

// TestProxyRequestDoesNotRetryNonDialErrors makes sure the retry is scoped to
// dial failures. Once a connection exists we cannot know whether the upstream
// applied the request, so anything else must surface on the first attempt.
func TestProxyRequestDoesNotRetryNonDialErrors(t *testing.T) {
	sentinel := errors.New("connection refused by test")
	client, dials := dialCountingClient(3, sentinel)

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
	if *dials != 1 {
		t.Errorf("dial count = %d, want 1 — a non-dial-timeout error must not be retried", *dials)
	}
}
