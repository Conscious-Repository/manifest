package recruiting

import (
	"strconv"
	"strings"
)

// roleRowKeys are the recognized keys per section of a role record.
var (
	criterionKeys = []string{"criterion", "class", "weight"}
	sourcingKeys  = []string{"term"}
)

// roleRecognized decides which fields-only bullets are rows of a role record:
// `## criteria` rows carry [criterion::], `## sourcing` rows carry [term::].
// Anything else — including a hand-added `## notes` section — round-trips
// verbatim in place.
func roleRecognized(heading string, r *Row) bool {
	switch strings.ToLower(strings.TrimSpace(heading)) {
	case "criteria":
		return r.Has("criterion")
	case "sourcing":
		return r.Has("term")
	}
	return false
}

// ParseRole reads roles/<slug>.md.
func ParseRole(content string) *RoleDoc {
	d := &RoleDoc{}
	d.DocFM, d.Sections = parseSections(content, roleRecognized)
	return d
}

// SerializeRole is the fixpoint emitter for a role record.
func SerializeRole(d *RoleDoc) string { return serializeSections(d.DocFM, d.Sections) }

// Criteria collects the `## criteria` rows in order.
func (d *RoleDoc) Criteria() []Criterion {
	out := []Criterion{}
	for _, r := range rows(section(d.Sections, "criteria")) {
		c := Criterion{
			Criterion: r.Get("criterion"),
			Class:     strings.ToLower(strings.TrimSpace(r.Get("class"))),
			Unknown:   unknownFields(r, criterionKeys...),
		}
		if n, err := strconv.Atoi(strings.TrimSpace(r.Get("weight"))); err == nil {
			c.Weight = n
		}
		out = append(out, c)
	}
	return out
}

// SetCriteria replaces the `## criteria` rows wholesale. Every entry is
// validated first, so a bad class can never half-write the section.
func (d *RoleDoc) SetCriteria(crits []Criterion) error {
	for _, c := range crits {
		if strings.TrimSpace(c.Criterion) == "" {
			return errf("a criterion needs text")
		}
		if !ValidCriterionClass(c.Class) {
			return errf("criterion class must be one of %s", strings.Join(CriterionClasses, ", "))
		}
		if c.Weight < 0 || c.Weight > 5 {
			return errf("criterion weight must be 0–5")
		}
	}
	sec := ensureSection(&d.Sections, "criteria")
	var kept []Line
	for _, ln := range sec.Lines {
		if ln.Row == nil {
			kept = append(kept, ln)
		}
	}
	// criteria are app-authored, so fixed-order emission is fine here
	// (plan §4.5): rows first, then whatever verbatim lines the section held.
	var built []Line
	for _, c := range crits {
		r := newRow("criterion", c.Criterion, "class", c.Class)
		if c.Class != ClassDisqualifier && c.Weight > 0 {
			r.Set("weight", strconv.Itoa(c.Weight))
		}
		for _, f := range c.Unknown {
			r.Set(f.Key, f.Value)
		}
		built = append(built, Line{Row: r})
	}
	sec.Lines = append(built, kept...)
	return nil
}

// Terms collects the `## sourcing` search terms in order.
func (d *RoleDoc) Terms() []string {
	out := []string{}
	for _, r := range rows(section(d.Sections, "sourcing")) {
		out = append(out, r.GetAll("term")...)
	}
	return out
}

// Posting returns the verbatim `## posting` body. Phase 2's Ashby mirror
// replaces this section and nothing else; `## criteria` is Benjamin's
// translation of the posting and a re-sync must never erase it.
func (d *RoleDoc) Posting() string {
	sec := section(d.Sections, "posting")
	if sec == nil {
		return ""
	}
	var out []string
	for _, ln := range sec.Lines {
		if ln.Row != nil {
			out = append(out, ln.Row.EmitLines()...)
		} else {
			out = append(out, ln.Raw)
		}
	}
	return strings.TrimSpace(strings.Join(out, "\n"))
}

// View is the role's read projection. openCount is supplied by the caller so
// the rail badge and the board filter share ONE derivation (the aionOpenCount
// lesson: two derivations made a badge read 154 against 17 real items).
func (d *RoleDoc) View(slug string, openCount int) Role {
	return Role{
		Slug:           slug,
		ID:             d.Get("id"),
		Title:          d.Get("title"),
		Status:         d.Get("status"),
		Location:       d.Get("location"),
		Employment:     d.Get("employment"),
		HandoffMode:    d.Get("handoff_mode"),
		AshbyJobID:     d.Get("ashby_job_id"),
		AshbyPostingID: d.Get("ashby_posting_id"),
		AshbyProjectID: d.Get("ashby_project_id"),
		Pinned:         boolField(d.Get("pinned")),
		Source:         d.Get("source"),
		Synced:         d.Get("synced"),
		Criteria:       d.Criteria(),
		Terms:          d.Terms(),
		Posting:        d.Posting(),
		OpenCount:      openCount,
	}
}
