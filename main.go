// (C) 2025 GoodData Corporation
package main

import (
	"encoding/json"
	"fmt"
	"goodmock/internal/common"
	"goodmock/internal/pureproxy"
	"goodmock/internal/record"
	"goodmock/internal/server"
	"goodmock/internal/types"
	"log"
	"os"
	"strings"

	"github.com/valyala/fasthttp"
)

func main() {
	// First arg is the mode (default: replay)
	mode := "replay"
	if len(os.Args) > 1 {
		mode = os.Args[1]
	}

	switch mode {
	case "replay":
		runReplay()
	case "record":
		record.RunRecord()
	case "proxy":
		pureproxy.RunProxy()
	default:
		fmt.Fprintf(os.Stderr, "Unknown mode: %s\nUsage: goodmock <mode>\nModes: replay, record, proxy\n", mode)
		os.Exit(1)
	}
}

func runReplay() {
	port := common.GetPort()

	proxyHost := os.Getenv("PROXY_HOST")
	if proxyHost == "" {
		proxyHost = "http://localhost"
	}

	refererPath := os.Getenv("REFERER_PATH")
	if refererPath == "" {
		refererPath = "/"
	}

	verbose := common.IsVerbose()
	s := server.NewServer(proxyHost, refererPath, verbose)

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
	fmt.Println("|                                                                              |")
	fmt.Println("└──────────────────────────────────────────────────────────────────────────────┘")

	log.Fatal(fasthttp.ListenAndServe(addr, func(ctx *fasthttp.RequestCtx) {
		server.HandleRequest(s, ctx)
	}))
}
