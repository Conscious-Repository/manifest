package aion

import (
	"fmt"
	"regexp"
	"sort"
	"strings"
	"time"
)

// The daily email digest for kairos (aion-context transcripts, Part 3): a
// PROJECTION over what the personal-email worker already produces for
// ben@aion.bio — the synced thread notes the vault index knows (subject,
// dates, participants) and the extractor's candidates for them (tasks,
// decisions, heuristics). Nothing here reads a mailbox, and nothing here
// renders a message body: a digest line is a subject, a date, who was on
// the thread, and the extractor's own one-line candidates.
//
// Sensitive-pattern routing: a thread whose subject or text matches a
// compensation / offer / negotiation / immigration / performance / legal /
// health / family pattern — or whose note the tier map holds — is NOT in the
// digest. It goes to the owner-only hold file (RenderEmailHold), outside
// kairos's readable tree; the digest says only how many were held.

// EmailAction is one extractor candidate for a thread, as the extractor
// titled it ("aion: task — <title>"): the kind and the title, never the quote.
type EmailAction struct {
	Kind  string // task | decision | heuristic
	Title string
}

// EmailThread is one synced mail thread as the projection sees it.
type EmailThread struct {
	Name     string // the note basename without .md
	ThreadID string
	Date     string // first message day (YYYY-MM-DD)
	EndDate  string // last message day ("" when single-day)
	Subject  string
	Senders  []string // people on the thread (display names) — no addresses
	Actions  []EmailAction
	Tier     Tier // from the tier map when the note is tiered
	Mapped   bool
	// Text is the note body, supplied ONLY for the sensitivity scan. It is
	// never rendered and never serialized (json:"-"), so the revision hash
	// over projected days cannot carry it either.
	Text string `json:"-"`
}

// ActivityDate is the day the thread files under: its last message day.
func (t EmailThread) ActivityDate() string {
	if t.EndDate != "" {
		return t.EndDate
	}
	return t.Date
}

// EmailHold is one thread routed to the owner's hold file, with the reason.
type EmailHold struct {
	Thread EmailThread
	Reason string
}

// EmailDay is one digest day: the threads kairos may see and the ones held.
type EmailDay struct {
	Date    string
	Threads []EmailThread
	Held    []EmailHold
}

// sensitivePattern is one routing rule; the label becomes the hold reason.
type sensitivePattern struct {
	Label string
	Re    *regexp.Regexp
}

// SensitivePatterns is the routing list, in evaluation order. Word-bounded,
// case-insensitive. Deliberately NOT matching bare "mri" or "clinical" —
// those are AION's research vocabulary, not personal health.
var SensitivePatterns = []sensitivePattern{
	{"compensation", regexp.MustCompile(`(?i)\b(compensation|comp package|salary|salaries|pay (raise|rise|band|range|bump)|raise request|bonus|equity grant|stock options?|option grant|vesting|cap table|409a|carried interest)\b`)},
	{"offer", regexp.MustCompile(`(?i)\b(job offer|offer letter|offer of employment|employment offer|term sheet|counter-?offer|signing bonus)\b`)},
	{"negotiation", regexp.MustCompile(`(?i)\b(negotiat\w*|counter-?propos\w*|walk[- ]away)\b`)},
	{"immigration", regexp.MustCompile(`(?i)\b(immigration|visa|h-?1b|o-?1 visa|green card|i-?140|i-?485|uscis|work permit|eb-?[123]|sponsorship)\b`)},
	{"performance", regexp.MustCompile(`(?i)\b(performance (review|improvement|plan)|underperform\w*|termination|let (him|her|them) go|firing|fired|severance|disciplinary|probation)\b`)},
	{"legal", regexp.MustCompile(`(?i)\b(legal|lawsuit|litigation|attorney|counsel|subpoena|settlement|nda|non-?compete|arbitration|indemnif\w*|cease and desist|privileged)\b`)},
	{"health", regexp.MustCompile(`(?i)\b(medical|diagnos\w*|surgery|hospital|therapist|therapy|mental health|sick leave|illness|pregnan\w*|maternity|paternity|disability|doctor'?s? appointment|health insurance|prescription|mri visit)\b`)},
	{"family", regexp.MustCompile(`(?i)\b(family|wife|husband|spouse|kids?|children|my son|my daughter|mom|dad|mother|father|wedding|funeral|divorce|bereavement|baby|childcare)\b`)},
}

