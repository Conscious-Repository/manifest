package record

import "strings"

// Archives come in exactly two policies (ARCHITECTURE §2):
//
//   - append-only dated (to do archive.md): lines accrue under dated `##`
//     section headings and are never rewritten — AppendSection below.
//   - structured round-trip (goals <quarter>.md): the domain re-parses and
//     re-serializes typed entries through the kernel grammar; the fixpoint
//     contract is the domain's serializer, so no engine lives here.

// AppendSection implements the append-only policy as a pure content
// transform (file I/O stays with the caller's writer): ensure the doc exists
// with its title line, ensure a `## section` heading (one already present
// anywhere is reused — sections are dated and grow chronologically, so the
// reused heading is the trailing one), and append the lines at the end.
func AppendSection(content, title, section string, lines []string) string {
	if content == "" {
		content = title + "\n"
	}
	head := "## " + section
	if !strings.Contains(content, "\n"+head+"\n") && !strings.HasSuffix(content, head+"\n") {
		content = strings.TrimRight(content, "\n") + "\n\n" + head + "\n"
	}
	return strings.TrimRight(content, "\n") + "\n" + strings.Join(lines, "\n") + "\n"
}
