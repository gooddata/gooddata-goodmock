// (C) 2025 GoodData Corporation
package main

import (
	_ "embed"
	"fmt"
	"goodmock/internal/pureproxy"
	"goodmock/internal/record"
	"goodmock/internal/replay"
	"os"
	"strings"
)

//go:embed VERSION
var version string

func main() {
	for _, arg := range os.Args[1:] {
		if arg == "-v" || arg == "--version" {
			fmt.Print(strings.TrimSpace(version))
			fmt.Println()
			os.Exit(0)
		}
	}

	// First arg is the mode (default: replay)
	mode := "replay"
	if len(os.Args) > 1 {
		mode = os.Args[1]
	}

	switch mode {
	case "replay":
		replay.RunReplay()
	case "record":
		record.RunRecord()
	case "proxy":
		pureproxy.RunProxy()
	default:
		fmt.Fprintf(os.Stderr, "Unknown mode: %s\nUsage: goodmock <mode>\nModes: replay, record, proxy\n", mode)
		os.Exit(1)
	}
}
