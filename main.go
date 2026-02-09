// (C) 2025 GoodData Corporation
package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"os"
	"regexp"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/valyala/fasthttp"
)

// WiremockMappings represents the root structure of a Wiremock mapping file
type WiremockMappings struct {
	Mappings []Mapping `json:"mappings"`
}

// Mapping represents a single request-response mapping
type Mapping struct {
	ID       string   `json:"id,omitempty"`
	UUID     string   `json:"uuid,omitempty"`
	Name     string   `json:"name,omitempty"`
	Request  Request  `json:"request"`
	Response Response `json:"response"`
}

// Request represents the request matching criteria
type Request struct {
	URL             string                       `json:"url,omitempty"`
	URLPath         string                       `json:"urlPath,omitempty"`
	URLPattern      string                       `json:"urlPattern,omitempty"`
	Method          string                       `json:"method"`
	QueryParameters map[string]QueryParamMatcher `json:"queryParameters,omitempty"`
	BodyPatterns    []BodyPattern                `json:"bodyPatterns,omitempty"`
	Headers         map[string]HeaderMatcher     `json:"headers,omitempty"`
}

// QueryParamMatcher represents a query parameter matcher
type QueryParamMatcher struct {
	EqualTo    string         `json:"equalTo,omitempty"`
	HasExactly []EqualMatcher `json:"hasExactly,omitempty"`
}

// EqualMatcher represents an equality matcher
type EqualMatcher struct {
	EqualTo string `json:"equalTo"`
}

// BodyPattern represents a request body pattern matcher
type BodyPattern struct {
	EqualToJSON         json.RawMessage `json:"equalToJson,omitempty"`
	IgnoreArrayOrder    bool            `json:"ignoreArrayOrder,omitempty"`
	IgnoreExtraElements bool            `json:"ignoreExtraElements,omitempty"`
}

// HeaderMatcher represents a header matcher
type HeaderMatcher struct {
	EqualTo  string `json:"equalTo,omitempty"`
	Contains string `json:"contains,omitempty"`
}

// Response represents the stub response
type Response struct {
	Status       int            `json:"status"`
	Body         string         `json:"body,omitempty"`
	Headers      map[string]any `json:"headers,omitempty"`
	ProxyBaseUrl string         `json:"proxyBaseUrl,omitempty"`
}

// Server holds the mock server state
type Server struct {
	mu        sync.RWMutex
	mappings  []Mapping
	proxyHost string
}

// NewServer creates a new mock server
func NewServer(proxyHost string) *Server {
	return &Server{
		mappings:  make([]Mapping, 0),
		proxyHost: proxyHost,
	}
}

// LoadMappings loads mappings from a JSON structure
func (s *Server) LoadMappings(wm WiremockMappings) {
	s.mu.Lock()
	s.mappings = append(s.mappings, wm.Mappings...)
	s.mu.Unlock()
}

// AddMapping adds a single mapping
func (s *Server) AddMapping(m Mapping) {
	s.mu.Lock()
	s.mappings = append(s.mappings, m)
	s.mu.Unlock()
}

// ClearMappings clears all loaded mappings
func (s *Server) ClearMappings() {
	s.mu.Lock()
	s.mappings = make([]Mapping, 0)
	s.mu.Unlock()
}

// MatchResult holds the result of matching a request against a stub
type MatchResult struct {
	Matched     bool
	Mapping     *Mapping
	URLMatch    bool
	MethodMatch bool
	QueryMatch  bool
	BodyMatch   bool
	HeaderMatch bool
	QueryDiffs  []string
	BodyDiff    string
	HeaderDiffs []string
}

const colWidth = 58

// matchRequest finds a matching stub for the incoming request
func (s *Server) matchRequest(method, path, fullURI string, queryArgs *fasthttp.Args, body []byte, reqHeaders *fasthttp.RequestHeader) MatchResult {
	s.mu.RLock()
	defer s.mu.RUnlock()

	var bestMatch MatchResult
	var bestScore int

	for i := range s.mappings {
		m := &s.mappings[i]
		result := s.evaluateMapping(m, method, path, fullURI, queryArgs, body, reqHeaders)

		score := 0
		if result.MethodMatch {
			score += 1
		}
		if result.URLMatch {
			score += 2
		}
		if result.QueryMatch {
			score += 4
		}
		if result.BodyMatch {
			score += 8
		}
		if result.HeaderMatch {
			score += 16
		}

		if result.Matched {
			result.Mapping = m
			return result
		}

		if score > bestScore {
			bestScore = score
			bestMatch = result
			bestMatch.Mapping = m
		}
	}

	return bestMatch
}

