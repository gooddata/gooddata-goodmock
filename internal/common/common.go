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
	v := strings.ToLower(os.Getenv("VERBOSE"))
	return v == "true" || v == "1" || v == "yes"
}
