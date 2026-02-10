// (C) 2025 GoodData Corporation
package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"os"
	"strings"

	"github.com/valyala/fasthttp"
)

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
