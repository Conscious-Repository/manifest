package recruiting

import (
	"fmt"
	"sort"
	"strconv"
	"strings"

	"manifest/record"
)

var (
	fitKeys      = []string{"criterion", "score", "evidence", "present"}
	evidenceKeys = []string{"id", "url", "file", "collected", "kind", "source"}
	pathKeys     = []string{"path", "kind", "confidence", "inferred"}
	nextKeys     = []string{"action", "due", "owner"}
	outreachKeys = []string{"log", "last", "status", "message", "thread"}
	overrideKeys = []string{"override", "override_reason", "override_at"}
)

// candidateRecognized decides which fields-only bullets are rows of a
// candidate record, per section. A profile row is recognized by carrying any
// profile key; a `## fit` row by [criterion::]; evidence by [id::]; and the
// recorded gate override rides the `## fit` section as an [override::] row.
// Every other line — an unknown `## notes` heading, a stray bullet, a
// tab-indented child — round-trips verbatim in place.
func candidateRecognized(heading string, r *Row) bool {
	switch strings.ToLower(strings.TrimSpace(heading)) {
	case "profile":
		for _, k := range ProfileKeys {
			if r.Has(k) {
				return true
			}
		}
		return false
	case "fit":
		return r.Has("criterion") || r.Has("override")
	case "evidence":
		return r.Has("id")
	case "network":
		return r.Has("path")
	case "outreach":
		return r.Has("log")
	case "next":
		return r.Has("action")
	}
	return false
}

// ParseCandidate reads candidates/<slug>.md.
func ParseCandidate(content string) *CandidateDoc {
	d := &CandidateDoc{}
	d.DocFM, d.Sections = parseSections(content, candidateRecognized)
	return d
}

// SerializeCandidate is the fixpoint emitter for a candidate record.
func SerializeCandidate(d *CandidateDoc) string { return serializeSections(d.DocFM, d.Sections) }

// Profile merges the `## profile` rows into the closed profile vocabulary.
// Keys outside ProfileKeys stay on the record and are not projected.
func (d *CandidateDoc) Profile() map[string]string {
	out := map[string]string{}
	for _, k := range ProfileKeys {
		out[k] = ""
	}
	for _, r := range rows(section(d.Sections, "profile")) {
		for _, f := range r.Fields {
			if inSetFold(ProfileKeys, f.Key) {
				out[strings.ToLower(f.Key)] = f.Value
			}
		}
	}
	return out
}

// SetProfile writes one profile field, in place when the record already
// carries the key — so the owner's own line layout survives an edit.
func (d *CandidateDoc) SetProfile(key, value string) error {
	key = strings.ToLower(strings.TrimSpace(key))
	if !inSetFold(ProfileKeys, key) {
		return errf("unknown profile field %q", key)
	}
	sec := ensureSection(&d.Sections, "profile")
	for _, r := range rows(sec) {
		if r.Has(key) {
			r.Set(key, value)
			return nil
		}
	}
	if rs := rows(sec); len(rs) > 0 {
		rs[len(rs)-1].Set(key, value)
		return nil
	}
	appendRow(sec, newRow(key, value))
	return nil
}

// Fit collects the `## fit` rows (the [override::] row is not one).
func (d *CandidateDoc) Fit() []FitEntry {
	out := []FitEntry{}
	for _, r := range rows(section(d.Sections, "fit")) {
		if !r.Has("criterion") {
			continue
		}
		score := strings.TrimSpace(r.Get("score"))
		if score == "" {
			score = ScoreUnknown
		}
		out = append(out, FitEntry{
			Criterion: r.Get("criterion"),
			Score:     score,
			Evidence:  r.GetAll("evidence"),
			Present:   boolField(r.Get("present")),
		})
	}
	return out
}

// Score sets one criterion's score and its backing evidence ids, creating the
// row when the criterion has not been judged yet. A numeric score with no
// evidence id is legal to WRITE — it parses, it renders `unscored`, and it
// fails the D6 gate. That keeps an Obsidian hand-edit honest instead of
// unrepresentable.
func (d *CandidateDoc) Score(criterion, score string, evidence []string, present bool) error {
	criterion = strings.TrimSpace(criterion)
	if criterion == "" {
		return errf("a fit row needs a criterion")
	}
	if err := ValidateScore(score); err != nil {
		return err
	}
	sec := ensureSection(&d.Sections, "fit")
	var row *Row
	for _, r := range rows(sec) {
		if strings.EqualFold(strings.TrimSpace(r.Get("criterion")), criterion) {
			row = r
			break
		}
	}
	if row == nil {
		row = newRow("criterion", criterion, "score", score)
		appendRow(sec, row)
	} else {
		row.Set("score", score)
	}
	row.SetAll("evidence", evidence)
	if present {
		row.Set("present", "true")
	} else {
		row.Drop("present")
	}
	return nil
}

