package record

import (
	"encoding/json"
	"fmt"
	"strings"
)

// JSONBlocks reads typed fenced payloads while leaving every unrelated byte
// opaque. Invalid known blocks are errors, never an empty record to overwrite.
func JSONBlocks(raw, kind string) ([]json.RawMessage, error) {
	lines := strings.Split(raw, "\n")
	out := []json.RawMessage{}
	for i := 0; i < len(lines); i++ {
		if strings.TrimSuffix(lines[i], "\r") != "```"+kind {
			continue
		}
		start := i + 1
		i++
		for i < len(lines) && strings.TrimSuffix(lines[i], "\r") != "```" {
			i++
		}
		if i == len(lines) {
			return nil, fmt.Errorf("unterminated %s block", kind)
		}
		b := json.RawMessage(strings.Join(lines[start:i], "\n"))
		if !json.Valid(b) {
			return nil, fmt.Errorf("invalid %s JSON", kind)
		}
		out = append(out, b)
	}
	return out, nil
}
func AppendJSONBlock(raw, kind string, value any) (string, error) {
	b, err := json.Marshal(value)
	if err != nil {
		return "", err
	}
	return raw + "\n\n```" + kind + "\n" + string(b) + "\n```\n", nil
}

// RewriteJSONBlocks preserves unknown block fields by letting the caller mutate
// a raw map, and preserves every byte outside the selected block payload.
func RewriteJSONBlocks(raw, kind string, change func(map[string]json.RawMessage) bool) (string, error) {
	blocks, err := JSONBlocks(raw, kind)
	if err != nil {
		return "", err
	}
	for _, b := range blocks {
		var fields map[string]json.RawMessage
		if err = json.Unmarshal(b, &fields); err != nil {
			return "", err
		}
		if change(fields) {
			next, err := json.Marshal(fields)
			if err != nil {
				return "", err
			}
			raw = strings.Replace(raw, string(b), string(next), 1)
		}
	}
	return raw, nil
}
