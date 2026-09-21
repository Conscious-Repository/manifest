package aion

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"regexp"
	"sort"
	"strings"
	"time"

	"manifest/vaultindex"
)

// The transcript corpus for kairos (aion-context transcripts, 2026-09-21):
// the aion-category notes under the vault's log/, gated by the tier map and
// projected as flat markdown into the standing context pack —
//
//	transcripts/<original filename>   the note, verbatim (open + internal only)
//	transcripts/INDEX.md              date · title · tier · people, one row per note
//	digests/{fundraising,hiring,research,strategy}.md  rollups over eligible notes
//	digests/email-<date>.md           the daily mail digest (emaildigest.go)
//	digests/README.md                 coverage + the revision stamp, written LAST
//
// Rules, mirroring aion_pack.go:
//   - the tier gate is DATA (tiers.go); a note the map does not name is
//     excluded and counted, never guessed at.
//   - held notes are never written, never indexed, never referenced: not by
//     filename, not by title, not by quote. Digests are rollups over ELIGIBLE
//     notes; the only trace of held material is a count.
//   - pure: no I/O, no clock. Same corpus → byte-identical files, so a diff
//     means the vault moved.

// RawNote is one file under the transcript directory as read from disk.
type RawNote struct {
	Name string // basename, with .md
	Body []byte
}

// TranscriptNote is one ELIGIBLE (open or internal) note, parsed for the index.
type TranscriptNote struct {
	Name       string
	Title      string // the name without its date prefix and .md
	Date       string // YYYY-MM-DD from the filename ("" when undated)
	EndDate    string // the second date of a "YYYY-MM-DD - YYYY-MM-DD title" name
	Tier       Tier
	Categories []string
	People     []string // person entities the note links (display names)
	Body       []byte   // verbatim
	Hash       string   // sha256 hex of Body — the portal FileRef address
}

// TranscriptCorpus is the tier-gated read of the transcript directory.
type TranscriptCorpus struct {
	Notes    []TranscriptNote // eligible only, sorted by name
	Held     int              // held notes present on disk
	Unmapped int              // aion-category notes on disk the map does not name
	// UnmappedNames is for the server LOG only — it is never rendered into
	// the pack, because an untiered name may be a held conversation.
	UnmappedNames []string
	Counts        TierCounts // the map's census, independent of what is on disk
}

// TranscriptCategory is the frontmatter category that puts a log/ note in the
// AION domain at all; notes without it are outside this corpus.
const TranscriptCategory = "aion"

// BuildTranscriptCorpus applies the tier gate to raw notes. isPerson resolves
// a wikilink key to a person's display name (nil → every link counts as a
// name, uncapitalized). Notes without the aion category are ignored outright.
func BuildTranscriptCorpus(tm TierMap, raws []RawNote, isPerson func(key string) (string, bool)) TranscriptCorpus {
	c := TranscriptCorpus{Counts: tm.Counts()}
	sorted := append([]RawNote(nil), raws...)
	sort.Slice(sorted, func(i, j int) bool { return sorted[i].Name < sorted[j].Name })
	for _, r := range sorted {
		if !strings.HasSuffix(r.Name, ".md") {
			continue
		}
		parsed := vaultindex.ParseNote(r.Name, r.Body, 0, nil)
		if !hasCategory(parsed.Categories, TranscriptCategory) {
			continue
		}
		tier, mapped := tm.Tier(r.Name)
		switch {
		case !mapped:
			c.Unmapped++
			c.UnmappedNames = append(c.UnmappedNames, r.Name)
			continue
		case tier == TierHeld:
			c.Held++
			continue
		}
		sum := sha256.Sum256(r.Body)
		n := TranscriptNote{
			Name: r.Name, Tier: tier, Categories: parsed.Categories,
			Body: r.Body, Hash: hex.EncodeToString(sum[:]),
		}
		n.Date, n.EndDate, n.Title = SplitTranscriptName(r.Name)
		seen := map[string]bool{}
		for _, l := range parsed.Links {
			key := strings.ToLower(strings.TrimSpace(l.Key))
			if key == "" || seen[key] {
				continue
			}
			display := key
			if isPerson != nil {
				d, ok := isPerson(key)
				if !ok {
					continue
				}
				display = d
			}
			seen[key] = true
			n.People = append(n.People, display)
		}
		c.Notes = append(c.Notes, n)
	}
	return c
}

