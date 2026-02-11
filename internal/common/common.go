package common

import (
	"log"
	"os"
	"strconv"
	"strings"
)

func GetPort() int {
	if p := os.Getenv("PORT"); p != "" {
		if port, err := strconv.Atoi(p); err == nil {
			return port
		}
		log.Fatalf("Invalid PORT value: %s", p)
	}
	return 8080
}

func IsVerbose() bool {
	return os.Getenv("VERBOSE") != ""
}

// PreserveJSONKeyOrder returns true if JSON response body key order should be
// preserved from the upstream server. When false (default), keys are sorted
// alphabetically for deterministic diffs. Record mode only.
func PreserveJSONKeyOrder() bool {
	return os.Getenv("PRESERVE_JSON_KEY_ORDER") != ""
}

func SortArrayMembers() bool {
	return os.Getenv("SORT_ARRAY_MEMBERS") != ""
}

// ParseJSONContentTypes returns the list of Content-Types whose response bodies
// should be stored as structured JSON (jsonBody) instead of escaped strings.
// application/json is always included.
func ParseJSONContentTypes() []string {
	types := []string{"application/json"}
	if env := os.Getenv("JSON_CONTENT_TYPES"); env != "" {
		for _, t := range strings.Split(env, ",") {
			t = strings.TrimSpace(t)
			if t != "" && t != "application/json" {
				types = append(types, t)
			}
		}
	}
	return types
}
