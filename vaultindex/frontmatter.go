package vaultindex

import (
	"strings"

	"manifest/record"
)

// splitFrontmatter parses a leading `---` YAML block into key → values and
// returns the remaining body. The fence grammar, unquoting, and inline-list
// form come from the kernel (record); what stays HERE is this index's read
// POLICY — the deliberate YAML subset the vault actually uses (audit §0):
// scalars (`key: value`), inline lists (`key: [a, b]`), and block dash-lists
// (`key:\n  - a\n  - b`). Every value is returned as a []string (a scalar is
// a one-element slice) so callers treat `categories: [x]` and the block form
// identically. Values are unquoted but otherwise preserved EXACTLY (no
// normalization — that is the whole point of the audit's "surface, don't
// rewrite" rule). A file without a leading `---` yields an empty map + itself.
func splitFrontmatter(content string) (map[string][]string, string) {
	fm := map[string][]string{}
	block, body, ok := record.SplitFrontmatter(content)
	if !ok {
		return fm, body
	}

	for i := 0; i < len(block); i++ {
		line := block[i]
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
				t := strings.TrimSpace(block[j])
				if t == "" {
					continue
				}
				if !strings.HasPrefix(t, "- ") && t != "-" {
					break
				}
				items = append(items, record.Unquote(strings.TrimSpace(strings.TrimPrefix(t, "-"))))
				i = j
			}
			fm[key] = append(fm[key], nonEmpty(items)...)
		case strings.HasPrefix(rest, "["):
			fm[key] = append(fm[key], record.ParseList(rest)...)
		default:
			fm[key] = append(fm[key], record.Unquote(rest))
		}
	}
	return fm, body
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
