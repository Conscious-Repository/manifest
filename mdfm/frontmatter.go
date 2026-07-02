// Package mdfm is a tiny hand-rolled markdown-frontmatter toolkit shared by the
// agent stores (profiles, feed, approvals). It is intentionally a YAML *subset*:
// `key: value` scalars and `key: [a, b]` / `key: a, b` lists, matching the style
// already used in app/agents (splitFrontmatter/parseList). No external dependency.
package mdfm

import "strings"

// Split parses a leading `---` frontmatter block into key→value pairs and returns
// the remaining body. Content without a leading `---` yields an empty map + the
// whole string as body.
func Split(content string) (map[string]string, string) {
	fm := map[string]string{}
	if !strings.HasPrefix(content, "---\n") {
		return fm, content
	}
	idx := strings.Index(content, "\n---")
	if idx < 0 {
		return fm, content
	}
	block := content[4:idx]
	rest := content[idx+4:]
	if nl := strings.IndexByte(rest, '\n'); nl >= 0 {
		rest = rest[nl+1:]
	}
	for _, line := range strings.Split(block, "\n") {
		if k, v, ok := strings.Cut(line, ":"); ok {
			fm[strings.TrimSpace(k)] = strings.TrimSpace(v)
		}
	}
	return fm, strings.TrimPrefix(rest, "\n")
}

// List parses a frontmatter list value: "[a, b]" or "a, b" → ["a","b"], tolerating
// quotes and blanks.
func List(v string) []string {
	v = strings.TrimSpace(v)
	v = strings.TrimPrefix(v, "[")
	v = strings.TrimSuffix(v, "]")
	if v == "" {
		return nil
	}
	var out []string
	for _, p := range strings.Split(v, ",") {
		if p = strings.TrimSpace(strings.Trim(p, `"'`)); p != "" {
			out = append(out, p)
		}
	}
	return out
}

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
