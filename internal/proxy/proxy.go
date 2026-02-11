// (C) 2025 GoodData Corporation
package proxy

import (
	"bufio"
	"bytes"
	"strings"

	"github.com/valyala/fasthttp"
)

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

	if err := client.Do(req, resp); err != nil {
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
