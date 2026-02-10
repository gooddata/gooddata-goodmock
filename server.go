// (C) 2025 GoodData Corporation
package main

import (
	"encoding/json"
	"log"
	"strings"

	"github.com/valyala/fasthttp"
)

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
