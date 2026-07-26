package todos

import (
	"os"
	"strings"
	"time"
)

// Migrate performs the ONE-TIME refactor of the legacy hand-written `to do.md`
// (2026-07 shape: plain bullets, pseudo-groups, no headings) into the domain
// grammar. Owner-approved mapping: loose items → Personal · "bio" group →
// Aion with "bio · " prefix · "real estate" group → Real Estate · "blogs"
// group → Personal with "blog · " prefix; trailing non-bullet lines (the
// [[x posts]] link) survive verbatim at the bottom. Nothing is deleted; the
// original is kept at `<name>.pre-migration`. Idempotent: a file that already
// carries ## headings or [todo::] ids is left untouched.
func (s *Store) Migrate(now time.Time, areas []string) (bool, error) {
	raw, err := os.ReadFile(s.Path())
	if err != nil {
		if os.IsNotExist(err) {
			return false, nil
		}
		return false, err
	}
	content := string(raw)
	if strings.Contains(content, "\n## ") || strings.HasPrefix(content, "## ") ||
		strings.Contains(content, "[todo::") {
		return false, nil // already in the new grammar
	}
	if err := os.WriteFile(s.Path()+".pre-migration", raw, 0o644); err != nil {
		return false, err
	}

	d := &Doc{preamble: []string{"# To Do"}}
	d.EnsureDomain(InboxName)
	for _, a := range areas { // live goals areas, one shared vocabulary
		d.EnsureDomain(a)
	}
	today := now.Format("2006-01-02")
	add := func(domain, text string) {
		text = strings.Join(strings.Fields(text), " ")
		if text == "" {
			return
		}
		dom := d.EnsureDomain(domain)
		dom.Todos = append(dom.Todos, &Todo{Text: text, Added: today})
	}
	// group name → (domain, prefix); unmatched groups land in the Inbox
	groupOf := func(name string) (string, string) {
		n := strings.ToLower(strings.TrimSpace(strings.TrimSuffix(name, ":")))
		switch {
		case strings.Contains(n, "bio"):
			return "Aion", "bio · "
		case strings.Contains(n, "real estate"):
			return "Real Estate", ""
		case strings.Contains(n, "blog"):
			return "Personal", "blog · "
		default:
			return InboxName, strings.TrimSpace(name) + " · "
		}
	}

	var tail []string
	curDomain, curPrefix := "", ""
	for _, line := range strings.Split(strings.ReplaceAll(content, "\r\n", "\n"), "\n") {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" {
			continue
		}
		isBullet := strings.HasPrefix(trimmed, "- ") || strings.HasPrefix(trimmed, "* ")
		indented := line != strings.TrimLeft(line, " \t")
		switch {
		case isBullet && !indented:
			text := strings.TrimSpace(trimmed[2:])
			dom, prefix := groupOf(text)
			if dom == InboxName && !strings.HasSuffix(text, ":") {
				add("Personal", text) // a loose item, not a group header
				curDomain, curPrefix = "", ""
			} else {
				curDomain, curPrefix = dom, prefix
			}
		case isBullet && indented && curDomain != "":
			add(curDomain, curPrefix+strings.TrimSpace(trimmed[2:]))
		case !isBullet:
			tail = append(tail, line) // e.g. [[x posts]]
		}
	}
	if len(tail) > 0 && len(d.Domains) > 0 {
		last := d.Domains[len(d.Domains)-1]
		last.extra = append(last.extra, tail...)
	}
	return true, s.Save(d)
}