func hasCategory(cats []string, want string) bool {
	for _, c := range cats {
		if strings.EqualFold(strings.TrimSpace(c), want) {
			return true
		}
	}
	return false
}

var transcriptNameRe = regexp.MustCompile(`^(\d{4}-\d{2}-\d{2})(?: - (\d{4}-\d{2}-\d{2}))? (.+)\.md$`)

// SplitTranscriptName reads "YYYY-MM-DD[ - YYYY-MM-DD] title.md".
func SplitTranscriptName(name string) (date, end, title string) {
	if m := transcriptNameRe.FindStringSubmatch(name); m != nil {
		return m[1], m[2], strings.TrimSpace(m[3])
	}
	return "", "", strings.TrimSuffix(name, ".md")
}

// Span is the corpus's date range (first, last) over eligible notes.
func (c TranscriptCorpus) Span() (string, string) {
	first, last := "", ""
	for _, n := range c.Notes {
		if n.Date == "" {
			continue
		}
		if first == "" || n.Date < first {
			first = n.Date
		}
		hi := n.Date
		if n.EndDate > hi {
			hi = n.EndDate
		}
		if hi > last {
			last = hi
		}
	}
	return first, last
}

// TranscriptArtifact is one open note as the portal publishes it: the
// content-addressed FileRef (sha256 of the verbatim body, matching the
// files/ tree) plus the date and title the ARTIFACTS list shows.
type TranscriptArtifact struct {
	Hash  string `json:"hash"`
	Name  string `json:"name"`
	Title string `json:"title"`
	Date  string `json:"date"`
	Size  int64  `json:"size"`
	Mime  string `json:"mime,omitempty"`
	Tier  Tier   `json:"tier"`
}

// PortalArtifacts is the corpus's OPEN subset, in the FileRef shape. Internal
// notes never leave here: the gate is the tier, checked per note again.
func (c TranscriptCorpus) PortalArtifacts() []TranscriptArtifact {
	var out []TranscriptArtifact
	for _, n := range c.Notes {
		if n.Tier != TierOpen {
			continue
		}
		out = append(out, TranscriptArtifact{
			Hash: n.Hash, Name: n.Name, Title: n.Title, Date: n.Date,
			Size: int64(len(n.Body)), Mime: "text/markdown", Tier: n.Tier,
		})
	}
	return out
}

// ---- rendering ---------------------------------------------------------------

// PackFile is one rendered file, path relative to the pack dir. The renderer
// returns them in WRITE ORDER: the stamp file (digests/README.md) is last.
type PackFile struct {
	Path string
	Body string
}

// TranscriptStampFile carries the corpus revision; a writer lands it last so a
// torn write never claims a revision it did not finish.
const TranscriptStampFile = "digests/README.md"

// TranscriptPackInput is everything the renderer needs — no clock, no I/O.
type TranscriptPackInput struct {
	Revision string
	At       time.Time // the snapshot's own time, never the wall clock
	Corpus   TranscriptCorpus
	Email    []EmailDay // the projected mail days (emaildigest.go)
}

func transcriptStamp(title, rev string, at time.Time) string {
	return fmt.Sprintf("# AION — %s\n\n> revision: %s · generated: %s · source: manifest AionLive (transcripts)\n\n",
		title, rev, at.UTC().Format(time.RFC3339))
}