// benignPhrases are business terms that would otherwise trip a pattern
// ("family office" is an investor, not a relative). They are blanked before
// the scan; anything else that matches still holds.
var benignPhrases = regexp.MustCompile(`(?i)\b(family offices?|legal name|legal entity|legal entities|therapy area|therapeutic)\b`)

// SensitiveReason reports the first pattern the text matches.
func SensitiveReason(text string) (string, bool) {
	text = benignPhrases.ReplaceAllString(text, " ")
	for _, p := range SensitivePatterns {
		if p.Re.MatchString(text) {
			return p.Label, true
		}
	}
	return "", false
}

// RouteEmailThread decides where a thread goes: "" = the digest; otherwise
// the hold reason. A held tier wins outright; then the pattern scan over
// subject + text. Unmapped notes are routed by the scan (new mail arrives
// daily and cannot wait for a tier), so a hit anywhere in the text holds it.
func RouteEmailThread(t EmailThread) string {
	if t.Mapped && t.Tier == TierHeld {
		return "tier: held"
	}
	if r, ok := SensitiveReason(t.Subject + "\n" + t.Text); ok {
		return "pattern: " + r
	}
	return ""
}

// ProjectEmailDays groups threads by activity day and applies the routing.
// Deterministic: days ascending, threads by name within a day.
func ProjectEmailDays(threads []EmailThread) []EmailDay {
	byDay := map[string]*EmailDay{}
	for _, t := range threads {
		day := t.ActivityDate()
		if day == "" {
			continue
		}
		d := byDay[day]
		if d == nil {
			d = &EmailDay{Date: day}
			byDay[day] = d
		}
		if reason := RouteEmailThread(t); reason != "" {
			d.Held = append(d.Held, EmailHold{Thread: t, Reason: reason})
		} else {
			d.Threads = append(d.Threads, t)
		}
	}
	days := make([]EmailDay, 0, len(byDay))
	for _, d := range byDay {
		sort.SliceStable(d.Threads, func(i, j int) bool { return d.Threads[i].Name < d.Threads[j].Name })
		sort.SliceStable(d.Held, func(i, j int) bool { return d.Held[i].Thread.Name < d.Held[j].Thread.Name })
		days = append(days, *d)
	}
	sort.Slice(days, func(i, j int) bool { return days[i].Date < days[j].Date })
	return days
}

