package aion

import "strings"

// ParseFinances reads finances.md: the frontmatter block is the data; the
// body is private and preserved verbatim (never rendered, never exported).
func ParseFinances(content string) *FinancesDoc {
	d := &FinancesDoc{}
	d.DocFM, d.Body = parseDocFM(content)
	return d
}

// SerializeFinances is the fixpoint emitter for finances.md.
func SerializeFinances(d *FinancesDoc) string { return d.emit(d.Body) }

// Get returns the frontmatter scalar for key ("" when absent).
func (d *FinancesDoc) Get(key string) string {
	for _, ln := range d.FM {
		if k, v, ok := strings.Cut(ln, ":"); ok && strings.EqualFold(strings.TrimSpace(k), key) {
			return strings.TrimSpace(v)
		}
	}
	return ""
}

// Set upserts a frontmatter scalar, preserving line order and formatting of
// untouched keys. A missing key appends at the end of the block; a missing
// block is created.
func (d *FinancesDoc) Set(key, value string) {
	line := key + ": " + value
	for i, ln := range d.FM {
		if k, _, ok := strings.Cut(ln, ":"); ok && strings.EqualFold(strings.TrimSpace(k), key) {
			d.FM[i] = line
			return
		}
	}
	if d.FM == nil {
		d.FMBlank = d.Body != ""
	}
	d.FM = append(d.FM, line)
}
