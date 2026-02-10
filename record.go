// (C) 2025 GoodData Corporation
package main

import (
	"bytes"
	"crypto/rand"
	"encoding/json"
	"fmt"
	"log"
	"net/url"
	"os"
	"regexp"
	"strings"
	"sync"

	"github.com/valyala/fasthttp"
)

// RecordedExchange captures a single proxied request/response pair.
type RecordedExchange struct {
	Method      string
	URL         string // raw URI (path + query string, percent-encoded)
	ReqBody     []byte
	Status      int
	RespHeaders map[string][]string
	RespBody    []byte
}

// RecordServer proxies requests to an upstream backend and records exchanges.
type RecordServer struct {
	*Server // embedded for stub matching and admin API
	mu      sync.Mutex
	exchanges []RecordedExchange
	upstream  string
	client    *fasthttp.Client
}

// NewRecordServer creates a new recording proxy server.
func NewRecordServer(upstream, proxyHost, refererPath string, verbose bool) *RecordServer {
	return &RecordServer{
		Server:    NewServer(proxyHost, refererPath, verbose),
		exchanges: make([]RecordedExchange, 0),
		upstream:  upstream,
		client:    &fasthttp.Client{},
	}
}

// HandleRequest routes admin requests locally, checks stubs, then proxies+records.
func (rs *RecordServer) HandleRequest(ctx *fasthttp.RequestCtx) {
	rawURI := string(ctx.RequestURI())
	path := rawURI
	if idx := strings.IndexByte(rawURI, '?'); idx != -1 {
		path = rawURI[:idx]
	}
	method := string(ctx.Method())

	// Admin endpoints handled locally
	if strings.HasPrefix(path, "/__admin") {
		rs.handleRecordAdmin(ctx, path, method)
		return
	}

	if rs.Server.verbose {
		rs.Server.logRequest(ctx, method, rawURI)
	}

	// Transform request headers before proxying
	rs.Server.transformRequestHeaders(&ctx.Request.Header)

	// In record mode, always proxy and record — no stub matching
	rs.proxyAndRecord(ctx)
}

// proxyAndRecord forwards the request to upstream, records the exchange, and returns the response.
func (rs *RecordServer) proxyAndRecord(ctx *fasthttp.RequestCtx) {
	status, respHeaders, body, err := proxyRequest(rs.client, rs.upstream, ctx)
	if err != nil {
		log.Printf("Proxy error: %v", err)
		ctx.SetStatusCode(502)
		ctx.SetBodyString(fmt.Sprintf(`{"error": "proxy error: %s"}`, err.Error()))
		return
	}

	// Record the exchange
	rawURI := string(ctx.RequestURI())
	reqBody := ctx.PostBody()
	reqBodyCopy := make([]byte, len(reqBody))
	copy(reqBodyCopy, reqBody)

	exchange := RecordedExchange{
		Method:      string(ctx.Method()),
		URL:         rawURI,
		ReqBody:     reqBodyCopy,
		Status:      status,
		RespHeaders: respHeaders,
		RespBody:    body,
	}

	rs.mu.Lock()
	rs.exchanges = append(rs.exchanges, exchange)
	rs.mu.Unlock()

	// Send response back to client, filtering headers
	for key, values := range respHeaders {
		upperKey := strings.ToUpper(key)
		if strings.HasPrefix(upperKey, "X-GDC") || upperKey == "DATE" {
			continue
		}
		// Skip Content-Encoding since we decompressed
		if upperKey == "CONTENT-ENCODING" {
			continue
		}
		// Skip Content-Length since body size may have changed after decompression
		if upperKey == "CONTENT-LENGTH" {
			continue
		}
		for _, v := range values {
			ctx.Response.Header.Add(key, v)
		}
	}
	ctx.SetStatusCode(status)
	ctx.SetBody(body)

	if rs.Server.verbose {
		log.Printf("[verbose] << %d %s %s (%d bytes)", status, string(ctx.Method()), string(ctx.RequestURI()), len(body))
	}
}

// clearExchanges removes all recorded exchanges.
func (rs *RecordServer) clearExchanges() {
	rs.mu.Lock()
	rs.exchanges = make([]RecordedExchange, 0)
	rs.mu.Unlock()
}

// handleRecordAdmin handles admin API endpoints in record mode.
func (rs *RecordServer) handleRecordAdmin(ctx *fasthttp.RequestCtx, path, method string) {
	// Snapshot is record-mode specific
	if path == "/__admin/recordings/snapshot" && method == "POST" {
		rs.handleSnapshot(ctx)
		return
	}

	// Reset clears both stubs and recordings
	if (path == "/__admin/reset" || path == "/__admin/mappings/reset") && method == "POST" {
		rs.Server.ClearMappings()
		rs.clearExchanges()
		log.Println("All mappings and recordings reset")
		ctx.SetStatusCode(fasthttp.StatusOK)
		return
	}

	// DELETE requests clears recordings
	if path == "/__admin/requests" && method == "DELETE" {
		rs.clearExchanges()
		ctx.SetStatusCode(fasthttp.StatusOK)
		return
	}

	// Delegate everything else to the replay server's admin handler
	rs.Server.handleAdmin(ctx, path, method)
}