// ValidateScore accepts the literal "unknown" or an integer 0–5.
func ValidateScore(score string) error {
	score = strings.TrimSpace(score)
	if score == ScoreUnknown {
		return nil
	}
	n, err := strconv.Atoi(score)
	if err != nil || n < 0 || n > 5 {
		return errf("score must be 0–5 or %q", ScoreUnknown)
	}
	return nil
}

// ScoreValue returns the numeric score and whether it is one (unknown is not).
func ScoreValue(score string) (int, bool) {
	n, err := strconv.Atoi(strings.TrimSpace(score))
	if err != nil {
		return 0, false
	}
	return n, true
}

// Evidence collects the `## evidence` rows, each with its verbatim snippet.
func (d *CandidateDoc) Evidence() []Evidence {
	out := []Evidence{}
	for _, r := range rows(section(d.Sections, "evidence")) {
		out = append(out, Evidence{
			ID:        r.Get("id"),
			URL:       r.Get("url"),
			File:      r.Get("file"),
			Collected: r.Get("collected"),
			Kind:      r.Get("kind"),
			Source:    r.Get("source"),
			Snippet:   snippetOf(r),
		})
	}
	return out
}

// snippetOf reads the blockquote lines beneath an evidence row back as text.
func snippetOf(r *Row) string {
	var out []string
	for _, ln := range r.Sub {
		t := strings.TrimLeft(ln, " \t")
		if !strings.HasPrefix(t, ">") {
			continue
		}
		out = append(out, strings.TrimSpace(strings.TrimPrefix(t, ">")))
	}
	return strings.Join(out, "\n")
}

// quoteLines renders a snippet as the indented blockquote the record uses.
func quoteLines(snippet string) []string {
	var out []string
	for _, ln := range strings.Split(strings.TrimRight(snippet, "\n"), "\n") {
		out = append(out, "  > "+ln)
	}
	return out
}

// AddEvidence appends one citation and returns its id. Ids are ev1, ev2, …
// per record: short enough to type into a `[evidence::]` field by hand, which
// is the whole point of them being on the record rather than in a database.
func (d *CandidateDoc) AddEvidence(ev Evidence) (string, error) {
	if strings.TrimSpace(ev.URL) == "" && strings.TrimSpace(ev.File) == "" &&
		strings.TrimSpace(ev.Snippet) == "" {
		return "", errf("evidence needs a url, a file, or a quote")
	}
	if strings.TrimSpace(ev.Kind) == "" {
		return "", errf("evidence needs a kind")
	}
	sec := ensureSection(&d.Sections, "evidence")
	taken := map[string]bool{}
	for _, r := range rows(sec) {
		taken[strings.TrimSpace(r.Get("id"))] = true
	}
	id := strings.TrimSpace(ev.ID)
	if id == "" {
		for n := 1; ; n++ {
			id = "ev" + strconv.Itoa(n)
			if !taken[id] {
				break
			}
		}
	} else if taken[id] {
		return "", errf("evidence %q already exists", id)
	}
	r := newRow("id", id)
	if ev.URL != "" {
		r.Set("url", ev.URL)
	}
	if ev.File != "" {
		r.Set("file", ev.File)
	}
	if ev.Collected != "" {
		r.Set("collected", ev.Collected)
	}
	r.Set("kind", ev.Kind)
	if ev.Source != "" {
		r.Set("source", ev.Source)
	}
	if s := strings.TrimSpace(ev.Snippet); s != "" {
		r.Sub = quoteLines(s)
	}
	appendRow(sec, r)
	return id, nil
}

// Paths collects the `## network` intro-path rows.
func (d *CandidateDoc) Paths() []PathClaim {
	out := []PathClaim{}
	for _, r := range rows(section(d.Sections, "network")) {
		out = append(out, PathClaim{
			Path:       r.Get("path"),
			Kind:       r.Get("kind"),
			Confidence: r.Get("confidence"),
			Inferred:   boolField(r.Get("inferred")),
		})
	}
	return out
}

// Outreach collects the `## outreach` pointers (never message bytes).
func (d *CandidateDoc) Outreach() []OutreachRef {
	out := []OutreachRef{}
	for _, r := range rows(section(d.Sections, "outreach")) {
		out = append(out, OutreachRef{Log: r.Get("log"), Last: r.Get("last"), Status: r.Get("status"),
			MessageID: r.Get("message"), ThreadID: r.Get("thread")})
	}
	return out
}

// SetOutreach upserts the pointer row for one log: the row is found by its
// [log::] and rewritten in place (the owner's field order survives), or
// appended. Message bytes never land here — only the ids Gmail answered.
func (d *CandidateDoc) SetOutreach(ref OutreachRef) {
	if strings.TrimSpace(ref.Log) == "" {
		return
	}
	sec := ensureSection(&d.Sections, "outreach")
	var row *Row
	for _, r := range rows(sec) {
		if strings.EqualFold(strings.TrimSpace(r.Get("log")), strings.TrimSpace(ref.Log)) {
			row = r
			break
		}
	}
	if row == nil {
		row = newRow("log", ref.Log, "last", ref.Last, "status", ref.Status)
		appendRow(sec, row)
	} else {
		row.Set("last", ref.Last)
		row.Set("status", ref.Status)
	}
	if ref.MessageID != "" {
		row.Set("message", ref.MessageID)
	}
	if ref.ThreadID != "" {
		row.Set("thread", ref.ThreadID)
	}
}

