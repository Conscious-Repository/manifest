package gmailsync

// The DETERMINISTIC half of portal email-sync: converting fetched Gmail
// thread messages into the candidate-note format. Ported from the engine's
// email_convert.go so the two pipelines write the same shape — one
// "## YYYY-MM-DD — <sender>" section per message, verbatim sender text with
// only transport encoding, markup, quoted history, and signatures removed.
// No model, no network: pure and unit-tested.

import (
	"html"
	"regexp"
	"strings"
)

// Resolver names an email address (the roster filter + display names). The
// engine uses the vault index here; the portal uses the OODA roster.
type Resolver interface {
	PersonByEmail(email string) (display string, ok bool)
}

var (
	// quoted-history markers: everything from the first match to the end of the
	// message is dropped (it repeats earlier messages already in the note).
	quoteAttribRe = regexp.MustCompile(`(?m)^On .{5,120}wrote: *$`)
	origMsgRe     = regexp.MustCompile(`(?mi)^-+ *(original|forwarded) message *-+ *$`)
	// a forwarded/reply header block: a "From:" line directly followed by
	// Sent/Date/To lines (Outlook-style quoting).
	fwdHeaderRe = regexp.MustCompile(`(?m)^From: .+\r?\n(Sent|Date): `)
	// signature delimiter (RFC 3676 "-- ")
	sigRe = regexp.MustCompile(`(?m)^-- ?$`)
	// address extraction out of a From/To/Cc header list
	emailAddrRe = regexp.MustCompile(`[A-Za-z0-9._%+\-]+@[A-Za-z0-9.\-]+\.[A-Za-z]{2,}`)
	// Re:/Fwd: prefixes on subjects (repeatable, case-blind)
	subjectPrefixRe = regexp.MustCompile(`(?i)^(re|fwd?|aw):\s*`)
	blankRunRe      = regexp.MustCompile(`\n{3,}`)
	htmlTagRe       = regexp.MustCompile(`(?s)<[^>]*>`)
	htmlDropRe      = regexp.MustCompile(`(?si)<(style|script|head)[^>]*>.*?</(style|script|head)>`)
	htmlBreakRe     = regexp.MustCompile(`(?i)<(/p|br[^>]*|/div|/tr|/li|/h[1-6])>`)
	// filenameStrip removes the characters banned from a note filename.
	filenameStrip = regexp.MustCompile(`[\[\]<>:"/\\|?*]`)
)

// stripHTML is the crude text/html fallback: block markup becomes newlines,
// tags drop, entities unescape. Ugly but verbatim-safe.
func stripHTML(s string) string {
	s = htmlDropRe.ReplaceAllString(s, "")
	s = htmlBreakRe.ReplaceAllString(s, "$0\n")
	s = htmlTagRe.ReplaceAllString(s, "")
	s = html.UnescapeString(s)
	lines := strings.Split(s, "\n")
	for i := range lines {
		lines[i] = strings.TrimRight(lines[i], " \t\r")
	}
	return strings.TrimSpace(blankRunRe.ReplaceAllString(strings.Join(lines, "\n"), "\n\n"))
}

// cleanEmailBody cuts quoted history and the trailing signature from one
// message body. Heuristic and lossy-SAFE: the worst failure mode is extra
// verbatim quoted text remaining in the note, never fabricated text.
func cleanEmailBody(text string) string {
	text = strings.ReplaceAll(text, "\r\n", "\n")
	cut := len(text)
	for _, re := range []*regexp.Regexp{quoteAttribRe, fwdHeaderRe, origMsgRe} {
		if loc := re.FindStringIndex(text); loc != nil && loc[0] < cut {
			cut = loc[0]
		}
	}
	text = text[:cut]
	// drop a trailing run of ">"-quoted lines (quotes with no attribution
	// line); interior "> " quotes stay — inline replies quote deliberately
	lines := strings.Split(text, "\n")
	end := len(lines)
	for end > 0 {
		t := strings.TrimSpace(lines[end-1])
		if t == "" || strings.HasPrefix(t, ">") {
			end--
			continue
		}
		break
	}
	text = strings.Join(lines[:end], "\n")
	if loc := sigRe.FindAllStringIndex(text, -1); len(loc) > 0 {
		text = text[:loc[len(loc)-1][0]]
	}
	text = blankRunRe.ReplaceAllString(text, "\n\n")
	return strings.TrimSpace(text)
}

// headerAddresses pulls every email address out of a From/To/Cc header value.
func headerAddresses(header string) []string {
	return emailAddrRe.FindAllString(header, -1)
}

// senderDisplay names a message's sender: the resolved roster person when the
// address is known, else the header's display name, else the bare address.
func senderDisplay(from string, res Resolver) string {
	addrs := headerAddresses(from)
	if res != nil && len(addrs) > 0 {
		if d, ok := res.PersonByEmail(strings.ToLower(addrs[0])); ok {
			return d
		}
	}
	if i := strings.Index(from, "<"); i > 0 {
		if name := strings.Trim(strings.TrimSpace(from[:i]), `"`); name != "" {
			return name
		}
	}
	if len(addrs) > 0 {
		return addrs[0]
	}
	return strings.TrimSpace(from)
}