// SnapshotRequest represents the body of a POST /__admin/recordings/snapshot request.
type SnapshotRequest struct {
	Filters struct {
		URLPattern string `json:"urlPattern,omitempty"`
	} `json:"filters"`
	Persist            bool `json:"persist"`
	RepeatsAsScenarios bool `json:"repeatsAsScenarios"`
}

// handleSnapshot handles POST /__admin/recordings/snapshot.
func (rs *RecordServer) handleSnapshot(ctx *fasthttp.RequestCtx) {
	var snapReq SnapshotRequest
	json.Unmarshal(ctx.PostBody(), &snapReq)

	rs.mu.Lock()
	// Filter by URL pattern and remove matched exchanges from the pool
	var filtered []RecordedExchange
	var remaining []RecordedExchange
	if snapReq.Filters.URLPattern != "" {
		matcher := compileURLMatcher(snapReq.Filters.URLPattern)
		for _, ex := range rs.exchanges {
			if matcher(ex.URL) {
				filtered = append(filtered, ex)
			} else {
				remaining = append(remaining, ex)
			}
		}
		rs.exchanges = remaining
	} else {
		filtered = make([]RecordedExchange, len(rs.exchanges))
		copy(filtered, rs.exchanges)
		rs.exchanges = make([]RecordedExchange, 0)
	}
	rs.mu.Unlock()

	// Convert to mappings — always use non-nil slice so JSON marshals
	// as [] not null (Cypress spreads this array and null is not iterable)
	mappings := make([]Mapping, 0)
	if snapReq.RepeatsAsScenarios {
		if m := exchangesToScenarioMappings(filtered); m != nil {
			mappings = m
		}
	} else {
		if m := exchangesToMappings(filtered); m != nil {
			mappings = m
		}
	}

	result := WiremockMappings{Mappings: mappings}
	data, _ := json.Marshal(result)
	ctx.Response.Header.Set("Content-Type", "application/json")
	ctx.SetStatusCode(fasthttp.StatusOK)
	ctx.SetBody(data)

	log.Printf("Snapshot returned %d mappings (filter: %q, scenarios: %v)",
		len(mappings), snapReq.Filters.URLPattern, snapReq.RepeatsAsScenarios)
}

// exchangesToMappings converts exchanges to mappings, deduplicating by
// method + path + query params + body (keeping the last occurrence).
// This matches WireMock's snapshot behavior with repeatsAsScenarios=false.
func exchangesToMappings(exchanges []RecordedExchange) []Mapping {
	type dedupEntry struct {
		key     string
		mapping Mapping
	}

	seen := make(map[string]int) // key -> index in entries
	var entries []dedupEntry

	for _, ex := range exchanges {
		m := exchangeToMapping(ex)
		key := deduplicationKey(m)

		if idx, exists := seen[key]; exists {
			// Replace with later occurrence
			entries[idx].mapping = m
		} else {
			seen[key] = len(entries)
			entries = append(entries, dedupEntry{key: key, mapping: m})
		}
	}

	mappings := make([]Mapping, 0, len(entries))
	for _, e := range entries {
		mappings = append(mappings, e.mapping)
	}
	return mappings
}

// deduplicationKey builds a key from a mapping's request fields for deduplication.
// Uses method + url/urlPath + sorted query params + body patterns.
func deduplicationKey(m Mapping) string {
	path := m.Request.URL
	if path == "" {
		path = m.Request.URLPath
	}

	key := m.Request.Method + " " + path

	// Append query parameters (deterministic order)
	if len(m.Request.QueryParameters) > 0 {
		qpJSON, _ := json.Marshal(m.Request.QueryParameters)
		key += " " + string(qpJSON)
	}

	// Append body patterns
	if len(m.Request.BodyPatterns) > 0 {
		bpJSON, _ := json.Marshal(m.Request.BodyPatterns)
		key += " " + string(bpJSON)
	}

	return key
}

