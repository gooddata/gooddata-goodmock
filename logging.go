// (C) 2025 GoodData Corporation
package main

import (
	"fmt"
	"strings"
	"time"
)

const colWidth = 58

// logMismatch outputs a request mismatch in the same format as WireMock
func logMismatch(method, fullURL string, result MatchResult) {
	separator := strings.Repeat("-", 119)
	timestamp := time.Now().UTC().Format("2006-01-02 15:04:05.000")

	fmt.Printf("%s \n", timestamp)
	fmt.Println("                                               Request was not matched")
	fmt.Println("                                               =======================")
	fmt.Println()
	fmt.Println(separator)
	fmt.Printf("| %-*s | %-*s |\n", colWidth, "Closest stub", colWidth, "Request")
	fmt.Println(separator)

	if result.Mapping != nil {
		m := result.Mapping

		// Stub name
		fmt.Printf("%-*s |\n", colWidth+1, "")
		if m.Name != "" {
			fmt.Printf("%-*s |\n", colWidth+1, " "+m.Name)
		}
		fmt.Printf("%-*s |\n", colWidth+1, "")

		// Method
		fmt.Printf("%-*s | %s\n", colWidth, " "+m.Request.Method, method)

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
				colWidth-8, truncate(expectedPath, colWidth-8),
				colWidth, truncate(fullURL, colWidth))
		} else {
			// Split the actual URL across lines if needed
			actualTrunc := truncate(fullURL, colWidth-6)
			remainder := ""
			if len(fullURL) > colWidth-6 {
				remainder = fullURL[colWidth-6:]
			}
			fmt.Printf(" [path] %-*s | %s<<<<< URL does not match\n",
				colWidth-8, truncate(expectedPath, colWidth-8),
				actualTrunc)
			if remainder != "" {
				fmt.Printf(" %-*s | %s\n", colWidth-1, "", remainder)
			}
		}
		fmt.Printf("%-*s |\n", colWidth+1, "")

		// Query parameter diffs
		for _, diff := range result.QueryDiffs {
			parts := strings.SplitN(diff, "|", 4)
			diffType := parts[0]
			paramName := parts[1]
			expectedVals := parts[2]

			stubCol := fmt.Sprintf(" Query: %s exactly %s", paramName, expectedVals)
			if diffType == "not_present" {
				fmt.Printf("%-*s | %s<<<<< Query is not present\n",
					colWidth, truncate(stubCol, colWidth),
					strings.Repeat(" ", colWidth-5-len("<<<<< Query is not present")+6))
			} else {
				actualVals := parts[3]
				actualCol := fmt.Sprintf("%s: %s", paramName, actualVals)
				fmt.Printf("%-*s | %-*s<<<<< Query does not match\n",
					colWidth, truncate(stubCol, colWidth),
					colWidth-26, truncate(actualCol, colWidth-26))
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
					colWidth, truncate(stubCol, colWidth),
					strings.Repeat(" ", colWidth-5-len("<<<<< Header is not present")+6))
			} else {
				actualVal := parts[3]
				actualCol := fmt.Sprintf("%s: %s", headerName, actualVal)
				fmt.Printf("%-*s | %-*s<<<<< Header does not match\n",
					colWidth, truncate(stubCol, colWidth),
					colWidth-27, truncate(actualCol, colWidth-27))
			}
		}

		// Body diff
		if result.BodyDiff != "" {
			fmt.Printf(" %-*s | <<<<< %s\n", colWidth-1, "Body [equalToJson]", result.BodyDiff)
		}
	} else {
		fmt.Printf(" No stub found for: %s %s\n", method, fullURL)
	}

	fmt.Printf("%-*s |\n", colWidth+1, "")
	fmt.Printf("%-*s |\n", colWidth+1, "")
	fmt.Println(separator)
	fmt.Println()
}

func truncate(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen-3] + "..."
}
