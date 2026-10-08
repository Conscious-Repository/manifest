package construction

import (
	"bytes"
	"errors"
	"strings"
	"testing"
)

// A request body is exactly one JSON value: a second complete document, or
// any trailing bytes after the first (including a stray ']' or '}', which a
// Decoder.More check does not see at top level), make it invalid.
func TestDecodeRequestExactlyOneValue(t *testing.T) {
	type body struct {
		A int `json:"a"`
	}
	for _, ok := range []string{`{"a":1}`, "{\"a\":1}\n", " \t{\"a\":1} \r\n\t "} {
		var v body
		if err := decodeRequest([]byte(ok), &v); err != nil || v.A != 1 {
			t.Fatalf("%q: %v", ok, err)
		}
	}
	for _, bad := range []string{
		`{"a":1}{"a":2}`, `{"a":1} {"a":2}`, "{\"a\":1}\n{\"a\":2}",
		`{"a":1}]`, `{"a":1}}`, `{"a":1}]{"a":2}`, `{"a":1}}{"a":2}`, `{"a":1},{"a":2}`,
		`{"a":1}x`, `{"a":1}null`, `{"a":1}"x"`, `{"a":1}[]`, `{"a":1}0`, `{"a":1}{`, "{\"a\":1}\x00",
		``, `   `, `{"a":1,"b":2}`,
	} {
		var v body
		err := decodeRequest([]byte(bad), &v)
		var e *Error
		if !errors.As(err, &e) || e.Status != 422 || e.Kind != "invalid" {
			t.Fatalf("%q must be invalid, got %v", bad, err)
		}
	}
	if err := decodeRequest(bytes.Repeat([]byte(" "), MaxCommandBytes+1), &body{}); !errors.Is(err, ErrTooLarge) {
		t.Fatalf("oversized body: %v", err)
	}
	// the exported form is the same check
	if err := DecodeRequest([]byte(`{"a":1}]`), &body{}); StatusOf(err) != 422 {
		t.Fatalf("DecodeRequest: %v", err)
	}
}

// Every typed request parser inherits the rule, whatever it does afterwards.
func TestParsersRefuseTrailingValues(t *testing.T) {
	create := `{"schemaVersion":1,"requestId":"create-trail-01","title":"Trailing"}`
	command := `{"schemaVersion":1,"requestId":"cmd-trail-0001","problemId":"cp-00000000000000000000000000000000","expectedProblemRevision":"` +
		strings.Repeat("a", 64) + `","operations":[{"op":"SetProblemText","title":"x"}]}`
	run := `{"schemaVersion":1,"requestId":"run-trail-0001","expectedProblemRevision":"` + strings.Repeat("a", 64) + `","agent":{"mode":"local-only"}}`
	parse := map[string]func([]byte) error{
		"create":  func(b []byte) error { _, _, err := ParseCreate(b); return err },
		"command": func(b []byte) error { _, err := ParseCommand(b); return err },
		"run":     func(b []byte) error { _, _, err := ParseRunRequest(b); return err },
	}
	valid := map[string]string{"create": create, "command": command, "run": run}
	for name, p := range parse {
		if err := p([]byte(valid[name])); err != nil {
			t.Fatalf("%s: the valid body must parse: %v", name, err)
		}
		for _, tail := range []string{"]", "}", valid[name], "]" + valid[name]} {
			if err := p([]byte(valid[name] + tail)); StatusOf(err) != 422 {
				t.Fatalf("%s with trailing %.12q…: %v", name, tail, err)
			}
		}
	}
}
