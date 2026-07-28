package record

import (
	"bufio"
	"os"
	"strings"
)

// SplitFrontmatter is THE fence grammar: a file whose first line is exactly
// `---` opens a YAML-subset block closed by a `---` (or YAML's `...`) line.
// Returns the raw block lines (CR stripped), the body (one leading blank line
// dropped, matching every existing reader), and ok=false when there is no
// terminated block (then body is the whole content). Key/value POLICY —
// scalar-map vs multi-value, list handling — belongs to the caller; the
// kernel owns only the fences.
func SplitFrontmatter(content string) (block []string, body string, ok bool) {
	if !strings.HasPrefix(content, "---\n") && !strings.HasPrefix(content, "---\r\n") {
		return nil, content, false
	}
	lines := strings.Split(content, "\n")
	end := -1
	for i := 1; i < len(lines); i++ {
		if t := strings.TrimRight(lines[i], "\r"); t == "---" || t == "..." {
			end = i
			break
		}
	}
	if end < 0 {
		return nil, content, false // unterminated block: treat as no frontmatter
	}
	block = make([]string, 0, end-1)
	for _, ln := range lines[1:end] {
		block = append(block, strings.TrimRight(ln, "\r"))
	}
	if end+1 < len(lines) {
		body = strings.Join(lines[end+1:], "\n")
	}
	return block, strings.TrimPrefix(body, "\n"), true
}

// Unquote strips one matched pair of wrapping quotes ("…" or '…') after
// trimming; values are otherwise preserved EXACTLY (surface, don't rewrite).
func Unquote(s string) string {
	s = strings.TrimSpace(s)
	if len(s) >= 2 {
		if (s[0] == '"' && s[len(s)-1] == '"') || (s[0] == '\'' && s[len(s)-1] == '\'') {
			return s[1 : len(s)-1]
		}
	}
	return s
}

// ParseList parses a frontmatter list value — `[a, b, "c"]` or the bare
// `a, b` form — tolerating a missing closing bracket, quotes, and blanks.
func ParseList(v string) []string {
	v = strings.TrimSpace(v)
	v = strings.TrimPrefix(v, "[")
	v = strings.TrimSuffix(v, "]")
	if strings.TrimSpace(v) == "" {
		return nil
	}
	var out []string
	for _, p := range strings.Split(v, ",") {
		if s := Unquote(strings.TrimSpace(p)); s != "" {
			out = append(out, s)
		}
	}
	return out
}

// FrontmatterScalar streams just the leading frontmatter of the file at path
// and returns the flat `key: value` scalar (unquoted), "" when the file has no
// frontmatter or no such key. It reads only up to the closing fence and never
// loads the whole file, so it is cheap to call on every note during a scan.
func FrontmatterScalar(path, key string) string {
	f, err := os.Open(path)
	if err != nil {
		return ""
	}
	defer f.Close()

	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	if !sc.Scan() {
		return ""
	}
	if strings.TrimRight(sc.Text(), "\r") != "---" {
		return "" // no frontmatter fence
	}
	for sc.Scan() {
		line := strings.TrimRight(sc.Text(), "\r")
		if line == "---" || line == "..." {
			return "" // closing fence, key not found
		}
		i := strings.IndexByte(line, ':')
		if i <= 0 {
			continue
		}
		if strings.EqualFold(strings.TrimSpace(line[:i]), key) {
			return Unquote(strings.TrimSpace(line[i+1:]))
		}
	}
	return ""
}
