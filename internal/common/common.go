package common

import (
	"log"
	"os"
	"strconv"
	"strings"
	"time"
)

func GetPort() int {
	if p := os.Getenv("PORT"); p != "" {
		if port, err := strconv.Atoi(p); err == nil {
			return port
		}
		log.Fatalf("Invalid PORT value: %s", p)
	}
	return 8080
}

func IsVerbose() bool {
	return os.Getenv("VERBOSE") != ""
}

// ProxyDialTimeout returns the TCP connect timeout used when dialing the
// upstream. A fasthttp.Client with no Dial set falls back to
// fasthttp.DefaultDialTimeout, which is 3s — shorter than a rolling
// load-balancer or ingress update. While the balancer's targets are draining,
// SYNs go unanswered, the dial times out, and the caller gets a 502 even
// though the upstream itself is healthy. Record and proxy modes.
func ProxyDialTimeout() time.Duration {
	if v := os.Getenv("PROXY_DIAL_TIMEOUT"); v != "" {
		d, err := time.ParseDuration(v)
		if err != nil {
			log.Fatalf("Invalid PROXY_DIAL_TIMEOUT value: %s (expected a Go duration, e.g. 30s)", v)
		}
		if d <= 0 {
			log.Fatalf("Invalid PROXY_DIAL_TIMEOUT value: %s (must be positive)", v)
		}
		return d
	}
	return 30 * time.Second
}

// ProxyDialAttempts returns the maximum total number of times a request may be
// dialed when the TCP connection cannot be established at all. The initial dial
// counts towards the budget, so 3 permits one dial plus two retries and 1
// disables retrying. Re-dialing is safe for every HTTP method: no bytes reached
// the upstream, so the request cannot have been applied. Record and proxy modes.
//
// Attempts also tend to land on different addresses when the upstream resolves
// to several — a load balancer with one node per availability zone, say.
// fasthttp advances a round-robin index over the resolved set once per dial, and
// it abandons a dial as soon as one address times out rather than walking the
// rest, so consecutive attempts start from consecutive addresses. Note this
// rotates over fasthttp's cached set; the addresses are only re-resolved once
// DNSCacheDuration (1 minute) has elapsed.
func ProxyDialAttempts() int {
	if v := os.Getenv("PROXY_DIAL_ATTEMPTS"); v != "" {
		n, err := strconv.Atoi(v)
		if err != nil || n < 1 {
			log.Fatalf("Invalid PROXY_DIAL_ATTEMPTS value: %s (expected an integer >= 1)", v)
		}
		return n
	}
	return 3
}

// PreserveJSONKeyOrder returns true if JSON response body key order should be
// preserved from the upstream server. When false (default), keys are sorted
// alphabetically for deterministic diffs. Record mode only.
func PreserveJSONKeyOrder() bool {
	return os.Getenv("PRESERVE_JSON_KEY_ORDER") != ""
}

func SortArrayMembers() bool {
	return os.Getenv("SORT_ARRAY_MEMBERS") != ""
}

// ParseJSONContentTypes returns the list of Content-Types whose response bodies
// should be stored as structured JSON (jsonBody) instead of escaped strings.
// application/json is always included.
func ParseJSONContentTypes() []string {
	types := []string{"application/json"}
	if env := os.Getenv("JSON_CONTENT_TYPES"); env != "" {
		for _, t := range strings.Split(env, ",") {
			t = strings.TrimSpace(t)
			if t != "" && t != "application/json" {
				types = append(types, t)
			}
		}
	}
	return types
}

// ParseBinaryContentTypes returns the list of Content-Types whose response bodies
// should be stored as base64-encoded strings. Empty by default.
func ParseBinaryContentTypes() []string {
	var types []string
	if env := os.Getenv("BINARY_CONTENT_TYPES"); env != "" {
		for _, t := range strings.Split(env, ",") {
			t = strings.TrimSpace(t)
			if t != "" {
				types = append(types, t)
			}
		}
	}
	return types
}
