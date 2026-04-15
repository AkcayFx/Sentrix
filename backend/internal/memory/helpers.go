package memory

import (
	"encoding/json"
	"fmt"
	"strings"
)

// vectorLiteral converts a float32 slice to a pgvector-compatible string literal.
// Example: [0.1, 0.2, 0.3] → "[0.1,0.2,0.3]"
func vectorLiteral(vec []float32) string {
	if len(vec) == 0 {
		return "[]"
	}

	var b strings.Builder
	b.WriteByte('[')
	for i, v := range vec {
		if i > 0 {
			b.WriteByte(',')
		}
		fmt.Fprintf(&b, "%g", v)
	}
	b.WriteByte(']')
	return b.String()
}

// marshalJSON converts a map to a JSON byte slice.
func marshalJSON(v interface{}) ([]byte, error) {
	return json.Marshal(v)
}

// parseMetadata safely parses a JSON string into a map.
func parseMetadata(raw string) map[string]interface{} {
	if raw == "" || raw == "{}" {
		return make(map[string]interface{})
	}
	var m map[string]interface{}
	if err := json.Unmarshal([]byte(raw), &m); err != nil {
		return make(map[string]interface{})
	}
	return m
}

// truncate shortens a string for logging purposes.
func truncate(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen] + "..."
}