// evaluateMapping checks how well a mapping matches the request
func (s *Server) evaluateMapping(m *Mapping, method, path, fullURI string, queryArgs *fasthttp.Args, body []byte, reqHeaders *fasthttp.RequestHeader) MatchResult {
	result := MatchResult{}

	// Check method - "ANY" matches all methods
	result.MethodMatch = strings.EqualFold(m.Request.Method, method) || strings.EqualFold(m.Request.Method, "ANY")

	// Check URL/path
	// In WireMock, "url" matches the full URI (path + query string),
	// while "urlPath" matches just the path component.
	if m.Request.URL != "" {
		result.URLMatch = m.Request.URL == fullURI
	} else if m.Request.URLPath != "" {
		result.URLMatch = m.Request.URLPath == path
	} else if m.Request.URLPattern != "" {
		// urlPattern in WireMock matches against the full URI (path + query string)
		re, err := regexp.Compile(m.Request.URLPattern)
		if err == nil {
			result.URLMatch = re.MatchString(fullURI)
		}
	}

	// Check query parameters
	if len(m.Request.QueryParameters) == 0 {
		result.QueryMatch = true
	} else {
		result.QueryMatch = true
		result.QueryDiffs = make([]string, 0)

		for paramName, matcher := range m.Request.QueryParameters {
			expectedValues := getExpectedValues(matcher)

			var actualValues []string
			queryArgs.VisitAll(func(key, value []byte) {
				if string(key) == paramName {
					actualValues = append(actualValues, string(value))
				}
			})

			if !matchQueryParam(expectedValues, actualValues) {
				result.QueryMatch = false
				if len(actualValues) == 0 {
					result.QueryDiffs = append(result.QueryDiffs,
						fmt.Sprintf("not_present|%s|%v", paramName, expectedValues))
				} else {
					result.QueryDiffs = append(result.QueryDiffs,
						fmt.Sprintf("mismatch|%s|%v|%s", paramName, expectedValues, strings.Join(actualValues, ",")))
				}
			}
		}
	}

	// Check body patterns
	if len(m.Request.BodyPatterns) == 0 {
		result.BodyMatch = true
	} else {
		result.BodyMatch = matchBodyPatterns(m.Request.BodyPatterns, body)
		if !result.BodyMatch {
			result.BodyDiff = "Body does not match"
		}
	}

	// Check headers
	if len(m.Request.Headers) == 0 {
		result.HeaderMatch = true
	} else {
		result.HeaderMatch = true
		result.HeaderDiffs = make([]string, 0)

		for headerName, matcher := range m.Request.Headers {
			actualValue := string(reqHeaders.Peek(headerName))
			if !matchHeader(matcher, actualValue) {
				result.HeaderMatch = false
				if actualValue == "" {
					result.HeaderDiffs = append(result.HeaderDiffs,
						fmt.Sprintf("not_present|%s|%s", headerName, matcher.EqualTo))
				} else {
					result.HeaderDiffs = append(result.HeaderDiffs,
						fmt.Sprintf("mismatch|%s|%s|%s", headerName, matcher.EqualTo, actualValue))
				}
			}
		}
	}

	result.Matched = result.MethodMatch && result.URLMatch && result.QueryMatch && result.BodyMatch && result.HeaderMatch
	return result
}

// matchBodyPatterns checks if the request body matches all body patterns
func matchBodyPatterns(patterns []BodyPattern, body []byte) bool {
	for _, pattern := range patterns {
		if pattern.EqualToJSON != nil {
			if !jsonEqual(pattern.EqualToJSON, body) {
				return false
			}
		}
	}
	return true
}

// jsonEqual compares two JSON values for equality.
// In WireMock mappings, equalToJson can be either a JSON object or a JSON string
// containing JSON (e.g. "{\"key\":\"value\"}"). We handle both cases.
func jsonEqual(expected json.RawMessage, actual []byte) bool {
	var expectedVal, actualVal interface{}
	if err := json.Unmarshal(expected, &expectedVal); err != nil {
		return false
	}
	// If equalToJson was stored as a string, parse it as JSON
	if str, ok := expectedVal.(string); ok {
		if err := json.Unmarshal([]byte(str), &expectedVal); err != nil {
			return false
		}
	}
	if err := json.Unmarshal(actual, &actualVal); err != nil {
		return false
	}
	expectedNorm, err1 := json.Marshal(expectedVal)
	actualNorm, err2 := json.Marshal(actualVal)
	if err1 != nil || err2 != nil {
		return false
	}
	return string(expectedNorm) == string(actualNorm)
}

