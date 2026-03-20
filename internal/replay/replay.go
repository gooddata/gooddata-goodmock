// (C) 2025 GoodData Corporation
package replay

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"goodmock/internal/common"
	"goodmock/internal/logging"
	"goodmock/internal/matching"
	"goodmock/internal/server"
	"goodmock/internal/types"
	"log"
	"os"
	"strings"

	"github.com/valyala/fasthttp"
)

func RunReplay() {
	port := common.GetPort()
	const maxRequestBodySize = 16 * 1024 * 1024

	proxyHost := os.Getenv("PROXY_HOST")
	if proxyHost == "" {
		proxyHost = "http://localhost"
	}

	refererPath := os.Getenv("REFERER_PATH")
	if refererPath == "" {
		refererPath = "/"
	}

	verbose := common.IsVerbose()
	binaryContentTypes := common.ParseBinaryContentTypes()
	s := server.NewServer(proxyHost, refererPath, verbose, binaryContentTypes)

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
					var wm types.WiremockMappings
					if err := json.Unmarshal(data, &wm); err != nil {
						log.Printf("Warning: Could not parse mapping file %s: %v", filePath, err)
					} else {
						server.LoadMappings(s, wm)
						log.Printf("Loaded %d mappings from %s", len(wm.Mappings), filePath)
					}
				}
			}
		}
	}

	addr := fmt.Sprintf(":%d", port)

	fmt.Println("┌──────────────────────────────────────────────────────────────────────────────┐")
	fmt.Println("|                                                                              |")
	fmt.Printf("|   GoodMock - Wiremock-compatible mock server (fasthttp)                      |\n")
	fmt.Printf("|   Mode: %-69s|\n", "replay")
	fmt.Printf("|   Port: %-69d|\n", port)
	fmt.Printf("|   Verbose: %-66v|\n", verbose)
	fmt.Printf("|   Max Request Body: %-57s|\n", fmt.Sprintf("%d bytes", maxRequestBodySize))
	fmt.Println("|                                                                              |")
	fmt.Println("└──────────────────────────────────────────────────────────────────────────────┘")

	httpServer := &fasthttp.Server{
		Handler:            func(ctx *fasthttp.RequestCtx) { handleReplayRequest(s, ctx) },
		MaxRequestBodySize: maxRequestBodySize,
		ErrorHandler: func(ctx *fasthttp.RequestCtx, err error) {
			ctx.SetStatusCode(fasthttp.StatusBadRequest)
			ctx.SetBodyString(err.Error())
		},
	}

	log.Fatal(httpServer.ListenAndServe(addr))
}

func handleReplayRequest(s *types.Server, ctx *fasthttp.RequestCtx) {
	rawURI := string(ctx.RequestURI())
	path := rawURI
	if idx := strings.IndexByte(rawURI, '?'); idx != -1 {
		path = rawURI[:idx]
	}
	method := string(ctx.Method())

	if strings.HasPrefix(path, "/__admin") {
		handleReplayAdmin(s, ctx, path, method)
		return
	}

	if s.Verbose {
		server.LogVerboseRequest(ctx, method, rawURI)
	}

	server.TransformRequestHeaders(&ctx.Request.Header, s.ProxyHost, s.RefererPath)

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
		if isBinaryResponse(m.Response.Headers, s.BinaryContentTypes) {
			decoded, err := base64.StdEncoding.DecodeString(m.Response.Body)
			if err == nil {
				ctx.SetBody(decoded)
			} else {
				ctx.SetBodyString(m.Response.Body)
			}
		} else {
			ctx.SetBodyString(m.Response.Body)
		}
	}

	if s.Verbose {
		log.Printf("[verbose] << %d %s", m.Response.Status, method+" "+rawURI)
	}
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

func handleReplayAdmin(s *types.Server, ctx *fasthttp.RequestCtx, path, method string) {
	if path == "/__admin/reset" && method == "POST" {
		server.ClearMappings(s)
		log.Println("All mappings reset")
		ctx.SetStatusCode(fasthttp.StatusOK)
		return
	}

	// TODO: Implementing this may allow us to turn more integrated tests into isolated tests
	if path == "/__admin/scenarios/reset" && method == "POST" {
		ctx.SetStatusCode(fasthttp.StatusNotImplemented)
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

		server.LoadMappings(s, wm)
		log.Printf("Imported %d mappings", len(wm.Mappings))
		ctx.SetStatusCode(fasthttp.StatusOK)
		return
	}

	if path == "/__admin/mappings/reset" && method == "POST" {
		server.ClearMappings(s)
		ctx.SetStatusCode(fasthttp.StatusOK)
		return
	}

	// Delegate everything else to the server's common admin handler
	server.HandleAdmin(ctx, path, method)
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
		server.ClearMappings(s)
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

func addMapping(s *types.Server, m types.Mapping) {
	s.Mu.Lock()
	s.Mappings = append(s.Mappings, m)
	s.Mu.Unlock()
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

// isBinaryResponse checks if the response Content-Type matches any of the given binary types.
func isBinaryResponse(headers map[string]any, binaryTypes []string) bool {
	if len(binaryTypes) == 0 {
		return false
	}
	for key, value := range headers {
		if !strings.EqualFold(key, "Content-Type") {
			continue
		}
		var ct string
		switch v := value.(type) {
		case string:
			ct = v
		case []interface{}:
			if len(v) > 0 {
				if s, ok := v[0].(string); ok {
					ct = s
				}
			}
		}
		if ct != "" {
			mediaType := strings.TrimSpace(strings.SplitN(ct, ";", 2)[0])
			for _, bt := range binaryTypes {
				if strings.EqualFold(mediaType, bt) {
					return true
				}
			}
		}
	}
	return false
}