// RenderEmailDigest is the kairos-facing file for one day. Topics (sender
// and subject words), threads, senders, decisions, action items; the held
// count; never a body, never a held subject.
func RenderEmailDigest(day EmailDay, rev string, at time.Time) string {
	var b strings.Builder
	b.WriteString(transcriptStamp("email digest · "+day.Date, rev, at))
	b.WriteString("Daily projection of ben@aion.bio mail the personal-email worker synced —\n" +
		"subjects, participants and the extractor's candidates. No message bodies\n" +
		"are in this pack. Threads matching a sensitive pattern (compensation,\n" +
		"offers, negotiation, immigration, performance, legal, health, family) are\n" +
		"not listed here; the owner reviews them separately.\n\n")
	fmt.Fprintf(&b, "Coverage: %d threads · %d items held for owner review\n\n", len(day.Threads), len(day.Held))

	if len(day.Threads) == 0 {
		b.WriteString("No shareable threads this day.\n")
		return b.String()
	}

	// topics: the distinct senders and the subject words, so a reader can
	// scan the day before opening a thread line
	senderCount := map[string]int{}
	var senders []string
	for _, t := range day.Threads {
		for _, s := range t.Senders {
			if senderCount[s] == 0 {
				senders = append(senders, s)
			}
			senderCount[s]++
		}
	}
	sort.Strings(senders)
	b.WriteString("## Topics\n")
	for _, t := range day.Threads {
		fmt.Fprintf(&b, "- %s\n", t.Subject)
	}
	b.WriteString("\n## Senders\n")
	if len(senders) == 0 {
		b.WriteString("- (no contacts resolved)\n")
	}
	for _, s := range senders {
		fmt.Fprintf(&b, "- %s (%d)\n", s, senderCount[s])
	}

	b.WriteString("\n## Threads\n")
	for _, t := range day.Threads {
		when := t.Date
		if t.EndDate != "" && t.EndDate != t.Date {
			when += " → " + t.EndDate
		}
		fmt.Fprintf(&b, "- **%s** (%s)", t.Subject, when)
		if len(t.Senders) > 0 {
			fmt.Fprintf(&b, " · with %s", strings.Join(t.Senders, ", "))
		}
		b.WriteString("\n")
	}

	decisions, actions := 0, 0
	for _, t := range day.Threads {
		for _, a := range t.Actions {
			if a.Kind == "decision" {
				decisions++
			} else {
				actions++
			}
		}
	}
	b.WriteString("\n## Decisions\n")
	if decisions == 0 {
		b.WriteString("- none extracted\n")
	}
	for _, t := range day.Threads {
		for _, a := range t.Actions {
			if a.Kind == "decision" {
				fmt.Fprintf(&b, "- %s — from: %s\n", a.Title, t.Subject)
			}
		}
	}
	b.WriteString("\n## Action items\n")
	if actions == 0 {
		b.WriteString("- none extracted\n")
	}
	for _, t := range day.Threads {
		for _, a := range t.Actions {
			if a.Kind != "decision" {
				fmt.Fprintf(&b, "- [%s] %s — from: %s\n", a.Kind, a.Title, t.Subject)
			}
		}
	}
	return b.String()
}

// RenderEmailHold is the OWNER-ONLY companion for one day: what was held and
// why. Subject and reason only — the note itself is in the vault.
func RenderEmailHold(day EmailDay, rev string, at time.Time) string {
	var b strings.Builder
	b.WriteString(transcriptStamp("email hold · "+day.Date+" (owner only)", rev, at))
	b.WriteString("Threads kept OUT of kairos's digest for this day. This file lives outside\n" +
		"his readable tree; the digest carries only the count below.\n\n")
	fmt.Fprintf(&b, "Held: %d\n\n", len(day.Held))
	for _, h := range day.Held {
		fmt.Fprintf(&b, "- %s — `%s.md` — %s\n", h.Thread.Subject, h.Thread.Name, h.Reason)
	}
	return b.String()
}

// EmailSubjectFromName strips the email note's date range and mailbox-local
// thread id suffix: "2026-08-29 - 2026-09-07 subject 1a04e57870972dba" → subject.
func EmailSubjectFromName(name, threadID string) string {
	_, _, title := SplitTranscriptName(name + ".md")
	if threadID != "" {
		title = strings.TrimSpace(strings.TrimSuffix(title, threadID))
	}
	return title
}

// ParseExtractorAction reads the extractor's proposal action line,
// "aion: <kind> — <title>", into an EmailAction.
func ParseExtractorAction(action string) (EmailAction, bool) {
	rest, ok := strings.CutPrefix(action, "aion: ")
	if !ok {
		return EmailAction{}, false
	}
	kind, title, ok := strings.Cut(rest, " — ")
	if !ok || strings.TrimSpace(title) == "" {
		return EmailAction{}, false
	}
	return EmailAction{Kind: strings.TrimSpace(kind), Title: strings.TrimSpace(title)}, true
}
