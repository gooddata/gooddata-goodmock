package jsonutil

import (
	"encoding/json"
	"testing"
)

func TestSortArrays(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{
			name:     "simple string array",
			input:    `["banana", "apple", "cherry"]`,
			expected: `["apple","banana","cherry"]`,
		},
		{
			name:     "number array",
			input:    `[3, 1, 2]`,
			expected: `[1,2,3]`,
		},
		{
			name:     "nested arrays sorted bottom-up",
			input:    `[["apple", "pear", "banana"], ["orange"], ["zzzzz", "aaaa"]]`,
			expected: `[["aaaa","zzzzz"],["apple","banana","pear"],["orange"]]`,
		},
		{
			name:     "array of objects sorted by JSON repr",
			input:    `[{"name": "bob"}, {"name": "alice"}]`,
			expected: `[{"name":"alice"},{"name":"bob"}]`,
		},
		{
			name:     "object with array values",
			input:    `{"items": ["c", "a", "b"], "nested": {"arr": [3, 1, 2]}}`,
			expected: `{"items":["a","b","c"],"nested":{"arr":[1,2,3]}}`,
		},
		{
			name:     "mixed types in array",
			input:    `[true, "abc", 123, null, false]`,
			expected: `["abc",123,false,null,true]`,
		},
		{
			name:     "empty array unchanged",
			input:    `[]`,
			expected: `[]`,
		},
		{
			name:     "no arrays — object unchanged",
			input:    `{"a": 1, "b": 2}`,
			expected: `{"a":1,"b":2}`,
		},
		{
			name:     "deeply nested bottom-up",
			input:    `[[["z", "a"], ["m", "b"]], [["x", "c"]]]`,
			expected: `[[["a","z"],["b","m"]],[["c","x"]]]`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var input any
			if err := json.Unmarshal([]byte(tt.input), &input); err != nil {
				t.Fatalf("failed to parse input: %v", err)
			}

			result := SortArrays(input)
			got, err := json.Marshal(result)
			if err != nil {
				t.Fatalf("failed to marshal result: %v", err)
			}

			if string(got) != tt.expected {
				t.Errorf("\n  got:  %s\n  want: %s", string(got), tt.expected)
			}
		})
	}
}
