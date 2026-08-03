// (C) 2025 GoodData Corporation
package proxy

import (
	"bufio"
	"bytes"
	"errors"
	"goodmock/internal/common"
	"log"
	"net"
	"strings"

	"github.com/valyala/fasthttp"
)

// NewClient builds the HTTP client used to reach the upstream.
//
// The dial timeout is set explicitly rather than left to fasthttp's 3s default;
// see common.ProxyDialTimeout for why. Read and write timeouts are deliberately
// left unset — upstream calls here include long polls that legitimately run for
// tens of seconds, and capping them would turn slow-but-fine responses into
// proxy errors.
//
// The dialer is owned by this client instead of using the package-level
// fasthttp.DialTimeout, which delegates to a process-global TCPDialer. Owning it
// keeps the DNS cache and the concurrency limiter scoped to this client rather
// than shared with anything else in the process that dials via fasthttp.
func NewClient() *fasthttp.Client {
	timeout := common.ProxyDialTimeout()
	// Concurrency mirrors fasthttp's own default dialer so that owning the
	// dialer does not silently change how many dials may run at once.
	dialer := &fasthttp.TCPDialer{Concurrency: 1000}
	return &fasthttp.Client{
		Dial: func(addr string) (net.Conn, error) {
			return dialer.DialTimeout(addr, timeout)
		},
	}
}

// ProxyRequest forwards a request to the upstream server and returns the response details.
func ProxyRequest(client *fasthttp.Client, upstream string, ctx *fasthttp.RequestCtx) (int, map[string][]string, []byte, error) {
	req := fasthttp.AcquireRequest()
	resp := fasthttp.AcquireResponse()
	defer fasthttp.ReleaseRequest(req)
	defer fasthttp.ReleaseResponse(resp)

	// Build upstream URL from the raw request URI
	rawURI := string(ctx.RequestURI())
	req.SetRequestURI(upstream + rawURI)
	req.Header.SetMethod(string(ctx.Method()))

	// Copy request headers, skip Host (set by SetRequestURI)
	ctx.Request.Header.VisitAll(func(key, value []byte) {
		if strings.EqualFold(string(key), "Host") {
			return
		}
		req.Header.SetBytesKV(key, value)
	})

	// Copy request body
	if body := ctx.PostBody(); len(body) > 0 {
		req.SetBody(body)
	}

	// Retry only on a dial timeout. That error means the TCP connection was
	// never established, so the upstream cannot have seen — let alone applied —
	// the request, which makes a retry safe for every method including POST.
	// Any other failure is returned as-is: once bytes are on the wire we have no
	// way to know whether the upstream acted on them.
	attempts := common.ProxyDialAttempts()
	var err error
	for attempt := 1; ; attempt++ {
		err = client.Do(req, resp)
		if err == nil || attempt >= attempts || !errors.Is(err, fasthttp.ErrDialTimeout) {
			break
		}
		log.Printf("Upstream dial timed out (attempt %d/%d), retrying: %v", attempt, attempts, err)
		resp.Reset()
	}
	if err != nil {
		return 0, nil, nil, err
	}

	// Decompress gzip if needed so recordings store readable bodies
	body := resp.Body()
	if string(resp.Header.Peek("Content-Encoding")) == "gzip" {
		if decompressed, err := fasthttp.AppendGunzipBytes(nil, body); err == nil {
			body = decompressed
		}
	}

	// Make a copy of the body (fasthttp reuses buffers)
	bodyCopy := make([]byte, len(body))
	copy(bodyCopy, body)

	// Collect response headers preserving original casing.
	// fasthttp's VisitAll normalizes header names to title-case (e.g. X-Xss-Protection),
	// so we parse the raw header bytes to preserve the upstream's original casing.
	respHeaders := parseRawHeaders(resp.Header.Header())

	return resp.StatusCode(), respHeaders, bodyCopy, nil
}

// parseRawHeaders extracts header key-value pairs from raw HTTP response header bytes,
// preserving the original header name casing from the upstream server.
func parseRawHeaders(raw []byte) map[string][]string {
	headers := make(map[string][]string)
	scanner := bufio.NewScanner(bytes.NewReader(raw))
	first := true
	for scanner.Scan() {
		line := scanner.Text()
		// Skip the status line (e.g. "HTTP/1.1 200 OK")
		if first {
			first = false
			continue
		}
		idx := strings.IndexByte(line, ':')
		if idx < 0 {
			continue
		}
		key := line[:idx]
		value := strings.TrimSpace(line[idx+1:])
		headers[key] = append(headers[key], value)
	}
	return headers
}