// MessageSections renders the note body — one "## YYYY-MM-DD — <sender>"
// heading per message with the cleaned body below.
func MessageSections(msgs []Msg, res Resolver) string {
	var b strings.Builder
	for _, m := range msgs {
		body := cleanEmailBody(m.Body)
		if body == "" {
			body = "(no text body)"
		}
		b.WriteString("## ")
		b.WriteString(m.Internal.Format("2006-01-02"))
		b.WriteString(" — ")
		b.WriteString(senderDisplay(m.From, res))
		b.WriteString("\n\n")
		b.WriteString(body)
		b.WriteString("\n\n")
	}
	return strings.TrimRight(b.String(), "\n")
}

// ParticipantsLine unions every From/To/Cc address across the thread and
// names the KNOWN roster people. The mailbox owner is never listed; unknown
// addresses are not listed — the roster filter already guaranteed at least
// one resolves. (The engine renders [[wikilinks]] here; the portal store is
// not a vault, so plain names.)
func ParticipantsLine(msgs []Msg, res Resolver, ownEmail string) string {
	seen := map[string]bool{}
	var names []string
	for _, m := range msgs {
		for _, header := range []string{m.From, m.To, m.Cc} {
			for _, addr := range headerAddresses(header) {
				lower := strings.ToLower(addr)
				if seen[lower] || strings.EqualFold(lower, ownEmail) {
					seen[lower] = true
					continue
				}
				seen[lower] = true
				if res == nil {
					continue
				}
				if d, ok := res.PersonByEmail(lower); ok && strings.TrimSpace(d) != "" {
					dup := false
					for _, n := range names {
						if n == d {
							dup = true
							break
						}
					}
					if !dup {
						names = append(names, d)
					}
				}
			}
		}
	}
	return strings.Join(names, " · ")
}

// ThreadNote renders the full candidate note: the participants line, then the
// message sections. No frontmatter — the candidate record itself carries the
// identity (thread id, account, filename); the note is what a member reads
// and what the extractor is later handed.
func ThreadNote(msgs []Msg, res Resolver, ownEmail string) string {
	var b strings.Builder
	if line := ParticipantsLine(msgs, res, ownEmail); line != "" {
		b.WriteString(line)
		b.WriteString("\n\n")
	}
	b.WriteString(MessageSections(msgs, res))
	return strings.TrimRight(b.String(), "\n") + "\n"
}

// NoteFilename builds the thread note's date-range filename: the FIRST
// message date, then " - <last message date>" once the thread spans days,
// then the subject (Re:/Fwd: prefixes stripped; empty subject falls back to
// "email thread"). Same-day threads stay single-dated.
func NoteFilename(msgs []Msg, subject string) string {
	for subjectPrefixRe.MatchString(subject) {
		subject = subjectPrefixRe.ReplaceAllString(subject, "")
	}
	subject = strings.TrimSpace(subject)
	if subject == "" {
		subject = "email thread"
	}
	clean := strings.TrimSpace(filenameStrip.ReplaceAllString(subject, ""))
	clean = strings.Join(strings.Fields(clean), " ")
	if clean == "" {
		clean = "untitled"
	}
	first := msgs[0].Internal.Format("2006-01-02")
	last := msgs[len(msgs)-1].Internal.Format("2006-01-02")
	name := strings.ToLower(first + " " + clean + ".md")
	if last == first {
		return name
	}
	return first + " - " + last + strings.TrimPrefix(name, first)
}

// calendarSubjectRe matches calendar-machinery subjects: invitations, RSVP
// echoes, updates, and cancellations (Google/Outlook shapes).
var calendarSubjectRe = regexp.MustCompile(`(?i)^\s*(invitation|updated invitation|accepted|declined|tentatively accepted|tentative|canceled event|cancelled event|new event|rescheduled( event)?|reminder)\b *[:—-]? `)

func isCalendarMsg(m Msg) bool {
	if m.HasCalendar {
		return true
	}
	from := strings.ToLower(m.From)
	if strings.Contains(from, "calendar-notification@google.com") ||
		strings.Contains(from, "via google calendar") {
		return true
	}
	return calendarSubjectRe.MatchString(m.Subject)
}

// IsCalendarThread reports whether a whole thread is calendar machinery —
// EVERY message is an invite/RSVP/update. A thread where humans also wrote
// real messages around an invite stays in.
func IsCalendarThread(msgs []Msg) bool {
	if len(msgs) == 0 {
		return false
	}
	for _, m := range msgs {
		if !isCalendarMsg(m) {
			return false
		}
	}
	return true
}
