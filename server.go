// (C) 2025 GoodData Corporation
package main

import (
	"encoding/json"
	"fmt"
	"log"
	"strings"

	"github.com/valyala/fasthttp"
)

// NewServer creates a new mock server
func NewServer(proxyHost, refererPath string, verbose bool) *Server {
	return &Server{
		mappings:    make([]Mapping, 0),
		proxyHost:   proxyHost,
		refererPath: refererPath,
		verbose:     verbose,
	}
}

func loadMappings(s *Server, wm WiremockMappings) {
	s.mu.Lock()
	s.mappings = append(s.mappings, wm.Mappings...)
	s.mu.Unlock()
}

func addMapping(s *Server, m Mapping) {
	s.mu.Lock()
	s.mappings = append(s.mappings, m)
	s.mu.Unlock()
}

func clearMappings(s *Server) {
	s.mu.Lock()
	s.mappings = make([]Mapping, 0)
	s.mu.Unlock()
}

// transformRequestHeaders rewrites incoming request headers to match recorded stubs.
func transformRequestHeaders(h *fasthttp.RequestHeader, proxyHost, refererPath string) {
	if proxyHost != "" {
		h.Set("Origin", proxyHost)
		h.Set("Referer", proxyHost+refererPath)
	}
	h.Set("Accept-Encoding", "gzip")
}

// applyResponseHeaders writes response headers to the context, filtering internal ones.
func applyResponseHeaders(ctx *fasthttp.RequestCtx, headers map[string]any) {
	for key, value := range headers {
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

// handleRequest handles incoming HTTP requests
func handleRequest(s *Server, ctx *fasthttp.RequestCtx) {
	rawURI := string(ctx.RequestURI())
	path := rawURI
	if idx := strings.IndexByte(rawURI, '?'); idx != -1 {
		path = rawURI[:idx]
	}
	method := string(ctx.Method())

	if strings.HasPrefix(path, "/__admin") {
		handleAdmin(s, ctx, path, method)
		return
	}

	if s.verbose {
		logVerboseRequest(ctx, method, rawURI)
	}

	transformRequestHeaders(&ctx.Request.Header, s.proxyHost, s.refererPath)

	body := ctx.PostBody()
	fullURI := rawURI

	result := matchRequest(s, method, path, fullURI, ctx.QueryArgs(), body, &ctx.Request.Header)

	if !result.Matched {
		logMismatch(method, fullURI, result)
		ctx.SetStatusCode(fasthttp.StatusNotFound)
		ctx.SetBodyString(`{"error": "No matching stub found"}`)
		return
	}

	m := result.Mapping
	applyResponseHeaders(ctx, m.Response.Headers)

	ctx.SetStatusCode(m.Response.Status)
	if m.Response.Body != "" {
		ctx.SetBodyString(m.Response.Body)
	}

	if s.verbose {
		log.Printf("[verbose] << %d %s", m.Response.Status, method+" "+rawURI)
	}
}

func handleAdmin(s *Server, ctx *fasthttp.RequestCtx, path, method string) {
	if path == "/__admin" && method == "GET" {
		ctx.SetStatusCode(fasthttp.StatusOK)
		ctx.SetBodyString(`{"status":"ok"}`)
		return
	}

	if path == "/__admin/health" {
		ctx.SetStatusCode(fasthttp.StatusOK)
		ctx.SetBodyString(`{"status":"ok"}`)
		return
	}

	if path == "/__admin/reset" && method == "POST" {
		clearMappings(s)
		log.Println("All mappings reset")
		ctx.SetStatusCode(fasthttp.StatusOK)
		return
	}

	if path == "/__admin/settings" && method == "POST" {
		ctx.SetStatusCode(fasthttp.StatusOK)
		return
	}

	if path == "/__admin/scenarios/reset" && method == "POST" {
		ctx.SetStatusCode(fasthttp.StatusOK)
		ctx.SetBodyString(`{}`)
		return
	}

	if path == "/__admin/mappings" {
		handleMappings(s, ctx, method)
		return
	}

	if path == "/__admin/mappings/import" && method == "POST" {
		var wm WiremockMappings
		if err := json.Unmarshal(ctx.PostBody(), &wm); err != nil {
			ctx.SetStatusCode(fasthttp.StatusBadRequest)
			ctx.SetBodyString(err.Error())
			return
		}

		loadMappings(s, wm)
		log.Printf("Imported %d mappings", len(wm.Mappings))
		ctx.SetStatusCode(fasthttp.StatusOK)
		return
	}

	if path == "/__admin/mappings/reset" && method == "POST" {
		clearMappings(s)
		ctx.SetStatusCode(fasthttp.StatusOK)
		return
	}

	if path == "/__admin/requests" && method == "DELETE" {
		ctx.SetStatusCode(fasthttp.StatusOK)
		return
	}

	if path == "/__admin/recordings/snapshot" && method == "POST" {
		ctx.Response.Header.Set("Content-Type", "application/json")
		ctx.SetStatusCode(fasthttp.StatusOK)
		ctx.SetBodyString(`{"mappings":[]}`)
		return
	}

	log.Printf("Unknown admin endpoint: %s %s", method, path)
	ctx.SetStatusCode(fasthttp.StatusNotFound)
}

func handleMappings(s *Server, ctx *fasthttp.RequestCtx, method string) {
	switch method {
	case "POST":
		var m Mapping
		if err := json.Unmarshal(ctx.PostBody(), &m); err != nil {
			ctx.SetStatusCode(fasthttp.StatusBadRequest)
			ctx.SetBodyString(err.Error())
			return
		}

		addMapping(s, m)
		log.Printf("Added mapping: %s %s", m.Request.Method, getRequestPattern(&m))
		ctx.SetStatusCode(fasthttp.StatusCreated)

	case "DELETE":
		clearMappings(s)
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

// logVerboseRequest logs incoming request details when verbose mode is enabled.
func logVerboseRequest(ctx *fasthttp.RequestCtx, method, rawURI string) {
	log.Printf("[verbose] >> %s %s", method, rawURI)
	ctx.Request.Header.VisitAll(func(key, value []byte) {
		log.Printf("[verbose]    %s: %s", string(key), string(value))
	})
	if body := ctx.PostBody(); len(body) > 0 {
		bodyStr := string(body)
		if len(bodyStr) > 1000 {
			bodyStr = bodyStr[:1000] + fmt.Sprintf("... (%d bytes total)", len(body))
		}
		log.Printf("[verbose]    Body: %s", bodyStr)
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
