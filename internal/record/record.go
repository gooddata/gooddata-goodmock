// (C) 2025 GoodData Corporation
package record

import (
	"bytes"
	"encoding/json"
	"fmt"
	"goodmock/internal/common"
	"goodmock/internal/jsonutil"
	"goodmock/internal/proxy"
	"goodmock/internal/server"
	"goodmock/internal/types"
	"log"
	"net/url"
	"os"
	"regexp"
	"sort"
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
	server           *types.Server
	mu               sync.Mutex
	exchanges        []RecordedExchange
	upstream         string
	client           *fasthttp.Client
	jsonContentTypes []string
	preserveKeyOrder bool
	sortArrayMembers bool
}

// NewRecordServer creates a new recording proxy server.
func NewRecordServer(upstream, proxyHost, refererPath string, verbose bool, jsonContentTypes []string, preserveKeyOrder, sortArrayMembers bool) *RecordServer {
	return &RecordServer{
		server:           server.NewServer(proxyHost, refererPath, verbose),
		exchanges:        make([]RecordedExchange, 0),
		upstream:         upstream,
		client:           &fasthttp.Client{},
		jsonContentTypes: jsonContentTypes,
		preserveKeyOrder: preserveKeyOrder,
		sortArrayMembers: sortArrayMembers,
	}
}

// handleRecordRequest routes admin requests locally, then proxies+records everything else.
func handleRecordRequest(rs *RecordServer, ctx *fasthttp.RequestCtx) {
	rawURI := string(ctx.RequestURI())
	path := rawURI
	if idx := strings.IndexByte(rawURI, '?'); idx != -1 {
		path = rawURI[:idx]
	}
	method := string(ctx.Method())

	// Admin endpoints handled locally
	if strings.HasPrefix(path, "/__admin") {
		handleRecordAdmin(rs, ctx, path, method)
		return
	}

	if rs.server.Verbose {
		server.LogVerboseRequest(ctx, method, rawURI)
	}

	// Transform request headers before proxying
	server.TransformRequestHeaders(&ctx.Request.Header, rs.server.ProxyHost, rs.server.RefererPath)

	// In record mode, always proxy and record — no stub matching
	proxyAndRecord(rs, ctx)
}

// proxyAndRecord forwards the request to upstream, records the exchange, and returns the response.
func proxyAndRecord(rs *RecordServer, ctx *fasthttp.RequestCtx) {
	status, respHeaders, body, err := proxy.ProxyRequest(rs.client, rs.upstream, ctx)
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

	if rs.server.Verbose {
		log.Printf("[verbose] << %d %s %s (%d bytes)", status, string(ctx.Method()), string(ctx.RequestURI()), len(body))
	}
}

func clearExchanges(rs *RecordServer) {
	rs.mu.Lock()
	rs.exchanges = make([]RecordedExchange, 0)
	rs.mu.Unlock()
}

func handleRecordAdmin(rs *RecordServer, ctx *fasthttp.RequestCtx, path, method string) {
	// Snapshot is record-mode specific
	if path == "/__admin/recordings/snapshot" && method == "POST" {
		handleSnapshot(rs, ctx)
		return
	}

	// Reset clears both stubs and recordings
	if (path == "/__admin/reset" || path == "/__admin/mappings/reset") && method == "POST" {
		server.ClearMappings(rs.server)
		clearExchanges(rs)
		log.Println("All mappings and recordings reset")
		ctx.SetStatusCode(fasthttp.StatusOK)
		return
	}

	// DELETE requests clears recordings
	if path == "/__admin/requests" && method == "DELETE" {
		clearExchanges(rs)
		ctx.SetStatusCode(fasthttp.StatusOK)
		return
	}

	// Delegate everything else to the replay server's admin handler
	server.HandleAdmin(rs.server, ctx, path, method)
}

// SnapshotRequest represents the body of a POST /__admin/recordings/snapshot request.
type SnapshotRequest struct {
	Filters struct {
		URLPattern string `json:"urlPattern,omitempty"`
	} `json:"filters"`
	Persist            bool `json:"persist"`
	RepeatsAsScenarios bool `json:"repeatsAsScenarios"`
}

