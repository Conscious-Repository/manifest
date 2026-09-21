package recruiting

import (
	"strings"

	"manifest/record"
)

var seedKeys = []string{"id", "class", "name", "org", "url", "added", "source", "consent", "cadence", "label"}

// SeedCadences is the closed set for a place's `cadence`: how often it is
// due for another sweep. Empty is "when you say". The cadence marks a row
// DUE; it never fires a sweep on its own — a sweep spends somebody else's
// rate limit, and the last gesture stays the owner's (the same rule as the
// "load, never run" pending card).
var SeedCadences = []string{"", "weekly", "monthly", "quarterly"}

func ValidSeedCadence(s string) bool { return inSet(SeedCadences, s) }

// seedRecognized: a seeds.md row is a fields-only bullet carrying [id::].
func seedRecognized(r *Row) bool { return r.Has("id") }

// ParseSeeds reads seeds.md — the four D11 seed classes in one file, because
// they are read together and are small (20–50 entries, not a corpus).
func ParseSeeds(content string) *SeedsDoc {
	d := &SeedsDoc{}
	d.DocFM, d.Lines = parseRows(content, seedRecognized)
	return d
}

// SerializeSeeds is the fixpoint emitter for seeds.md.
func SerializeSeeds(d *SeedsDoc) string { return serializeRows(d.DocFM, d.Lines) }

// Seeds collects the rows in order.
func (d *SeedsDoc) Seeds() []Seed {
	out := []Seed{}
	for _, ln := range d.Lines {
		if ln.Row == nil {
			continue
		}
		out = append(out, seedOf(ln.Row))
	}
	return out
}

// seedOf is THE projection of one row — read by Seeds() and by the editor in
// mutate.go, so a field added here reaches both without a second copy.
func seedOf(r *Row) Seed {
	s := Seed{
		ID:      r.Get("id"),
		Class:   strings.ToLower(strings.TrimSpace(r.Get("class"))),
		Name:    r.Get("name"),
		Org:     r.Get("org"),
		URL:     r.Get("url"),
		Added:   r.Get("added"),
		Source:  r.Get("source"),
		Consent: r.Get("consent"),
		Cadence: strings.ToLower(strings.TrimSpace(r.Get("cadence"))),
		Label:   strings.TrimSpace(r.Get("label")),
		Unknown: unknownFields(r, seedKeys...),
	}
	s.Display = LabLabel(s)
	return s
}

// Add appends one seed, refusing a duplicate id and anything outside the
// closed class set. Returns the stored seed.
func (d *SeedsDoc) Add(s Seed) (Seed, error) {
	if !ValidSeedClass(s.Class) {
		return Seed{}, errf("seed class must be one of %s", strings.Join(SeedClasses, ", "))
	}
	if strings.TrimSpace(s.Name) == "" {
		return Seed{}, errf("a seed needs a name")
	}
	if s.ID == "" {
		s.ID = NewSeedID(s.Class, s.Name, d.takenIDs())
	}
	if d.takenIDs()[s.ID] {
		return Seed{}, errf("seed %q already exists", s.ID)
	}
	r := newRow("id", s.ID, "class", s.Class, "name", s.Name)
	for _, kv := range [][2]string{{"org", s.Org}, {"url", s.URL}, {"added", s.Added},
		{"source", s.Source}, {"consent", s.Consent}, {"cadence", s.Cadence}, {"label", s.Label}} {
		if kv[1] != "" {
			r.Set(kv[0], kv[1])
		}
	}
	for _, f := range s.Unknown {
		r.Set(f.Key, f.Value)
	}
	d.Lines = append(d.Lines, Line{Row: r})
	return s, nil
}

func (d *SeedsDoc) takenIDs() map[string]bool {
	taken := map[string]bool{}
	for _, ln := range d.Lines {
		if ln.Row != nil {
			taken[strings.TrimSpace(ln.Row.Get("id"))] = true
		}
	}
	return taken
}

// seedIDPrefix maps a class onto the id namespace the plan's examples use:
// `seed/lab-…`, `seed/co-hyperfine`, `seed/person-…`, `seed/work-…`,
// `seed/repo-…`.
func seedIDPrefix(class string) string {
	if class == SeedCompany {
		return "co"
	}
	return class
}

// NewSeedID derives a stable, readable seed id, deduplicated against what is
// already in the file.
func NewSeedID(class, name string, taken map[string]bool) string {
	base := "seed/" + seedIDPrefix(class) + "-" + record.Slug(name, 48)
	base = strings.TrimSuffix(base, "-")
	id := base
	for n := 2; taken[id]; n++ {
		id = base + "-" + itoa(n)
	}
	return id
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	var b []byte
	for ; n > 0; n /= 10 {
		b = append([]byte{byte('0' + n%10)}, b...)
	}
	return string(b)
}