// coverageLine is the sentence every digest carries so kairos never believes
// he has seen everything: what was counted, and what was excluded.
func coverageLine(counted int, first, last string, c TranscriptCorpus) string {
	span := "no dated notes"
	if first != "" {
		span = first + " → " + last
	}
	line := fmt.Sprintf("Coverage: %d notes counted · span %s · held notes excluded: %d",
		counted, span, c.Held)
	if c.Unmapped > 0 {
		line += fmt.Sprintf(" · unmapped notes excluded: %d (awaiting a tier)", c.Unmapped)
	}
	return line + "\n"
}

// RenderTranscriptPack composes every file of the transcripts + digests
// sub-pack, README last. Pure.
func RenderTranscriptPack(in TranscriptPackInput) []PackFile {
	c := in.Corpus
	var out []PackFile

	// ---- transcripts/<name>: verbatim, so the bytes hash to the portal address
	for _, n := range c.Notes {
		out = append(out, PackFile{Path: "transcripts/" + n.Name, Body: string(n.Body)})
	}

	// ---- transcripts/INDEX.md: newest first
	{
		var b strings.Builder
		b.WriteString(transcriptStamp("transcripts", in.Revision, in.At))
		b.WriteString("The AION transcript corpus kairos may read: `open` (shareable with the\n" +
			"team) and `internal` (company-internal) notes, one file per note, the\n" +
			"vault's own text unedited. `held` notes (compensation, immigration,\n" +
			"negotiation, personal, legal) are NOT here — not listed, not quoted, not\n" +
			"summarized anywhere in this pack; only their count is. Pull a single\n" +
			"note only when a task needs it; the digests/ rollups cover the rest.\n\n")
		first, last := c.Span()
		open, internal := 0, 0
		for _, n := range c.Notes {
			if n.Tier == TierOpen {
				open++
			} else {
				internal++
			}
		}
		fmt.Fprintf(&b, "Coverage: %d notes (%d open · %d internal) · span %s · held notes excluded: %d",
			len(c.Notes), open, internal, spanText(first, last), c.Held)
		if c.Unmapped > 0 {
			fmt.Fprintf(&b, " · unmapped notes excluded: %d (awaiting a tier)", c.Unmapped)
		}
		b.WriteString("\n\n| date | title | tier | people | file |\n|---|---|---|---|---|\n")
		notes := append([]TranscriptNote(nil), c.Notes...)
		sort.SliceStable(notes, func(i, j int) bool { return notes[i].Name > notes[j].Name })
		for _, n := range notes {
			date := orText(n.Date, "undated")
			if n.EndDate != "" {
				date += " → " + n.EndDate
			}
			fmt.Fprintf(&b, "| %s | %s | %s | %s | `%s` |\n",
				date, cell(n.Title), n.Tier, cell(strings.Join(n.People, ", ")), n.Name)
		}
		out = append(out, PackFile{Path: "transcripts/INDEX.md", Body: b.String()})
	}

	// ---- digests/<topic>.md
	for _, t := range digestTopics {
		out = append(out, PackFile{Path: "digests/" + t.File, Body: renderTopicDigest(t, c, in.Revision, in.At)})
	}

	// ---- digests/email-<date>.md
	for _, day := range in.Email {
		out = append(out, PackFile{Path: "digests/email-" + day.Date + ".md", Body: RenderEmailDigest(day, in.Revision, in.At)})
	}

	// ---- digests/README.md — the stamp, LAST
	{
		var b strings.Builder
		b.WriteString(transcriptStamp("digests", in.Revision, in.At))
		b.WriteString("Precomputed rollups over the transcript corpus in ../transcripts/,\n" +
			"regenerated whenever the corpus changes. Every digest states its own\n" +
			"coverage — notes counted, date span, and how many held notes were\n" +
			"excluded — because a rollup here is never the whole record.\n\n")
		first, last := c.Span()
		b.WriteString(coverageLine(len(c.Notes), first, last, c))
		fmt.Fprintf(&b, "Tier map: %d open · %d internal · %d held (data: manifest aion/tier-map.json).\n\n",
			c.Counts.Open, c.Counts.Internal, c.Counts.Held)
		for _, t := range digestTopics {
			fmt.Fprintf(&b, "- %s — %s\n", t.File, t.Blurb)
		}
		if len(in.Email) > 0 {
			fmt.Fprintf(&b, "- email-<date>.md — the daily mail digest for ben@aion.bio (%d days; topics, threads, senders, decisions, action items — never message bodies)\n", len(in.Email))
		} else {
			b.WriteString("- email-<date>.md — the daily mail digest (none projected yet)\n")
		}
		out = append(out, PackFile{Path: TranscriptStampFile, Body: b.String()})
	}
	return out
}