// matchHeader checks if an actual header value matches the expected matcher
func matchHeader(matcher HeaderMatcher, actual string) bool {
	if matcher.EqualTo != "" {
		return matcher.EqualTo == actual
	}
	if matcher.Contains != "" {
		return strings.Contains(actual, matcher.Contains)
	}
	return true
}

// getExpectedValues extracts expected values from a query param matcher
func getExpectedValues(matcher QueryParamMatcher) []string {
	if matcher.EqualTo != "" {
		return []string{matcher.EqualTo}
	}

	values := make([]string, 0, len(matcher.HasExactly))
	for _, m := range matcher.HasExactly {
		values = append(values, m.EqualTo)
	}
	return values
}

// matchQueryParam checks if actual values match expected values
func matchQueryParam(expected, actual []string) bool {
	if len(expected) != len(actual) {
		return false
	}

	sortedExpected := make([]string, len(expected))
	sortedActual := make([]string, len(actual))
	copy(sortedExpected, expected)
	copy(sortedActual, actual)
	sort.Strings(sortedExpected)
	sort.Strings(sortedActual)

	for i := range sortedExpected {
		if sortedExpected[i] != sortedActual[i] {
			return false
		}
	}
	return true
}

// logMismatch outputs a request mismatch in the same format as WireMock
func (s *Server) logMismatch(method, fullURL string, result MatchResult) {
	separator := strings.Repeat("-", 119)
	timestamp := time.Now().UTC().Format("2006-01-02 15:04:05.000")

	fmt.Printf("%s \n", timestamp)
	fmt.Println("                                               Request was not matched")
	fmt.Println("                                               =======================")
	fmt.Println()
	fmt.Println(separator)
	fmt.Printf("| %-*s | %-*s |\n", colWidth, "Closest stub", colWidth, "Request")
	fmt.Println(separator)

	if result.Mapping != nil {
		m := result.Mapping

		// Stub name
		fmt.Printf("%-*s |\n", colWidth+1, "")
		if m.Name != "" {
			fmt.Printf("%-*s |\n", colWidth+1, " "+m.Name)
		}
		fmt.Printf("%-*s |\n", colWidth+1, "")

		// Method
		fmt.Printf("%-*s | %s\n", colWidth, " "+m.Request.Method, method)

		// Path comparison
		expectedPath := m.Request.URL
		if expectedPath == "" {
			expectedPath = m.Request.URLPath
		}
		if expectedPath == "" {
			expectedPath = m.Request.URLPattern
		}

		if result.URLMatch {
			fmt.Printf(" [path] %-*s | %-*s\n",
				colWidth-8, truncate(expectedPath, colWidth-8),
				colWidth, truncate(fullURL, colWidth))
		} else {
			// Split the actual URL across lines if needed
			actualTrunc := truncate(fullURL, colWidth-6)
			remainder := ""
			if len(fullURL) > colWidth-6 {
				remainder = fullURL[colWidth-6:]
			}
			fmt.Printf(" [path] %-*s | %s<<<<< URL does not match\n",
				colWidth-8, truncate(expectedPath, colWidth-8),
				actualTrunc)
			if remainder != "" {
				fmt.Printf(" %-*s | %s\n", colWidth-1, "", remainder)
			}
		}
		fmt.Printf("%-*s |\n", colWidth+1, "")

		// Query parameter diffs
		for _, diff := range result.QueryDiffs {
			parts := strings.SplitN(diff, "|", 4)
			diffType := parts[0]
			paramName := parts[1]
			expectedVals := parts[2]

			stubCol := fmt.Sprintf(" Query: %s exactly %s", paramName, expectedVals)
			if diffType == "not_present" {
				fmt.Printf("%-*s | %s<<<<< Query is not present\n",
					colWidth, truncate(stubCol, colWidth),
					strings.Repeat(" ", colWidth-5-len("<<<<< Query is not present")+6))
			} else {
				actualVals := parts[3]
				actualCol := fmt.Sprintf("%s: %s", paramName, actualVals)
				fmt.Printf("%-*s | %-*s<<<<< Query does not match\n",
					colWidth, truncate(stubCol, colWidth),
					colWidth-26, truncate(actualCol, colWidth-26))
			}
		}

		// Header diffs
		for _, diff := range result.HeaderDiffs {
			parts := strings.SplitN(diff, "|", 4)
			diffType := parts[0]
			headerName := parts[1]
			expectedVal := parts[2]

			stubCol := fmt.Sprintf(" Header: %s [equalTo %s]", headerName, expectedVal)
			if diffType == "not_present" {
				fmt.Printf("%-*s | %s<<<<< Header is not present\n",
					colWidth, truncate(stubCol, colWidth),
					strings.Repeat(" ", colWidth-5-len("<<<<< Header is not present")+6))
			} else {
				actualVal := parts[3]
				actualCol := fmt.Sprintf("%s: %s", headerName, actualVal)
				fmt.Printf("%-*s | %-*s<<<<< Header does not match\n",
					colWidth, truncate(stubCol, colWidth),
					colWidth-27, truncate(actualCol, colWidth-27))
			}
		}

		// Body diff
		if result.BodyDiff != "" {
			fmt.Printf(" %-*s | <<<<< %s\n", colWidth-1, "Body [equalToJson]", result.BodyDiff)
		}
	} else {
		fmt.Printf(" No stub found for: %s %s\n", method, fullURL)
	}

	fmt.Printf("%-*s |\n", colWidth+1, "")
	fmt.Printf("%-*s |\n", colWidth+1, "")
	fmt.Println(separator)
	fmt.Println()
}

