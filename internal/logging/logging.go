// (C) 2025 GoodData Corporation
package logging

import (
	"encoding/json"
	"fmt"
	"goodmock/internal/types"
	"sort"
	"strings"
	"time"
)

const fullWidth = 119
const innerWidth = fullWidth - 4
const colWidth2 = 58

// LogMismatch outputs a request mismatch in the same format as WireMock
func LogMismatch(method, fullURL string, result types.MatchResult) {
	separator := strings.Repeat("-", fullWidth)
	timestamp := time.Now().UTC().Format("2006-01-02 15:04:05.000")

	fmt.Printf("%s \n", timestamp)
	fmt.Println("                                               Request was not matched")
	fmt.Println("                                               =======================")
	fmt.Println()
	fmt.Println(separator)
	fmt.Printf("| %-*s | %-*s |\n", colWidth2, "Closest stub", colWidth2, "Request")
	fmt.Println(separator)

	if result.Mapping != nil {
		m := result.Mapping

		// Stub name
		fmt.Printf("%-*s |\n", colWidth2+1, "")
		if m.Name != "" {
			fmt.Printf("%-*s |\n", colWidth2+1, " "+m.Name)
		}
		fmt.Printf("%-*s |\n", colWidth2+1, "")

		// Method
		fmt.Printf("%-*s | %s\n", colWidth2, " "+m.Request.Method, method)

		// Path comparison
		expectedPath := m.Request.URL
		if expectedPath == "" {
			expectedPath = m.Request.URLPath
		}
		if expectedPath == "" {
			expectedPath = m.Request.URLPattern
		}

		if result.URLMatch {
			fmt.Printf(" [path] %-*s | %-*s\n",
				colWidth2-8, truncate(expectedPath, colWidth2-8),
				colWidth2, truncate(fullURL, colWidth2))
		} else {
			// Split the actual URL across lines if needed
			actualTrunc := truncate(fullURL, colWidth2-6)
			remainder := ""
			if len(fullURL) > colWidth2-6 {
				remainder = fullURL[colWidth2-6:]
			}
			fmt.Printf(" [path] %-*s | %s<<<<< URL does not match\n",
				colWidth2-8, truncate(expectedPath, colWidth2-8),
				actualTrunc)
			if remainder != "" {
				fmt.Printf(" %-*s | %s\n", colWidth2-1, "", remainder)
			}
		}
		fmt.Printf("%-*s |\n", colWidth2+1, "")

		// Query parameter diffs
		for _, diff := range result.QueryDiffs {
			parts := strings.SplitN(diff, "|", 4)
			diffType := parts[0]
			paramName := parts[1]
			expectedVals := parts[2]

			stubCol := fmt.Sprintf(" Query: %s exactly %s", paramName, expectedVals)
			if diffType == "not_present" {
				fmt.Printf("%-*s | %s<<<<< Query is not present\n",
					colWidth2, truncate(stubCol, colWidth2),
					strings.Repeat(" ", colWidth2-5-len("<<<<< Query is not present")+6))
			} else {
				actualVals := parts[3]
				actualCol := fmt.Sprintf("%s: %s", paramName, actualVals)
				fmt.Printf("%-*s | %-*s<<<<< Query does not match\n",
					colWidth2, truncate(stubCol, colWidth2),
					colWidth2-26, truncate(actualCol, colWidth2-26))
			}
		}

		// Header diffs
		for _, diff := range result.HeaderDiffs {
			parts := strings.SplitN(diff, "|", 4)
			diffType := parts[0]
			headerName := parts[1]
			expectedVal := parts[2]

			stubCol := fmt.Sprintf(" Header: %s [equalTo %s]", headerName, expectedVal)
			if diffType == "not_present" {
				fmt.Printf("%-*s | %s<<<<< Header is not present\n",
					colWidth2, truncate(stubCol, colWidth2),
					strings.Repeat(" ", colWidth2-5-len("<<<<< Header is not present")+6))
			} else {
				actualVal := parts[3]
				actualCol := fmt.Sprintf("%s: %s", headerName, actualVal)
				fmt.Printf("%-*s | %-*s<<<<< Header does not match\n",
					colWidth2, truncate(stubCol, colWidth2),
					colWidth2-27, truncate(actualCol, colWidth2-27))
			}
		}

		// Body diff
		if result.BodyDiff != "" {
			fmt.Printf(" %-*s | <<<<< %s\n", colWidth2-1, "Body [equalToJson]", result.BodyDiff)
		}
	} else {
		fmt.Printf(" No stub found for: %s %s\n", method, fullURL)
	}

	fmt.Printf("%-*s |\n", colWidth2+1, "")
	fmt.Printf("%-*s |\n", colWidth2+1, "")
	fmt.Println(separator)
	fmt.Println()
}