func spanText(first, last string) string {
	if first == "" {
		return "no dated notes"
	}
	return first + " → " + last
}

func orText(v, fallback string) string {
	if strings.TrimSpace(v) == "" {
		return fallback
	}
	return v
}

// cell makes a value safe inside a markdown table row.
func cell(s string) string {
	s = strings.ReplaceAll(s, "|", "\\|")
	return strings.Join(strings.Fields(s), " ")
}

// ---- topic digests -------------------------------------------------------------

// digestTopic selects eligible notes by frontmatter category or a title
// keyword. The rules are a table on purpose: a rollup's membership must be
// readable, and it is NEVER a body-content guess.
type digestTopic struct {
	File       string
	Title      string
	Blurb      string
	Categories []string
	Keywords   []string // matched against the lowercased title
}

var digestTopics = []digestTopic{
	{File: "fundraising.md", Title: "fundraising", Blurb: "investor conversations, rounds, grants and tax credits",
		Categories: []string{"fundraising", "investor", "investors", "budget", "grant"},
		Keywords:   []string{"investor", "fundrais", "pitch", "seed", "round", "grant", "tax credit", "capital"}},
	{File: "hiring.md", Title: "hiring", Blurb: "interviews, candidates and roles",
		Categories: []string{"interview", "hiring", "recruiting", "candidate", "candidates"},
		Keywords:   []string{"interview", "candidate", "hiring", "recruit", "role"}},
	{File: "research.md", Title: "research", Blurb: "science, ultrasound/MRI, regulatory and technical memos",
		Categories: []string{"research", "biotech", "ultrasound", "science", "regulatory", "ai", "mri"},
		Keywords:   []string{"memo", "ultrasound", "mri", "research", "protocol", "experiment", "fda", "regulatory", "cell", "imaging"}},
	{File: "strategy.md", Title: "strategy", Blurb: "direction, plans, board and company-level discussions",
		Categories: []string{"strategy", "discussion", "startup", "community", "projects"},
		Keywords:   []string{"strategy", "roadmap", "plan", "board", "vision", "offsite", "quarterly", "butterfly"}},
}

func (t digestTopic) matches(n TranscriptNote) bool {
	for _, c := range n.Categories {
		for _, want := range t.Categories {
			if strings.EqualFold(strings.TrimSpace(c), want) {
				return true
			}
		}
	}
	title := strings.ToLower(n.Title)
	for _, k := range t.Keywords {
		if strings.Contains(title, k) {
			return true
		}
	}
	return false
}

