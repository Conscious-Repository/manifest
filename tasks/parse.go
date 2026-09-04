package tasks

import (
	"regexp"
	"strings"

	"manifest/record"
)

var (
	headingRe    = regexp.MustCompile(`^##[ \t]+(.*\S)\s*$`)
	subHeadingRe = regexp.MustCompile(`^###[ \t]+(.*\S)\s*$`)
	todoLineRe   = record.CheckboxRe // indent m[1], state m[2], rest m[3]
	bulletLineRe = regexp.MustCompile(`^[ \t]*[-*]\s+\S`)
	// the kernel grammar + its wikilink-value affordance (waiting values may
	// BE a [[wikilink]], matched before the generic scan)
	inlineFieldRe = record.FieldRe
	waitingLinkRe = record.WikilinkValueRe("waiting")
	linkRe        = regexp.MustCompile(`\[\[([^\]]+)\]\]`)
)

// sub-section modes within a domain
const (
	modeLoose = iota
	modeBucket
	modeIssues
	modeBacklog
)

// Parse reads `tasks.md` bytes into a Doc. Domain sections (`## X`) hold, in
// canonical order: loose todos → `### <bucket>` blocks → `### issues` →
// `### backlog`. Tolerant: unknown lines round-trip verbatim (preamble before
// the first heading, extra lines inside a domain — the [[x posts]] tail
// survives untouched; backlog lines are verbatim plain bullets).
func Parse(raw string) *Doc {
	d := &Doc{}
	var cur *Domain
	mode := modeLoose
	var bucket *Bucket
	for _, line := range strings.Split(strings.ReplaceAll(raw, "\r\n", "\n"), "\n") {
		if m := headingRe.FindStringSubmatch(line); m != nil && !strings.HasPrefix(strings.TrimSpace(line), "###") {
			cur = &Domain{Name: strings.TrimSpace(m[1])}
			d.Domains = append(d.Domains, cur)
			mode, bucket = modeLoose, nil
			continue
		}
		if cur == nil {
			d.preamble = append(d.preamble, line)
			continue
		}
		if m := subHeadingRe.FindStringSubmatch(line); m != nil {
			head := strings.TrimSpace(m[1])
			switch strings.ToLower(head) {
			case "issues":
				mode, bucket = modeIssues, nil
			case "backlog":
				mode, bucket = modeBacklog, nil
			default:
				bucket = parseBucketHeading(head)
				cur.Buckets = append(cur.Buckets, bucket)
				mode = modeBucket
			}
			continue
		}
		if m := todoLineRe.FindStringSubmatch(line); m != nil {
			switch mode {
			case modeIssues:
				cur.Issues = append(cur.Issues, parseIssue(m[2] != " ", m[3]))
			case modeBacklog:
				cur.Backlog = append(cur.Backlog, line) // checkboxes in backlog stay verbatim
			case modeBucket:
				bucket.Tasks = append(bucket.Tasks, parseTask(m[2] != " ", m[3]))
			default:
				cur.Tasks = append(cur.Tasks, parseTask(m[2] != " ", m[3]))
			}
			continue
		}
		if strings.TrimSpace(line) == "" {
			continue
		}
		if mode == modeBacklog && bulletLineRe.MatchString(line) {
			cur.Backlog = append(cur.Backlog, line)
			continue
		}
		cur.extra = append(cur.extra, line)
	}
	d.assignIDs()
	return d
}

// parseBucketHeading reads `<name> · [[link]] [[link]] [bucket:: slug]`.
func parseBucketHeading(head string) *Bucket {
	b := &Bucket{}
	head = inlineFieldRe.ReplaceAllStringFunc(head, func(m string) string {
		sm := inlineFieldRe.FindStringSubmatch(m)
		if strings.EqualFold(strings.TrimSpace(sm[1]), "bucket") {
			b.Slug = strings.TrimSpace(sm[2])
			b.pinned = true
			return ""
		}
		return m
	})
	head = linkRe.ReplaceAllStringFunc(head, func(m string) string {
		b.Links = append(b.Links, strings.TrimSpace(linkRe.FindStringSubmatch(m)[1]))
		return ""
	})
	head = strings.TrimSpace(head)
	head = strings.TrimRight(head, "· \t")
	b.Name = strings.TrimSpace(head)
	if b.Slug == "" {
		b.Slug = slug(b.Name)
	}
	return b
}

func parseTask(checked bool, rest string) *Task {
	t := &Task{Checked: checked}
	rest = waitingLinkRe.ReplaceAllStringFunc(rest, func(m string) string {
		t.Waiting = strings.TrimSpace(waitingLinkRe.FindStringSubmatch(m)[1])
		return ""
	})
	var unknown []Field
	clean := inlineFieldRe.ReplaceAllStringFunc(rest, func(m string) string {
		sm := inlineFieldRe.FindStringSubmatch(m)
		key, val := strings.TrimSpace(sm[1]), strings.TrimSpace(sm[2])
		switch strings.ToLower(key) {
		case "todo":
			unknown = append(unknown, Field{Key: "todo", Value: val}) // identity, kept in Fields for explicitID
		case "added":
			t.Added = val
		case "done":
			t.Done = val
		case "waiting":
			t.Waiting = val
		case "since":
			t.Since = val
		case "rock":
			t.Rock = val
		case "issue":
			t.Issue = val
		case "stage":
			t.Stage = val
		case "owner":
			t.Owner = val
		case "rank":
			t.Rank = val
		case "priority":
			// closed set: anything else stays verbatim as an unknown field
			// (never dropped, never guessed) and reads as no priority
			if p, ok := NormalizePriority(val); ok {
				t.Priority = p
			} else {
				unknown = append(unknown, Field{Key: sm[1], Value: val})
			}
		case "depends":
			t.Depends = append(t.Depends, splitDepends(val)...)
		default:
			unknown = append(unknown, Field{Key: sm[1], Value: val})
		}
		return ""
	})
	t.Fields = unknown
	t.Depends = cleanDepends(t.Depends) // two [depends::] fields merge, duplicates drop
	t.Text = strings.Join(strings.Fields(clean), " ")
	return t
}

func parseIssue(checked bool, rest string) *Issue {
	is := &Issue{Checked: checked}
	var unknown []Field
	clean := inlineFieldRe.ReplaceAllStringFunc(rest, func(m string) string {
		sm := inlineFieldRe.FindStringSubmatch(m)
		key, val := strings.TrimSpace(sm[1]), strings.TrimSpace(sm[2])
		switch strings.ToLower(key) {
		case "issue":
			is.ID = val
			is.explicit = true
		case "added":
			is.Added = val
		case "done":
			is.Done = val
		case "resolution":
			is.Resolution = val
		default:
			unknown = append(unknown, Field{Key: sm[1], Value: val})
		}
		return ""
	})
	is.Fields = unknown
	is.Text = strings.Join(strings.Fields(clean), " ")
	return is
}
