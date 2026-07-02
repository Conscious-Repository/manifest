package vaultindex

import "strings"

// splitFrontmatter parses a leading `---` YAML block into key → values and
// returns the remaining body. It is a deliberate YAML SUBSET that covers what
// this vault actually uses (audit §0): scalars (`key: value`), inline lists
// (`key: [a, b]`), and block dash-lists (`key:\n  - a\n  - b`) — the block form
// covers most meeting notes. Every value is returned as a []string (a scalar is
// a one-element slice) so callers treat `categories: [x]` and the block form
// identically. Values are unquoted but otherwise preserved EXACTLY (no
// normalization — that is the whole point of the audit's "surface, don't
// rewrite" rule). A file without a leading `---` yields an empty map + itself.
func splitFrontmatter(content string) (map[string][]string, string) {
	fm := map[string][]string{}
	if !strings.HasPrefix(content, "---\n") && !strings.HasPrefix(content, "---\r\n") {
		return fm, content
	}
	lines := strings.Split(content, "\n")
	// find the closing fence (first `---` line after the opener)
	end := -1
	for i := 1; i < len(lines); i++ {
		if strings.TrimRight(lines[i], "\r") == "---" {
			end = i
			break
		}
	}
	if end < 0 {
		return fm, content // unterminated block: treat as no frontmatter
	}

	block := lines[1:end]
	for i := 0; i < len(block); i++ {
		line := strings.TrimRight(block[i], "\r")
		if strings.TrimSpace(line) == "" || strings.HasPrefix(strings.TrimSpace(line), "#") {
			continue
		}
		// only treat top-level (unindented) "key:" lines as keys; indented lines
		// are handled as block-list items by the look-ahead below.
		if line[0] == ' ' || line[0] == '\t' || line[0] == '-' {
			continue
		}
		key, rest, ok := strings.Cut(line, ":")
		if !ok {
			continue
		}
		key = strings.TrimSpace(key)
		rest = strings.TrimSpace(rest)
		switch {
		case rest == "":
			// block list: consume following `  - item` lines
			var items []string
			for j := i + 1; j < len(block); j++ {
				t := strings.TrimSpace(strings.TrimRight(block[j], "\r"))
				if t == "" {
					continue
				}
				if !strings.HasPrefix(t, "- ") && t != "-" {
					break
				}
				items = append(items, unquote(strings.TrimSpace(strings.TrimPrefix(t, "-"))))
				i = j
			}
			fm[key] = append(fm[key], nonEmpty(items)...)
		case strings.HasPrefix(rest, "["):
			fm[key] = append(fm[key], parseInlineList(rest)...)
		default:
			fm[key] = append(fm[key], unquote(rest))
		}
	}

	// body = everything after the closing fence line
	body := ""
	if end+1 < len(lines) {
		body = strings.Join(lines[end+1:], "\n")
	}
	return fm, strings.TrimPrefix(body, "\n")
}

// parseInlineList parses `[a, b, "c"]` → ["a","b","c"], tolerating a missing
// closing bracket and quotes/blanks.
func parseInlineList(v string) []string {
	v = strings.TrimSpace(v)
	v = strings.TrimPrefix(v, "[")
	v = strings.TrimSuffix(v, "]")
	if strings.TrimSpace(v) == "" {
		return nil
	}
	var out []string
	for _, p := range strings.Split(v, ",") {
		if s := unquote(strings.TrimSpace(p)); s != "" {
			out = append(out, s)
		}
	}
	return out
}

func unquote(s string) string {
	s = strings.TrimSpace(s)
	if len(s) >= 2 {
		if (s[0] == '"' && s[len(s)-1] == '"') || (s[0] == '\'' && s[len(s)-1] == '\'') {
			return s[1 : len(s)-1]
		}
	}
	return s
}

func nonEmpty(xs []string) []string {
	var out []string
	for _, x := range xs {
		if strings.TrimSpace(x) != "" {
			out = append(out, x)
		}
	}
	return out
}
