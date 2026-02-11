// (C) 2025 GoodData Corporation
package main

import (
	"encoding/json"
	"fmt"
	"regexp"
	"sort"
	"strings"

	"github.com/valyala/fasthttp"
)

// matchRequest finds the best matching stub for the incoming request.
// When multiple mappings match, returns the most specific one (most query params + body patterns + headers).
func (s *Server) matchRequest(method, path, fullURI string, queryArgs *fasthttp.Args, body []byte, reqHeaders *fasthttp.RequestHeader) MatchResult {
	s.mu.RLock()
	defer s.mu.RUnlock()

	var bestMatch MatchResult
	var bestScore int
	bestMatched := false

	for i := range s.mappings {
		m := &s.mappings[i]
		result := s.evaluateMapping(m, method, path, fullURI, queryArgs, body, reqHeaders)

		if result.Matched {
			// Calculate specificity: more criteria = more specific
			specificity := len(m.Request.QueryParameters) + len(m.Request.BodyPatterns) + len(m.Request.Headers)
			// URL exact match (includes query string) is more specific than urlPath
			if m.Request.URL != "" {
				specificity += 100
			}

			if !bestMatched || specificity > bestScore {
				bestMatched = true
				bestScore = specificity
				bestMatch = result
				bestMatch.Mapping = m
			}
		} else if !bestMatched {
			// Track closest non-match for diagnostics
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
			if score > bestScore {
				bestScore = score
				bestMatch = result
				bestMatch.Mapping = m
			}
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
