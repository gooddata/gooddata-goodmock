package common

import (
	"testing"
	"time"
)

func TestProxyDialTimeout(t *testing.T) {
	tests := []struct {
		name string
		env  string
		want time.Duration
	}{
		// The default must stay comfortably longer than fasthttp's own 3s
		// DefaultDialTimeout, which is what this setting exists to override.
		{name: "unset falls back to the default", env: "", want: 30 * time.Second},
		{name: "seconds", env: "5s", want: 5 * time.Second},
		{name: "sub-second", env: "250ms", want: 250 * time.Millisecond},
		{name: "compound", env: "1m30s", want: 90 * time.Second},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Always set it, empty included: an empty value is what the getter
			// treats as unset, and setting it unconditionally stops a value
			// inherited from the caller's environment from deciding the result.
			t.Setenv("PROXY_DIAL_TIMEOUT", tt.env)
			if got := ProxyDialTimeout(); got != tt.want {
				t.Errorf("ProxyDialTimeout() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestProxyDialAttempts(t *testing.T) {
	tests := []struct {
		name string
		env  string
		want int
	}{
		{name: "unset falls back to the default", env: "", want: 3},
		{name: "one disables retrying", env: "1", want: 1},
		{name: "explicit value", env: "5", want: 5},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Setenv("PROXY_DIAL_ATTEMPTS", tt.env)
			if got := ProxyDialAttempts(); got != tt.want {
				t.Errorf("ProxyDialAttempts() = %v, want %v", got, tt.want)
			}
		})
	}
}