// exchangesToScenarioMappings converts exchanges to mappings, creating scenarios for repeated URLs.
func exchangesToScenarioMappings(exchanges []RecordedExchange) []Mapping {
	// Group by URL+method
	type group struct {
		key       string
		exchanges []RecordedExchange
	}
	groups := make(map[string]*group)
	var order []string

	for _, ex := range exchanges {
		key := ex.Method + " " + ex.URL
		g, exists := groups[key]
		if !exists {
			g = &group{key: key}
			groups[key] = g
			order = append(order, key)
		}
		g.exchanges = append(g.exchanges, ex)
	}

	var mappings []Mapping
	for _, key := range order {
		g := groups[key]
		if len(g.exchanges) == 1 {
			// Single occurrence — no scenario needed
			mappings = append(mappings, exchangeToMapping(g.exchanges[0]))
		} else {
			// Multiple occurrences — create scenario chain
			scenarioName := generateMappingName(g.exchanges[0].URL)
			for i, ex := range g.exchanges {
				m := exchangeToMapping(ex)
				m.ScenarioName = scenarioName
				if i == 0 {
					m.RequiredScenarioState = "Started"
				} else {
					m.RequiredScenarioState = fmt.Sprintf("state_%d", i)
				}
				if i < len(g.exchanges)-1 {
					m.NewScenarioState = fmt.Sprintf("state_%d", i+1)
				}
				mappings = append(mappings, m)
			}
		}
	}
	return mappings
}

// exchangeToMapping converts a recorded exchange to a WireMock mapping.
func exchangeToMapping(ex RecordedExchange) Mapping {
	// Split URL into path and query parameters
	rawPath := ex.URL
	var queryString string
	if idx := strings.IndexByte(ex.URL, '?'); idx != -1 {
		rawPath = ex.URL[:idx]
		queryString = ex.URL[idx+1:]
	}

	name := generateMappingName(ex.URL)
	uuid := generateUUID()

	req := Request{
		Method: ex.Method,
	}

	// WireMock uses "url" (exact full URI) when there are no query params,
	// and "urlPath" + "queryParameters" when there are.
	if queryString != "" {
		req.URLPath = rawPath
		qp := parseQueryParams(queryString)
		if len(qp) > 0 {
			req.QueryParameters = qp
		}
	} else {
		req.URL = rawPath
	}

	// Add body pattern for requests with body
	if len(ex.ReqBody) > 0 {
		// Check if body is valid JSON — store as a JSON string (WireMock format)
		var js json.RawMessage
		if json.Unmarshal(ex.ReqBody, &js) == nil {
			// Compact the JSON and wrap as a quoted string
			compacted, err := compactJSON(ex.ReqBody)
			if err == nil {
				quoted, _ := json.Marshal(string(compacted))
				falseVal := false
				req.BodyPatterns = []BodyPattern{
					{
						EqualToJSON:         json.RawMessage(quoted),
						IgnoreArrayOrder:    &falseVal,
						IgnoreExtraElements: &falseVal,
					},
				}
			}
		}
	}

	// Build response headers, filtering hop-by-hop and internal headers
	headers := make(map[string]any)
	for key, values := range ex.RespHeaders {
		upperKey := strings.ToUpper(key)
		if strings.HasPrefix(upperKey, "X-GDC") || upperKey == "DATE" {
			continue
		}
		if upperKey == "CONTENT-ENCODING" || upperKey == "CONTENT-LENGTH" || upperKey == "CONNECTION" || upperKey == "TRANSFER-ENCODING" {
			continue
		}
		// Normalize header casing to match WireMock's output
		normalizedKey := normalizeHeaderName(key)
		if len(values) == 1 {
			headers[normalizedKey] = values[0]
		} else {
			ifaces := make([]interface{}, len(values))
			for i, v := range values {
				ifaces[i] = v
			}
			headers[normalizedKey] = ifaces
		}
	}

	return Mapping{
		ID:   uuid,
		UUID: uuid,
		Name: name,
		Request: req,
		Response: Response{
			Status:  ex.Status,
			Body:    string(ex.RespBody),
			Headers: headers,
		},
	}
}

// normalizeHeaderName converts a header name to HTTP canonical form (Title-Case),
// matching WireMock's header casing behavior.
func normalizeHeaderName(name string) string {
	// Use net/http's canonical form: "x-xss-protection" -> "X-Xss-Protection"
	// Then apply special-case overrides to match WireMock
	parts := strings.Split(name, "-")
	for i, part := range parts {
		if len(part) > 0 {
			upper := strings.ToUpper(part)
			// WireMock keeps certain abbreviations fully uppercased
			switch upper {
			case "XSS", "HTTP", "ID", "DNS", "CSP", "URI", "URL", "SSL", "TLS", "IP":
				parts[i] = upper
			default:
				parts[i] = strings.ToUpper(part[:1]) + strings.ToLower(part[1:])
			}
		}
	}
	return strings.Join(parts, "-")
}

