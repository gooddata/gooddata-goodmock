// (C) 2025 GoodData Corporation
package main

import (
	"encoding/json"
	"sync"
)

// WiremockMappings represents the root structure of a Wiremock mapping file
type WiremockMappings struct {
	Mappings []Mapping `json:"mappings"`
}

// Mapping represents a single request-response mapping
type Mapping struct {
	ID                    string   `json:"id,omitempty"`
	UUID                  string   `json:"uuid,omitempty"`
	Name                  string   `json:"name,omitempty"`
	ScenarioName          string   `json:"scenarioName,omitempty"`
	RequiredScenarioState string   `json:"requiredScenarioState,omitempty"`
	NewScenarioState      string   `json:"newScenarioState,omitempty"`
	Request               Request  `json:"request"`
	Response              Response `json:"response"`
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
	IgnoreArrayOrder    *bool           `json:"ignoreArrayOrder,omitempty"`
	IgnoreExtraElements *bool           `json:"ignoreExtraElements,omitempty"`
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
	mu          sync.RWMutex
	mappings    []Mapping
	proxyHost   string
	refererPath string
	verbose     bool
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
