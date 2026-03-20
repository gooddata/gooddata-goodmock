// (C) 2025 GoodData Corporation
package server

import (
	"fmt"
	"goodmock/internal/types"
	"log"

	"github.com/valyala/fasthttp"
)

// NewServer creates a new mock server
func NewServer(proxyHost, refererPath string, verbose bool, binaryContentTypes []string) *types.Server {
	return &types.Server{
		Mappings:           make([]types.Mapping, 0),
		ProxyHost:          proxyHost,
		RefererPath:        refererPath,
		Verbose:            verbose,
		BinaryContentTypes: binaryContentTypes,
	}
}

func LoadMappings(s *types.Server, wm types.WiremockMappings) {
	s.Mu.Lock()
	s.Mappings = append(s.Mappings, wm.Mappings...)
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

func HandleAdmin(ctx *fasthttp.RequestCtx, path, method string) {
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

	if path == "/__admin/settings" && method == "POST" {
		ctx.SetStatusCode(fasthttp.StatusNotImplemented)
		return
	}

	if path == "/__admin/requests" && method == "DELETE" {
		ctx.SetStatusCode(fasthttp.StatusNotImplemented)
		return
	}

	log.Printf("Unknown admin endpoint: %s %s", method, path)
	ctx.SetStatusCode(fasthttp.StatusNotFound)
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