// compactJSON compacts a JSON byte slice, removing unnecessary whitespace.
func compactJSON(data []byte) ([]byte, error) {
	var buf bytes.Buffer
	if err := json.Compact(&buf, data); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

// generateUUID generates a random UUID v4 string.
func generateUUID() string {
	b := make([]byte, 16)
	rand.Read(b)
	b[6] = (b[6] & 0x0f) | 0x40 // version 4
	b[8] = (b[8] & 0x3f) | 0x80 // variant
	return fmt.Sprintf("%08x-%04x-%04x-%04x-%012x", b[0:4], b[4:6], b[6:8], b[8:10], b[10:16])
}

// parseQueryParams parses a raw query string into WireMock QueryParamMatcher format.
// Values are URL-decoded to match WireMock's recording behavior.
func parseQueryParams(qs string) map[string]QueryParamMatcher {
	params := make(map[string][]string)
	var paramOrder []string

	for _, part := range strings.Split(qs, "&") {
		kv := strings.SplitN(part, "=", 2)
		key := urlDecode(kv[0])
		val := ""
		if len(kv) == 2 {
			val = urlDecode(kv[1])
		}
		if _, exists := params[key]; !exists {
			paramOrder = append(paramOrder, key)
		}
		params[key] = append(params[key], val)
	}

	result := make(map[string]QueryParamMatcher)
	for _, key := range paramOrder {
		values := params[key]
		matchers := make([]EqualMatcher, len(values))
		for i, v := range values {
			matchers[i] = EqualMatcher{EqualTo: v}
		}
		result[key] = QueryParamMatcher{HasExactly: matchers}
	}
	return result
}

// urlDecode decodes a percent-encoded string, returning the original on error.
func urlDecode(s string) string {
	decoded, err := url.QueryUnescape(s)
	if err != nil {
		return s
	}
	return decoded
}

// generateMappingName creates a WireMock-style name from a URL path.
func generateMappingName(rawURL string) string {
	path := rawURL
	if idx := strings.IndexByte(rawURL, '?'); idx != -1 {
		path = rawURL[:idx]
	}
	name := strings.TrimPrefix(path, "/")
	name = strings.ReplaceAll(name, "/", "_")
	name = strings.ReplaceAll(name, "%3A", "")
	name = strings.ReplaceAll(name, "%3a", "")
	name = strings.ToLower(name)
	return name
}

// negativeLookaheadRe matches patterns like ((?!SOMETHING).)*
var negativeLookaheadRe = regexp.MustCompile(`^\(?(?:\(\?\!(.+?)\)\.)\)\*$`)

// compileURLMatcher returns a function that matches URLs against the given pattern.
// Handles Perl-style negative lookahead patterns like ((?!executionResults).)*
// which Go's regexp doesn't support natively.
func compileURLMatcher(pattern string) func(string) bool {
	// Try standard Go regexp first
	re, err := regexp.Compile(pattern)
	if err == nil {
		return func(url string) bool {
			return re.MatchString(url)
		}
	}

	// Handle negative lookahead: ((?!INNER).)*  means "doesn't contain INNER"
	if m := negativeLookaheadRe.FindStringSubmatch(pattern); len(m) > 1 {
		inner, err := regexp.Compile(m[1])
		if err == nil {
			log.Printf("Using negative lookahead filter: excluding URLs matching %q", m[1])
			return func(url string) bool {
				return !inner.MatchString(url)
			}
		}
	}

	log.Printf("Warning: unsupported URL filter pattern: %s", pattern)
	return func(url string) bool {
		return true // pass everything through if we can't parse
	}
}

func runRecord() {
	port := getPort()

	upstream := os.Getenv("PROXY_HOST")
	if upstream == "" {
		fmt.Fprintf(os.Stderr, "PROXY_HOST environment variable is required in record mode\n")
		os.Exit(1)
	}

	refererPath := os.Getenv("REFERER_PATH")
	if refererPath == "" {
		refererPath = "/"
	}

	verbose := isVerbose()
	server := NewRecordServer(upstream, upstream, refererPath, verbose)

	addr := fmt.Sprintf(":%d", port)

	fmt.Println("┌──────────────────────────────────────────────────────────────────────────────┐")
	fmt.Println("|                                                                              |")
	fmt.Printf("|   GoodMock - Wiremock-compatible mock server (fasthttp)                      |\n")
	fmt.Printf("|   Mode: %-69s|\n", "record")
	fmt.Printf("|   Port: %-69d|\n", port)
	fmt.Printf("|   Upstream: %-66s|\n", upstream)
	fmt.Printf("|   Verbose: %-66v|\n", verbose)
	fmt.Println("|                                                                              |")
	fmt.Println("└──────────────────────────────────────────────────────────────────────────────┘")

	log.Fatal(fasthttp.ListenAndServe(addr, server.HandleRequest))
}
