package aion

import (
	"strings"

	"manifest/record"
)

// parseDocFM splits a corpus file into its frontmatter carrier and body,
// recovering the fence/body blank-line fact the kernel drops (length
// accounting — exact for LF files; a CRLF file fails fixpoint upstream
// anyway because the kernel CR-strips block lines).
func parseDocFM(content string) (DocFM, string) {
	fm, body, ok := record.SplitFrontmatter(content)
	if !ok {
		return DocFM{}, content
	}
	base := len("---\n") + len("---\n") + len(body)
	for _, ln := range fm {
		base += len(ln) + 1
	}
	return DocFM{FM: fm, FMBlank: len(content) > base}, body
}

// emit renders the frontmatter block back around a body.
func (d DocFM) emit(body string) string {
	if d.FM == nil {
		return body
	}
	var b strings.Builder
	b.WriteString("---\n")
	for _, ln := range d.FM {
		b.WriteString(ln)
		b.WriteByte('\n')
	}
	b.WriteString("---\n")
	if d.FMBlank {
		b.WriteByte('\n')
	}
	b.WriteString(body)
	return b.String()
}

// bodyLines splits a body into lines, canonicalizing to exactly one trailing
// newline on emit (joinLines).
func bodyLines(body string) []string {
	if body == "" {
		return nil
	}
	return strings.Split(strings.TrimSuffix(body, "\n"), "\n")
}

func joinLines(lines []string) string {
	if len(lines) == 0 {
		return ""
	}
	return strings.Join(lines, "\n") + "\n"
}

// bulletContent returns the content of a top-level plain bullet ("- …") that
// is NOT a checkbox line; ok=false otherwise.
func bulletContent(line string) (string, bool) {
	if !strings.HasPrefix(line, "- ") || record.CheckboxRe.MatchString(line) {
		return "", false
	}
	return line[2:], true
}

// composeLine renders "- " (or a checkbox prefix) + text + fields.
func composeLine(prefix, text string, fields []string) string {
	parts := make([]string, 0, 1+len(fields))
	if text != "" {
		parts = append(parts, text)
	}
	parts = append(parts, fields...)
	return prefix + strings.Join(parts, " ")
}
