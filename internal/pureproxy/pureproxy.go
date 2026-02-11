// (C) 2025 GoodData Corporation
package pureproxy

import (
	"fmt"
	"goodmock/internal/common"
	"goodmock/internal/proxy"
	"goodmock/internal/server"
	"goodmock/internal/types"
	"log"
	"os"
	"strings"

	"github.com/valyala/fasthttp"
)

// ProxyServer forwards requests to an upstream backend without recording.
type ProxyServer struct {
	server   *types.Server
	upstream string
	client   *fasthttp.Client
}

func NewProxyServer(upstream, proxyHost, refererPath string, verbose bool) *ProxyServer {
	return &ProxyServer{
		server:   server.NewServer(proxyHost, refererPath, verbose),
		upstream: upstream,
		client:   &fasthttp.Client{},
	}
}

func handleProxyRequest(ps *ProxyServer, ctx *fasthttp.RequestCtx) {
	rawURI := string(ctx.RequestURI())
	path := rawURI
	if idx := strings.IndexByte(rawURI, '?'); idx != -1 {
		path = rawURI[:idx]
	}
	method := string(ctx.Method())

	// Admin endpoints handled locally
	if strings.HasPrefix(path, "/__admin") {
		server.HandleAdmin(ps.server, ctx, path, method)
		return
	}

	if ps.server.Verbose {
		server.LogVerboseRequest(ctx, method, rawURI)
	}

	// Transform request headers before proxying
	server.TransformRequestHeaders(&ctx.Request.Header, ps.server.ProxyHost, ps.server.RefererPath)

	// Proxy to upstream
	forwardAndRespond(ps, ctx)
}

func forwardAndRespond(ps *ProxyServer, ctx *fasthttp.RequestCtx) {
	status, respHeaders, body, err := proxy.ProxyRequest(ps.client, ps.upstream, ctx)
	if err != nil {
		log.Printf("Proxy error: %v", err)
		ctx.SetStatusCode(502)
		ctx.SetBodyString(fmt.Sprintf(`{"error": "proxy error: %s"}`, err.Error()))
		return
	}

	// Send response back to client, filtering headers
	for key, values := range respHeaders {
		upperKey := strings.ToUpper(key)
		if strings.HasPrefix(upperKey, "X-GDC") || upperKey == "DATE" {
			continue
		}
		if upperKey == "CONTENT-ENCODING" {
			continue
		}
		if upperKey == "CONTENT-LENGTH" {
			continue
		}
		for _, v := range values {
			ctx.Response.Header.Add(key, v)
		}
	}
	ctx.SetStatusCode(status)
	ctx.SetBody(body)

	if ps.server.Verbose {
		log.Printf("[verbose] << %d %s %s (%d bytes)", status, string(ctx.Method()), string(ctx.RequestURI()), len(body))
	}
}

func RunProxy() {
	port := common.GetPort()

	upstream := os.Getenv("PROXY_HOST")
	if upstream == "" {
		fmt.Fprintf(os.Stderr, "PROXY_HOST environment variable is required in proxy mode\n")
		os.Exit(1)
	}

	refererPath := os.Getenv("REFERER_PATH")
	if refererPath == "" {
		refererPath = "/"
	}

	verbose := common.IsVerbose()
	ps := NewProxyServer(upstream, upstream, refererPath, verbose)

	addr := fmt.Sprintf(":%d", port)

	fmt.Println("┌──────────────────────────────────────────────────────────────────────────────┐")
	fmt.Println("|                                                                              |")
	fmt.Printf("|   GoodMock - Wiremock-compatible mock server (fasthttp)                      |\n")
	fmt.Printf("|   Mode: %-69s|\n", "proxy")
	fmt.Printf("|   Port: %-69d|\n", port)
	fmt.Printf("|   Upstream: %-66s|\n", upstream)
	fmt.Printf("|   Verbose: %-66v|\n", verbose)
	fmt.Println("|                                                                              |")
	fmt.Println("└──────────────────────────────────────────────────────────────────────────────┘")

	log.Fatal(fasthttp.ListenAndServe(addr, func(ctx *fasthttp.RequestCtx) {
		handleProxyRequest(ps, ctx)
	}))
}
