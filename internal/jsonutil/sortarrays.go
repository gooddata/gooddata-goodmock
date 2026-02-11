// (C) 2025 GoodData Corporation
package jsonutil

import (
	"encoding/json"
	"sort"
)

// SortArrays recursively walks a JSON value (bottom-up) and sorts all arrays
// by the JSON-stringified representation of each element. Inner arrays are
// sorted before outer ones so that the sort key is stable.
func SortArrays(v any) any {
	switch val := v.(type) {
	case map[string]any:
		for k, child := range val {
			val[k] = SortArrays(child)
		}
		return val
	case []any:
		// Recurse into each element first (bottom-up)
		for i, child := range val {
			val[i] = SortArrays(child)
		}
		// Sort elements by their JSON representation
		sort.SliceStable(val, func(i, j int) bool {
			a, _ := json.Marshal(val[i])
			b, _ := json.Marshal(val[j])
			return string(a) < string(b)
		})
		return val
	default:
		return v
	}
}