func truncate(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen-3] + "..."
}

// applyResponseHeaders applies response header transformations
func (s *Server) applyResponseHeaders(ctx *fasthttp.RequestCtx, headers map[string]any) {
	for key, value := range headers {
		// Skip X-GDC... and Date headers
		upperKey := strings.ToUpper(key)
		if strings.HasPrefix(upperKey, "X-GDC") || upperKey == "DATE" {
			continue
		}

		switch v := value.(type) {
		case []interface{}:
			for _, item := range v {
				if str, ok := item.(string); ok {
					ctx.Response.Header.Add(key, str)
				}
			}
		case string:
			ctx.Response.Header.Set(key, v)
		}
	}
}

// HandleRequest handles incoming HTTP requests
func (s *Server) HandleRequest(ctx *fasthttp.RequestCtx) {
	path := string(ctx.Path())
	method := string(ctx.Method())

	// Admin API endpoints
	if strings.HasPrefix(path, "/__admin") {
		s.handleAdmin(ctx, path, method)
		return
	}

	body := ctx.PostBody()

	// Build the full URI (path + query string) for WireMock-compatible "url" matching
	fullURI := path
	qs := string(ctx.QueryArgs().QueryString())
	if qs != "" {
		fullURI = path + "?" + qs
	}

	// Find matching stub
	result := s.matchRequest(method, path, fullURI, ctx.QueryArgs(), body, &ctx.Request.Header)

	if !result.Matched {
		s.logMismatch(method, fullURI, result)
		ctx.SetStatusCode(fasthttp.StatusNotFound)
		ctx.SetBodyString(`{"error": "No matching stub found"}`)
		return
	}

	// Apply response
	m := result.Mapping

	// Set headers
	s.applyResponseHeaders(ctx, m.Response.Headers)

	ctx.SetStatusCode(m.Response.Status)
	if m.Response.Body != "" {
		ctx.SetBodyString(m.Response.Body)
	}
}

// handleAdmin handles all admin API endpoints
func (s *Server) handleAdmin(ctx *fasthttp.RequestCtx, path, method string) {
	// Health/ready check - GET /__admin
	if path == "/__admin" && method == "GET" {
		ctx.SetStatusCode(fasthttp.StatusOK)
		ctx.SetBodyString(`{"status":"ok"}`)
		return
	}

	// Health endpoint
	if path == "/__admin/health" {
		ctx.SetStatusCode(fasthttp.StatusOK)
		ctx.SetBodyString(`{"status":"ok"}`)
		return
	}

	// Reset mappings - POST /__admin/reset
	if path == "/__admin/reset" && method == "POST" {
		s.ClearMappings()
		log.Println("All mappings reset")
		ctx.SetStatusCode(fasthttp.StatusOK)
		return
	}

	// Settings - POST /__admin/settings (just acknowledge)
	if path == "/__admin/settings" && method == "POST" {
		ctx.SetStatusCode(fasthttp.StatusOK)
		return
	}

	// Scenarios reset - POST /__admin/scenarios/reset
	if path == "/__admin/scenarios/reset" && method == "POST" {
		ctx.SetStatusCode(fasthttp.StatusOK)
		ctx.SetBodyString(`{}`)
		return
	}

	// Mappings management
	if path == "/__admin/mappings" {
		s.handleMappings(ctx, method)
		return
	}

	// Import mappings - POST /__admin/mappings/import
	if path == "/__admin/mappings/import" && method == "POST" {
		var wm WiremockMappings
		if err := json.Unmarshal(ctx.PostBody(), &wm); err != nil {
			ctx.SetStatusCode(fasthttp.StatusBadRequest)
			ctx.SetBodyString(err.Error())
			return
		}

		s.LoadMappings(wm)
		log.Printf("Imported %d mappings", len(wm.Mappings))
		ctx.SetStatusCode(fasthttp.StatusOK)
		return
	}

	// Reset mappings (alternate endpoint)
	if path == "/__admin/mappings/reset" && method == "POST" {
		s.ClearMappings()
		ctx.SetStatusCode(fasthttp.StatusOK)
		return
	}

	// Delete requests log - DELETE /__admin/requests
	if path == "/__admin/requests" && method == "DELETE" {
		ctx.SetStatusCode(fasthttp.StatusOK)
		return
	}

	// Recordings snapshot (for recording mode - stub implementation)
	if path == "/__admin/recordings/snapshot" && method == "POST" {
		ctx.Response.Header.Set("Content-Type", "application/json")
		ctx.SetStatusCode(fasthttp.StatusOK)
		ctx.SetBodyString(`{"mappings":[]}`)
		return
	}

	// Unknown admin endpoint
	log.Printf("Unknown admin endpoint: %s %s", method, path)
	ctx.SetStatusCode(fasthttp.StatusNotFound)
}

