// (C) 2025 GoodData Corporation
package server

import (
	"encoding/json"
	"fmt"
	"goodmock/internal/logging"
	"goodmock/internal/matching"
	"goodmock/internal/types"
	"log"
	"strings"

	"github.com/valyala/fasthttp"
)

// NewServer creates a new mock server
func NewServer(proxyHost, refererPath string, verbose bool) *types.Server {
	return &types.Server{
		Mappings:    make([]types.Mapping, 0),
		ProxyHost:   proxyHost,
		RefererPath: refererPath,
		Verbose:     verbose,
	}
}

func LoadMappings(s *types.Server, wm types.WiremockMappings) {
	s.Mu.Lock()
	s.Mappings = append(s.Mappings, wm.Mappings...)
	s.Mu.Unlock()
}

func addMapping(s *types.Server, m types.Mapping) {
	s.Mu.Lock()
	s.Mappings = append(s.Mappings, m)
	s.Mu.Unlock()
}

func ClearMappings(s *types.Server) {
	s.Mu.Lock()
	s.Mappings = make([]types.Mapping, 0)
	s.Mu.Unlock()
}

// TransformRequestHeaders rewrites incoming request headers to match recorded stubs.
func TransformRequestHeaders(h *fasthttp.RequestHeader, proxyHost, refererPath string) {
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

// HandleRequest handles incoming HTTP requests
func HandleRequest(s *types.Server, ctx *fasthttp.RequestCtx) {
	rawURI := string(ctx.RequestURI())
	path := rawURI
	if idx := strings.IndexByte(rawURI, '?'); idx != -1 {
		path = rawURI[:idx]
	}
	method := string(ctx.Method())

	if strings.HasPrefix(path, "/__admin") {
		HandleAdmin(s, ctx, path, method)
		return
	}

	if s.Verbose {
		LogVerboseRequest(ctx, method, rawURI)
	}

	TransformRequestHeaders(&ctx.Request.Header, s.ProxyHost, s.RefererPath)

	body := ctx.PostBody()
	fullURI := rawURI

	result := matching.MatchRequest(s, method, path, fullURI, ctx.QueryArgs(), body, &ctx.Request.Header)

	if !result.Matched {
		logging.LogMismatch(method, fullURI, result)
		ctx.SetStatusCode(fasthttp.StatusNotFound)
		ctx.SetBodyString(`{"error": "No matching stub found"}`)
		return
	}

	m := result.Mapping
	applyResponseHeaders(ctx, m.Response.Headers)

	ctx.SetStatusCode(m.Response.Status)
	if m.Response.JsonBody != nil {
		data, err := json.Marshal(m.Response.JsonBody)
		if err == nil {
			ctx.SetBody(data)
		}
	} else if m.Response.Body != "" {
		ctx.SetBodyString(m.Response.Body)
	}

	if s.Verbose {
		log.Printf("[verbose] << %d %s", m.Response.Status, method+" "+rawURI)
	}
}

func HandleAdmin(s *types.Server, ctx *fasthttp.RequestCtx, path, method string) {
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
		ClearMappings(s)
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
		var wm types.WiremockMappings
		if err := json.Unmarshal(ctx.PostBody(), &wm); err != nil {
			ctx.SetStatusCode(fasthttp.StatusBadRequest)
			ctx.SetBodyString(err.Error())
			return
		}

		LoadMappings(s, wm)
		log.Printf("Imported %d mappings", len(wm.Mappings))
		ctx.SetStatusCode(fasthttp.StatusOK)
		return
	}

	if path == "/__admin/mappings/reset" && method == "POST" {
		ClearMappings(s)
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

func handleMappings(s *types.Server, ctx *fasthttp.RequestCtx, method string) {
	switch method {
	case "POST":
		var m types.Mapping
		if err := json.Unmarshal(ctx.PostBody(), &m); err != nil {
			ctx.SetStatusCode(fasthttp.StatusBadRequest)
			ctx.SetBodyString(err.Error())
			return
		}

		addMapping(s, m)
		log.Printf("Added mapping: %s %s", m.Request.Method, getRequestPattern(&m))
		ctx.SetStatusCode(fasthttp.StatusCreated)

	case "DELETE":
		ClearMappings(s)
		ctx.SetStatusCode(fasthttp.StatusOK)

	case "GET":
		s.Mu.RLock()
		wm := types.WiremockMappings{Mappings: s.Mappings}
		s.Mu.RUnlock()

		ctx.Response.Header.Set("Content-Type", "application/json")
		data, _ := json.Marshal(wm)
		ctx.SetBody(data)

	default:
		ctx.SetStatusCode(fasthttp.StatusMethodNotAllowed)
	}
}

// LogVerboseRequest logs incoming request details when verbose mode is enabled.
func LogVerboseRequest(ctx *fasthttp.RequestCtx, method, rawURI string) {
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

func getRequestPattern(m *types.Mapping) string {
	if m.Request.URL != "" {
		return m.Request.URL
	}
	if m.Request.URLPath != "" {
		return m.Request.URLPath
	}
	return m.Request.URLPattern
}