// Next collects the `## next` action rows.
func (d *CandidateDoc) Next() []NextAction {
	out := []NextAction{}
	for _, r := range rows(section(d.Sections, "next")) {
		out = append(out, NextAction{Action: r.Get("action"), Due: r.Get("due"), Owner: r.Get("owner")})
	}
	return out
}

// AddNext appends one next action.
func (d *CandidateDoc) AddNext(a NextAction) error {
	if strings.TrimSpace(a.Action) == "" {
		return errf("a next action needs text")
	}
	r := newRow("action", a.Action, "due", a.Due, "owner", a.Owner)
	appendRow(ensureSection(&d.Sections, "next"), r)
	return nil
}

// Override reads the recorded D6 gate override.
func (d *CandidateDoc) Override() Override {
	for _, r := range rows(section(d.Sections, "fit")) {
		if r.Has("override") {
			return Override{By: r.Get("override"), Reason: r.Get("override_reason"), At: r.Get("override_at")}
		}
	}
	return Override{}
}

// SetOverride records — or clears — the gate override on the record. An
// override is never silent: it is a row in the file with a reason and a date.
func (d *CandidateDoc) SetOverride(o Override) error {
	sec := ensureSection(&d.Sections, "fit")
	var found *Row
	for _, r := range rows(sec) {
		if r.Has("override") {
			found = r
			break
		}
	}
	if !o.Present() {
		if found == nil {
			return nil
		}
		var kept []Line
		for _, ln := range sec.Lines {
			if ln.Row == found {
				continue
			}
			kept = append(kept, ln)
		}
		sec.Lines = kept
		return nil
	}
	if strings.TrimSpace(o.Reason) == "" {
		return errf("an override needs a reason — it is written onto the record")
	}
	if found == nil {
		appendRow(sec, newRow("override", o.By, "override_reason", o.Reason, "override_at", o.At))
		return nil
	}
	found.Set("override", o.By)
	found.Set("override_reason", o.Reason)
	found.Set("override_at", o.At)
	return nil
}

// View is the candidate's read projection. The gate is computed, never
// stored (ARCHITECTURE §3) — the caller supplies the role it is judged
// against.
func (d *CandidateDoc) View(slug string, role *RoleDoc) Candidate {
	c := Candidate{
		Slug:               slug,
		ID:                 d.Get("id"),
		Name:               d.Get("name"),
		Role:               d.Get("role"),
		Stage:              d.Get("stage"),
		Owner:              d.Get("owner"),
		PII:                boolField(d.Get("pii")),
		AshbyCandidateID:   d.Get("ashby_candidate_id"),
		AshbyApplicationID: d.Get("ashby_application_id"),
		SourceRef:          d.Get("source_ref"),
		Created:            d.Get("created"),
		Archived:           d.Get("archived"),
		Profile:            d.Profile(),
		Fit:                d.Fit(),
		Evidence:           d.Evidence(),
		Paths:              d.Paths(),
		Outreach:           d.Outreach(),
		Next:               d.Next(),
		Override:           d.Override(),
	}
	if c.ID == "" {
		c.ID = CandidateID(slug)
	}
	if c.Stage == "" {
		c.Stage = StageNew
	}
	c.Gate = EvaluateGate(role, c)
	return c
}

// CandidateID is the record identity: `cand/<slug>`.
func CandidateID(slug string) string { return "cand/" + slug }

// CandidateSlug reduces an id or a bare slug to the slug half.
func CandidateSlug(id string) string {
	return strings.TrimPrefix(strings.TrimSpace(id), "cand/")
}

// NewCandidateSlug is the file identity for a new candidate: the kernel slug
// rule, deduplicated against the slugs already on disk.
func NewCandidateSlug(name string, taken map[string]bool) string {
	base := record.Slug(name, 60)
	if base == "" {
		base = "candidate"
	}
	slug := base
	for n := 2; taken[slug]; n++ {
		slug = fmt.Sprintf("%s-%d", base, n)
	}
	return slug
}

// sortCandidates orders the board deterministically: stage order first, then
// name — so two loads of the same vault paint identically.
func sortCandidates(cs []Candidate) {
	order := map[string]int{}
	for i, s := range Stages {
		order[s] = i
	}
	sort.SliceStable(cs, func(i, j int) bool {
		if a, b := order[cs[i].Stage], order[cs[j].Stage]; a != b {
			return a < b
		}
		return strings.ToLower(cs[i].Name) < strings.ToLower(cs[j].Name)
	})
}

func errf(format string, args ...any) error { return fmt.Errorf(format, args...) }