func renderTopicDigest(t digestTopic, c TranscriptCorpus, rev string, at time.Time) string {
	var picked []TranscriptNote
	for _, n := range c.Notes {
		if t.matches(n) {
			picked = append(picked, n)
		}
	}
	sort.SliceStable(picked, func(i, j int) bool { return picked[i].Name > picked[j].Name })

	var b strings.Builder
	b.WriteString(transcriptStamp("digest · "+t.Title, rev, at))
	fmt.Fprintf(&b, "Rollup of the %s transcripts kairos may read (%s). Membership is by\n"+
		"frontmatter category (%s) or a title keyword. Each entry is the note's\n"+
		"own lead paragraph; open the file under ../transcripts/ for the full text.\n\n",
		t.Title, t.Blurb, strings.Join(t.Categories, ", "))
	first, last := "", ""
	byMonth := map[string]int{}
	for _, n := range picked {
		if n.Date == "" {
			continue
		}
		if first == "" || n.Date < first {
			first = n.Date
		}
		if n.Date > last {
			last = n.Date
		}
		byMonth[n.Date[:7]]++
	}
	b.WriteString(coverageLine(len(picked), first, last, c))
	b.WriteString("Held notes are not classified by topic — the count above is the whole held set.\n\n")
	if len(byMonth) > 0 {
		months := make([]string, 0, len(byMonth))
		for m := range byMonth {
			months = append(months, m)
		}
		sort.Sort(sort.Reverse(sort.StringSlice(months)))
		b.WriteString("## By month\n")
		for _, m := range months {
			fmt.Fprintf(&b, "- %s: %d\n", m, byMonth[m])
		}
		b.WriteString("\n")
	}
	b.WriteString("## Notes\n")
	if len(picked) == 0 {
		b.WriteString("- none in the eligible corpus\n")
	}
	for _, n := range picked {
		fmt.Fprintf(&b, "- **%s** — %s (%s)", orText(n.Date, "undated"), n.Title, n.Tier)
		if len(n.People) > 0 {
			fmt.Fprintf(&b, " · people: %s", strings.Join(n.People, ", "))
		}
		if cats := categoriesExcept(n.Categories, TranscriptCategory); len(cats) > 0 {
			fmt.Fprintf(&b, " · categories: %s", strings.Join(cats, ", "))
		}
		fmt.Fprintf(&b, " · `transcripts/%s`\n", n.Name)
		if lead := LeadParagraph(n.Body, 240); lead != "" {
			fmt.Fprintf(&b, "  > %s\n", lead)
		}
	}
	return b.String()
}

func categoriesExcept(cats []string, drop string) []string {
	var out []string
	for _, c := range cats {
		if !strings.EqualFold(c, drop) {
			out = append(out, c)
		}
	}
	return out
}

var wikilinkOnlyRe = regexp.MustCompile(`^(\s*\[\[[^\]]*\]\]\s*)+$`)

// LeadParagraph is the first prose paragraph of a note body: not the
// frontmatter, not a heading, not a line of only wikilinks (the attendee row),
// not a table or a fence. Trimmed to max runes with an ellipsis.
func LeadParagraph(body []byte, max int) string {
	_, text := splitFM(string(body))
	inFence := false
	for _, line := range strings.Split(text, "\n") {
		l := strings.TrimSpace(line)
		if strings.HasPrefix(l, "```") {
			inFence = !inFence
			continue
		}
		if inFence || l == "" || strings.HasPrefix(l, "#") || strings.HasPrefix(l, "|") ||
			strings.HasPrefix(l, "<") || wikilinkOnlyRe.MatchString(l) {
			continue
		}
		l = strings.TrimLeft(l, "-*> ")
		l = strings.Join(strings.Fields(l), " ")
		if l == "" {
			continue
		}
		if r := []rune(l); len(r) > max {
			return strings.TrimSpace(string(r[:max])) + "…"
		}
		return l
	}
	return ""
}

// splitFM drops a leading --- frontmatter block.
func splitFM(s string) (string, string) {
	if !strings.HasPrefix(s, "---\n") {
		return "", s
	}
	rest := s[4:]
	i := strings.Index(rest, "\n---")
	if i < 0 {
		return "", s
	}
	fm := rest[:i]
	body := rest[i+4:]
	if j := strings.Index(body, "\n"); j >= 0 {
		body = body[j+1:]
	} else {
		body = ""
	}
	return fm, body
}