func handleSnapshot(rs *RecordServer, ctx *fasthttp.RequestCtx) {
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
	mappings := make([]types.Mapping, 0)
	if snapReq.RepeatsAsScenarios {
		if m := exchangesToScenarioMappings(filtered, rs.jsonContentTypes, rs.preserveKeyOrder, rs.sortArrayMembers); m != nil {
			mappings = m
		}
	} else {
		if m := exchangesToMappings(filtered, rs.jsonContentTypes, rs.preserveKeyOrder, rs.sortArrayMembers); m != nil {
			mappings = m
		}
	}

	result := types.WiremockMappings{Mappings: mappings}
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
func exchangesToMappings(exchanges []RecordedExchange, jsonContentTypes []string, preserveKeyOrder, sortArrayMembers bool) []types.Mapping {
	type dedupEntry struct {
		key     string
		mapping types.Mapping
	}

	seen := make(map[string]int) // key -> index in entries
	var entries []dedupEntry

	for _, ex := range exchanges {
		m := exchangeToMapping(ex, jsonContentTypes, preserveKeyOrder, sortArrayMembers)
		key := deduplicationKey(m)

		if idx, exists := seen[key]; exists {
			// Replace with later occurrence
			entries[idx].mapping = m
		} else {
			seen[key] = len(entries)
			entries = append(entries, dedupEntry{key: key, mapping: m})
		}
	}

	mappings := make([]types.Mapping, 0, len(entries))
	for _, e := range entries {
		mappings = append(mappings, e.mapping)
	}
	sortMappings(mappings)
	return mappings
}

// deduplicationKey builds a key from a mapping's request fields for deduplication.
// Uses method + url/urlPath + sorted query params + body patterns.
func deduplicationKey(m types.Mapping) string {
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
func exchangesToScenarioMappings(exchanges []RecordedExchange, jsonContentTypes []string, preserveKeyOrder, sortArrayMembers bool) []types.Mapping {
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

	var mappings []types.Mapping
	for _, key := range order {
		g := groups[key]
		if len(g.exchanges) == 1 {
			// Single occurrence — no scenario needed
			mappings = append(mappings, exchangeToMapping(g.exchanges[0], jsonContentTypes, preserveKeyOrder, sortArrayMembers))
		} else {
			// Multiple occurrences — create scenario chain
			scenarioName := generateMappingName(g.exchanges[0].URL)
			for i, ex := range g.exchanges {
				m := exchangeToMapping(ex, jsonContentTypes, preserveKeyOrder, sortArrayMembers)
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
	sortMappings(mappings)
	return mappings
}

// sortMappings sorts mappings by name, using the deduplication key as tiebreaker
// for mappings with identical names.
func sortMappings(mappings []types.Mapping) {
	sort.SliceStable(mappings, func(i, j int) bool {
		if mappings[i].Name != mappings[j].Name {
			return mappings[i].Name < mappings[j].Name
		}
		return deduplicationKey(mappings[i]) < deduplicationKey(mappings[j])
	})
}

// exchangeToMapping converts a recorded exchange to a WireMock mapping.
func exchangeToMapping(ex RecordedExchange, jsonContentTypes []string, preserveKeyOrder, sortArrayMembers bool) types.Mapping {
	// Split URL into path and query parameters
	rawPath := ex.URL
	var queryString string
	if idx := strings.IndexByte(ex.URL, '?'); idx != -1 {
		rawPath = ex.URL[:idx]
		queryString = ex.URL[idx+1:]
	}

	name := generateMappingName(ex.URL)

	req := types.Request{
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
		var bodyBytes []byte
		if preserveKeyOrder {
			if sortArrayMembers {
				var parsed any
				if json.Unmarshal(ex.ReqBody, &parsed) == nil {
					parsed = jsonutil.SortArrays(parsed)
					if b, err := json.Marshal(parsed); err == nil {
						bodyBytes = b
					}
				}
			} else {
				compacted, err := compactJSON(ex.ReqBody)
				if err == nil {
					bodyBytes = compacted
				}
			}
		} else {
			var parsed any
			if json.Unmarshal(ex.ReqBody, &parsed) == nil {
				if sortArrayMembers {
					parsed = jsonutil.SortArrays(parsed)
				}
				if b, err := json.Marshal(parsed); err == nil {
					bodyBytes = b
				}
			}
		}
		if bodyBytes != nil {
			quoted, _ := json.Marshal(string(bodyBytes))
			falseVal := false
			req.BodyPatterns = []types.BodyPattern{
				{
					EqualToJSON:         json.RawMessage(quoted),
					IgnoreArrayOrder:    &falseVal,
					IgnoreExtraElements: &falseVal,
				},
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

	resp := types.Response{
		Status:  ex.Status,
		Headers: headers,
	}

	// Store as structured JSON if Content-Type matches, otherwise as string
	if isJSONContentType(ex.RespHeaders, jsonContentTypes) {
		if preserveKeyOrder && !sortArrayMembers {
			// Use json.RawMessage to preserve original key order from upstream
			var raw json.RawMessage
			if json.Unmarshal(ex.RespBody, &raw) == nil {
				resp.JsonBody = raw
			} else {
				resp.Body = string(ex.RespBody)
			}
		} else {
			// Unmarshal to interface{} — json.Marshal will sort keys alphabetically
			var parsed any
			if json.Unmarshal(ex.RespBody, &parsed) == nil {
				if sortArrayMembers {
					parsed = jsonutil.SortArrays(parsed)
				}
				resp.JsonBody = parsed
			} else {
				resp.Body = string(ex.RespBody)
			}
		}
	} else {
		resp.Body = string(ex.RespBody)
	}

	return types.Mapping{
		Name:     name,
		Request:  req,
		Response: resp,
	}
}

// isJSONContentType checks if the response Content-Type matches any of the given JSON types.
func isJSONContentType(headers map[string][]string, jsonTypes []string) bool {
	for key, values := range headers {
		if !strings.EqualFold(key, "Content-Type") {
			continue
		}
		for _, v := range values {
			mediaType := strings.TrimSpace(strings.SplitN(v, ";", 2)[0])
			for _, jt := range jsonTypes {
				if strings.EqualFold(mediaType, jt) {
					return true
				}
			}
		}
	}
	return false
}

// normalizeHeaderName converts a header name to HTTP canonical form (Title-Case),
// matching WireMock's header casing behavior.
func normalizeHeaderName(name string) string {
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

// parseQueryParams parses a raw query string into WireMock QueryParamMatcher format.
// Values are URL-decoded to match WireMock's recording behavior.
func parseQueryParams(qs string) map[string]types.QueryParamMatcher {
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

	result := make(map[string]types.QueryParamMatcher)
	for _, key := range paramOrder {
		values := params[key]
		matchers := make([]types.EqualMatcher, len(values))
		for i, v := range values {
			matchers[i] = types.EqualMatcher{EqualTo: v}
		}
		result[key] = types.QueryParamMatcher{HasExactly: matchers}
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

func RunRecord() {
	port := common.GetPort()

	upstream := os.Getenv("PROXY_HOST")
	if upstream == "" {
		fmt.Fprintf(os.Stderr, "PROXY_HOST environment variable is required in record mode\n")
		os.Exit(1)
	}

	refererPath := os.Getenv("REFERER_PATH")
	if refererPath == "" {
		refererPath = "/"
	}

	verbose := common.IsVerbose()
	jsonContentTypes := common.ParseJSONContentTypes()
	preserveKeyOrder := common.PreserveJSONKeyOrder()
	sortArrayMembers := common.SortArrayMembers()
	rs := NewRecordServer(upstream, upstream, refererPath, verbose, jsonContentTypes, preserveKeyOrder, sortArrayMembers)

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

	log.Fatal(fasthttp.ListenAndServe(addr, func(ctx *fasthttp.RequestCtx) {
		handleRecordRequest(rs, ctx)
	}))
}
