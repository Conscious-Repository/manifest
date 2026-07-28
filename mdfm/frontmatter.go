// Package mdfm is the markdown-frontmatter toolkit shared by the agent stores
// (profiles, feed, approvals). The fence grammar and list form come from the
// kernel (record); what stays HERE is the agent-store read policy — a flat
// scalar map (`key: value`, values RAW so quotes round-trip through the
// Writer) — plus the Writer and the fenced-block extractors.
package mdfm

import (
	"strings"

	"manifest/record"
)

// Split parses a leading `---` frontmatter block into key→value pairs and returns
// the remaining body. Content without a leading `---` yields an empty map + the
// whole string as body. Values stay raw (not unquoted) so a Load→Save cycle
// through the Writer preserves them byte-for-byte.
func Split(content string) (map[string]string, string) {
	fm := map[string]string{}
	block, body, ok := record.SplitFrontmatter(content)
	if !ok {
		return fm, body
	}
	for _, line := range block {
		if k, v, ok := strings.Cut(line, ":"); ok {
			fm[strings.TrimSpace(k)] = strings.TrimSpace(v)
		}
	}
	return fm, body
}

// List parses a frontmatter list value: "[a, b]" or "a, b" → ["a","b"], tolerating
// quotes and blanks (the kernel list form).
func List(v string) []string { return record.ParseList(v) }

// ExtractJSONArray pulls the structured output an agent appends to its reply: the
// LAST fenced code block whose content is a JSON array (```json … ```), or the whole
// text if it is itself a JSON array. Returns the array text and whether one was found.
func ExtractJSONArray(raw string) (string, bool) {
	var last string
	found := false
	rest := raw
	for {
		i := strings.Index(rest, "```")
		if i < 0 {
			break
		}
		rest = rest[i+3:]
		if nl := strings.IndexByte(rest, '\n'); nl >= 0 { // skip the ```json language token
			rest = rest[nl+1:]
		}
		j := strings.Index(rest, "```")
		if j < 0 {
			break
		}
		block := strings.TrimSpace(rest[:j])
		rest = rest[j+3:]
		if strings.HasPrefix(block, "[") {
			last, found = block, true
		}
	}
	if found {
		return last, true
	}
	if t := strings.TrimSpace(raw); strings.HasPrefix(t, "[") {
		return t, true
	}
	return "", false
}

// ExtractFencedBlock returns the content of the FIRST fenced code block whose
// info string equals lang — e.g. ExtractFencedBlock(body, "proposed") for the
// ````proposed … ```` payload the write_approval cast emits. Fences may open
// with 3 or more backticks; a block opened with N backticks is closed only by a
// line of N-or-more backticks, so ordinary 3-backtick fences nested inside a
// 4-backtick `proposed` block never close it early. Returns the block's inner
// text (fence lines excluded) and whether such a block was found.
func ExtractFencedBlock(raw, lang string) (string, bool) {
	lines := strings.Split(raw, "\n")
	for i := 0; i < len(lines); i++ {
		n := countLeadingBackticks(lines[i])
		if n < 3 || strings.TrimSpace(lines[i][n:]) != lang {
			continue
		}
		var body []string
		for j := i + 1; j < len(lines); j++ {
			m := countLeadingBackticks(lines[j])
			if m >= n && strings.TrimSpace(lines[j]) == strings.Repeat("`", m) {
				return strings.Join(body, "\n"), true // matched closing fence
			}
			body = append(body, lines[j])
		}
		return strings.Join(body, "\n"), true // opener with no closer — tolerate
	}
	return "", false
}

func countLeadingBackticks(s string) int {
	n := 0
	for n < len(s) && s[n] == '`' {
		n++
	}
	return n
}

// Writer builds a `---`-delimited frontmatter document with ordered fields plus a
// body, mirroring the marshalApproval style. Ordered (not a map) so serialized
// files are stable and diff-friendly.
type Writer struct {
	fields []string // pre-rendered "key: value" lines, in insertion order
}

// Set adds a scalar field (skipped when empty, to keep files clean).
func (w *Writer) Set(key, val string) *Writer {
	if strings.TrimSpace(val) != "" {
		w.fields = append(w.fields, key+": "+val)
	}
	return w
}

// SetRaw adds a field even when the value is empty (for keys whose presence matters).
func (w *Writer) SetRaw(key, val string) *Writer {
	w.fields = append(w.fields, key+": "+val)
	return w
}

// SetList renders items as `[a, b, c]` (omitted when empty).
func (w *Writer) SetList(key string, items []string) *Writer {
	if len(items) == 0 {
		return w
	}
	return w.SetRaw(key, "["+strings.Join(items, ", ")+"]")
}

// String renders the document: frontmatter block + a blank line + trimmed body.
func (w *Writer) String(body string) string {
	var b strings.Builder
	b.WriteString("---\n")
	for _, f := range w.fields {
		b.WriteString(f)
		b.WriteByte('\n')
	}
	b.WriteString("---\n")
	body = strings.TrimRight(body, "\n")
	if body != "" {
		b.WriteString("\n")
		b.WriteString(body)
		b.WriteString("\n")
	}
	return b.String()
}