// LogUnused outputs an unused stub in the same format as WireMock
func LogUnused(mapping types.Mapping) {
	separator := strings.Repeat("-", fullWidth)
	timestamp := time.Now().UTC().Format("2006-01-02 15:04:05.000")

	fmt.Printf("%s \n", timestamp)
	fmt.Println("                                               Stub was unused")
	fmt.Println("                                               ===============")
	fmt.Println()
	fmt.Println(separator)
	fmt.Printf("| %-*s | %-*s |\n", colWidth2, "Stub request", colWidth2, "Stub response")
	fmt.Println(separator)

	left := stubRequestLines(mapping)
	right := stubResponseLines(mapping)
	rows := max(len(left), len(right))

	fmt.Printf("%-*s |\n", colWidth2+1, "")
	for i := 0; i < rows; i++ {
		l, r := "", ""
		if i < len(left) {
			l = left[i]
		}
		if i < len(right) {
			r = right[i]
		}
		fmt.Printf("%-*s | %s\n", colWidth2+1, truncate(l, colWidth2+1), truncate(r, colWidth2))
	}
	fmt.Printf("%-*s |\n", colWidth2+1, "")
	fmt.Println(separator)
	fmt.Println()
}

// stubRequestLines renders the request side of a stub, one line per criterion.
func stubRequestLines(m types.Mapping) []string {
	lines := []string{}
	if m.Name != "" {
		lines = append(lines, " "+m.Name, "")
	}
	lines = append(lines, " "+m.Request.Method)

	// url matches the full URI, urlPath only the path, urlPattern is a regex on the full URI
	switch {
	case m.Request.URL != "":
		lines = append(lines, " [url] "+m.Request.URL)
	case m.Request.URLPath != "":
		lines = append(lines, " [urlPath] "+m.Request.URLPath)
	case m.Request.URLPattern != "":
		lines = append(lines, " [urlPattern] "+m.Request.URLPattern)
	default:
		lines = append(lines, " [url] <none>")
	}

	for _, name := range sortedKeys(m.Request.QueryParameters) {
		matcher := m.Request.QueryParameters[name]
		if matcher.EqualTo != "" {
			lines = append(lines, fmt.Sprintf(" Query: %s [equalTo %s]", name, matcher.EqualTo))
			continue
		}
		vals := make([]string, 0, len(matcher.HasExactly))
		for _, v := range matcher.HasExactly {
			vals = append(vals, v.EqualTo)
		}
		lines = append(lines, fmt.Sprintf(" Query: %s exactly %v", name, vals))
	}

	for _, name := range sortedKeys(m.Request.Headers) {
		matcher := m.Request.Headers[name]
		if matcher.EqualTo != "" {
			lines = append(lines, fmt.Sprintf(" Header: %s [equalTo %s]", name, matcher.EqualTo))
		} else {
			lines = append(lines, fmt.Sprintf(" Header: %s [contains %s]", name, matcher.Contains))
		}
	}

	for _, p := range m.Request.BodyPatterns {
		if p.EqualToJSON != nil {
			lines = append(lines, " Body [equalToJson] "+string(p.EqualToJSON))
		}
	}
	return lines
}

// stubResponseLines renders the response side of a stub.
func stubResponseLines(m types.Mapping) []string {
	lines := []string{fmt.Sprintf(" Status: %d", m.Response.Status)}

	for _, name := range sortedKeys(m.Response.Headers) {
		lines = append(lines, fmt.Sprintf(" Header: %s: %v", name, m.Response.Headers[name]))
	}

	if m.Response.ProxyBaseUrl != "" {
		lines = append(lines, " ProxyBaseUrl: "+m.Response.ProxyBaseUrl)
	}

	if m.Response.JsonBody != nil {
		if data, err := json.Marshal(m.Response.JsonBody); err == nil {
			lines = append(lines, " Body [json] "+string(data))
		}
	} else if m.Response.Body != "" {
		lines = append(lines, " Body "+m.Response.Body)
	}
	return lines
}

// sortedKeys returns the keys of a map in sorted order for stable output.
func sortedKeys[V any](m map[string]V) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

func truncate(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen-3] + "..."
}