// handleMappings handles the /__admin/mappings endpoint
func (s *Server) handleMappings(ctx *fasthttp.RequestCtx, method string) {
	switch method {
	case "POST":
		var m Mapping
		if err := json.Unmarshal(ctx.PostBody(), &m); err != nil {
			ctx.SetStatusCode(fasthttp.StatusBadRequest)
			ctx.SetBodyString(err.Error())
			return
		}

		s.AddMapping(m)
		log.Printf("Added mapping: %s %s", m.Request.Method, getRequestPattern(&m))
		ctx.SetStatusCode(fasthttp.StatusCreated)

	case "DELETE":
		s.ClearMappings()
		ctx.SetStatusCode(fasthttp.StatusOK)

	case "GET":
		s.mu.RLock()
		wm := WiremockMappings{Mappings: s.mappings}
		s.mu.RUnlock()

		ctx.Response.Header.Set("Content-Type", "application/json")
		data, _ := json.Marshal(wm)
		ctx.SetBody(data)

	default:
		ctx.SetStatusCode(fasthttp.StatusMethodNotAllowed)
	}
}

func getRequestPattern(m *Mapping) string {
	if m.Request.URL != "" {
		return m.Request.URL
	}
	if m.Request.URLPath != "" {
		return m.Request.URLPath
	}
	return m.Request.URLPattern
}

func main() {
	port := flag.Int("port", 8080, "Port to listen on")
	flag.Parse()

	proxyHost := os.Getenv("PROXY_HOST")
	if proxyHost == "" {
		proxyHost = "http://localhost"
	}

	server := NewServer(proxyHost)

	// Load mappings from MAPPINGS_DIR env if set
	mappingsDir := os.Getenv("MAPPINGS_DIR")
	if mappingsDir != "" {
		entries, err := os.ReadDir(mappingsDir)
		if err != nil {
			log.Printf("Warning: Could not read mappings directory %s: %v", mappingsDir, err)
		} else {
			for _, entry := range entries {
				if entry.IsDir() {
					continue
				}
				if strings.HasSuffix(entry.Name(), ".json") {
					filePath := mappingsDir + "/" + entry.Name()
					data, err := os.ReadFile(filePath)
					if err != nil {
						log.Printf("Warning: Could not read mapping file %s: %v", filePath, err)
						continue
					}
					var wm WiremockMappings
					if err := json.Unmarshal(data, &wm); err != nil {
						log.Printf("Warning: Could not parse mapping file %s: %v", filePath, err)
					} else {
						server.LoadMappings(wm)
						log.Printf("Loaded %d mappings from %s", len(wm.Mappings), filePath)
					}
				}
			}
		}
	}

	addr := fmt.Sprintf(":%d", *port)

	fmt.Println("┌──────────────────────────────────────────────────────────────────────────────┐")
	fmt.Println("|                                                                              |")
	fmt.Printf("|   GoodMock - Wiremock-compatible mock server (fasthttp)                      |\n")
	fmt.Printf("|   Port: %-69d|\n", *port)
	fmt.Println("|                                                                              |")
	fmt.Println("└──────────────────────────────────────────────────────────────────────────────┘")

	log.Fatal(fasthttp.ListenAndServe(addr, server.HandleRequest))
}
