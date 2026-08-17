package fundraising

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	"manifest/mdfm"
	"manifest/record"
)

const registrySeed = `# CRM contacts

Explicit contact identities used by Manifest CRM records. A personal note is optional.
`

// Store reads private fundraising records and the shared CRM contact registry.
// Both writers are injected vaultwriter capabilities; this package never writes
// the vault directly.
type Store struct {
	vaultRoot   string
	root        string
	registryRel string
	writeRecord func(string, []byte) error
	writePeople func(string, []byte) error
}

func NewStore(vaultRoot, root, registryRel string, writeRecord, writePeople func(string, []byte) error) *Store {
	return &Store{vaultRoot: vaultRoot, root: filepath.ToSlash(root), registryRel: filepath.ToSlash(registryRel), writeRecord: writeRecord, writePeople: writePeople}
}

func (s *Store) Root() string          { return s.root }
func (s *Store) RegistryRel() string   { return s.registryRel }
func (s *Store) abs(rel string) string { return filepath.Join(s.vaultRoot, filepath.FromSlash(rel)) }

func (s *Store) Ensure() error {
	if _, err := os.Stat(s.abs(s.registryRel)); errors.Is(err, os.ErrNotExist) {
		if s.writePeople == nil {
			return errors.New("fundraising: CRM contacts writer unavailable")
		}
		return s.writePeople(s.abs(s.registryRel), []byte(registrySeed))
	}
	return nil
}

func validStatus(v string) bool {
	for _, x := range Statuses {
		if v == x {
			return true
		}
	}
	return false
}
func validInterest(v string) bool {
	for _, x := range Interests {
		if v == x {
			return true
		}
	}
	return false
}

func (s *Store) List() ([]Opportunity, error) {
	dir := s.abs(s.root)
	ents, err := os.ReadDir(dir)
	if errors.Is(err, os.ErrNotExist) {
		return []Opportunity{}, nil
	}
	if err != nil {
		return nil, err
	}
	out := make([]Opportunity, 0, len(ents))
	for _, ent := range ents {
		if ent.IsDir() || !strings.HasSuffix(strings.ToLower(ent.Name()), ".md") || ent.Name() == "resources.md" {
			continue
		}
		rel := s.root + "/" + ent.Name()
		if op, ok := s.loadRel(rel); ok {
			out = append(out, op)
		}
	}
	sort.SliceStable(out, func(i, j int) bool {
		if out[i].Archived != out[j].Archived {
			return !out[i].Archived
		}
		if rankStatus(out[i].Status) != rankStatus(out[j].Status) {
			return rankStatus(out[i].Status) < rankStatus(out[j].Status)
		}
		if out[i].NextStepDue != out[j].NextStepDue {
			if out[i].NextStepDue == "" {
				return false
			}
			if out[j].NextStepDue == "" {
				return true
			}
			return out[i].NextStepDue < out[j].NextStepDue
		}
		return strings.ToLower(out[i].Firm) < strings.ToLower(out[j].Firm)
	})
	return out, nil
}

func rankStatus(s string) int {
	switch s {
	case StatusActive:
		return 0
	case StatusProspect:
		return 1
	case StatusCommitted:
		return 2
	case StatusPassed:
		return 3
	}
	return 4
}

func (s *Store) Get(id string) (Opportunity, bool) {
	ops, _ := s.List()
	for _, op := range ops {
		if op.ID == id {
			return op, true
		}
	}
	return Opportunity{}, false
}

func (s *Store) loadRel(rel string) (Opportunity, bool) {
	b, err := os.ReadFile(s.abs(rel))
	if err != nil {
		return Opportunity{}, false
	}
	fm, _ := mdfm.Split(string(b))
	if !containsFold(mdfm.List(fm["categories"]), "fundraising") {
		return Opportunity{}, false
	}
	op := Opportunity{Path: rel, ID: scalar(fm["id"]), Firm: scalar(fm["firm"]), Status: strings.ToLower(scalar(fm["status"])), Interest: strings.ToLower(scalar(fm["interest"])), Currency: scalar(fm["currency"]), IntroVia: scalar(fm["intro-via"]), LastTouchpoint: scalar(fm["last-touchpoint"]), LastTouchpointDate: scalar(fm["last-touchpoint-date"]), NextStep: scalar(fm["next-step"]), NextStepDue: scalar(fm["next-step-due"]), Notes: scalar(fm["notes"]), Archived: parseBool(fm["archived"]), ImportReview: parseBool(fm["import-review"])}
	if op.ID == "" {
		op.ID = "fr/" + strings.TrimSuffix(filepath.Base(rel), ".md")
	}
	if op.Firm == "" {
		op.Firm = strings.TrimSuffix(filepath.Base(rel), ".md")
	}
	if !validStatus(op.Status) {
		op.Status = StatusProspect
	}
	if !validInterest(op.Interest) {
		op.Interest = InterestUnknown
	}
	if op.Currency == "" {
		op.Currency = "USD"
	}
	op.Amount, _ = strconv.ParseFloat(strings.TrimSpace(fm["amount"]), 64)
	_ = json.Unmarshal([]byte(fm["people"]), &op.People)
	_ = json.Unmarshal([]byte(fm["source"]), &op.Source)
	_ = json.Unmarshal([]byte(fm["source-rows"]), &op.SourceRows)
	if op.People == nil {
		op.People = []PersonRef{}
	}
	return op, true
}

func containsFold(xs []string, want string) bool {
	for _, x := range xs {
		if strings.EqualFold(strings.TrimSpace(x), want) {
			return true
		}
	}
	return false
}
func parseBool(v string) bool { b, _ := strconv.ParseBool(strings.TrimSpace(v)); return b }
func scalar(v string) string {
	v = strings.TrimSpace(v)
	if v == "" {
		return ""
	}
	if u, err := strconv.Unquote(v); err == nil {
		return u
	}
	return v
}
func q(v string) string { return strconv.Quote(strings.TrimSpace(v)) }

func (s *Store) Create(firm string) (Opportunity, error) {
	firm = strings.TrimSpace(firm)
	if firm == "" {
		return Opportunity{}, errors.New("firm is required")
	}
	slug := record.Slug(firm, 72)
	if slug == "" {
		slug = "opportunity"
	}
	base := slug
	for n := 2; ; n++ {
		if _, err := os.Stat(s.abs(s.root + "/" + slug + ".md")); errors.Is(err, os.ErrNotExist) {
			break
		}
		slug = fmt.Sprintf("%s-%d", base, n)
	}
	op := Opportunity{ID: "fr/" + slug, Path: s.root + "/" + slug + ".md", Firm: firm, Status: StatusProspect, Interest: InterestUnknown, Currency: "USD", People: []PersonRef{}}
	if err := s.writeNew(op); err != nil {
		return Opportunity{}, err
	}
	return op, nil
}

func (s *Store) writeNew(op Opportunity) error {
	if s.writeRecord == nil {
		return errors.New("fundraising: record writer unavailable")
	}
	people, _ := json.Marshal(op.People)
	rows, _ := json.Marshal(op.SourceRows)
	var b strings.Builder
	b.WriteString("---\n")
	b.WriteString("categories: [fundraising]\n")
	b.WriteString("id: " + q(op.ID) + "\nfirm: " + q(op.Firm) + "\n")
	b.WriteString("status: " + op.Status + "\ninterest: " + op.Interest + "\n")
	if op.Amount > 0 {
		b.WriteString("amount: " + strconv.FormatFloat(op.Amount, 'f', -1, 64) + "\n")
	}
	b.WriteString("currency: " + op.Currency + "\npeople: " + string(people) + "\n")
	if op.Source != nil {
		source, _ := json.Marshal(op.Source)
		b.WriteString("source: " + string(source) + "\n")
	}
	b.WriteString("intro-via: " + q(op.IntroVia) + "\nlast-touchpoint: " + q(op.LastTouchpoint) + "\nlast-touchpoint-date: " + q(op.LastTouchpointDate) + "\n")
	b.WriteString("next-step: " + q(op.NextStep) + "\nnext-step-due: " + q(op.NextStepDue) + "\nnotes: " + q(op.Notes) + "\n")
	b.WriteString("archived: " + strconv.FormatBool(op.Archived) + "\nsource-rows: " + string(rows) + "\nimport-review: " + strconv.FormatBool(op.ImportReview) + "\n---\n\n# " + op.Firm + "\n")
	return s.writeRecord(s.abs(op.Path), []byte(b.String()))
}

// ImportUpsert is the idempotent migration entry point. Existing source rows
// win over firm-name matching, so re-running the one-time import cannot create
// duplicates.
func (s *Store) ImportUpsert(op Opportunity) (Opportunity, error) {
	ops, err := s.List()
	if err != nil {
		return Opportunity{}, err
	}
	for _, have := range ops {
		if overlap(have.SourceRows, op.SourceRows) {
			op.Path, op.ID = have.Path, have.ID
			return op, s.replaceKnown(op)
		}
	}
	if op.ID == "" {
		op.ID = "fr/" + record.Slug(op.Firm, 72)
	}
	if op.Path == "" {
		op.Path = s.root + "/" + strings.TrimPrefix(op.ID, "fr/") + ".md"
	}
	if _, err := os.Stat(s.abs(op.Path)); err == nil {
		base := strings.TrimSuffix(op.Path, ".md")
		for n := 2; ; n++ {
			candidate := fmt.Sprintf("%s-%d.md", base, n)
			if _, e := os.Stat(s.abs(candidate)); errors.Is(e, os.ErrNotExist) {
				op.Path = candidate
				op.ID = "fr/" + strings.TrimSuffix(filepath.Base(candidate), ".md")
				break
			}
		}
	}
	return op, s.writeNew(op)
}
func overlap(a, b []int) bool {
	m := map[int]bool{}
	for _, x := range a {
		m[x] = true
	}
	for _, x := range b {
		if m[x] {
			return true
		}
	}
	return false
}

func (s *Store) replaceKnown(op Opportunity) error {
	b, err := os.ReadFile(s.abs(op.Path))
	if err != nil {
		return err
	}
	people, _ := json.Marshal(op.People)
	rows, _ := json.Marshal(op.SourceRows)
	vals := map[string]*string{}
	put := func(k, v string) { vv := v; vals[k] = &vv }
	put("id", q(op.ID))
	put("firm", q(op.Firm))
	put("status", op.Status)
	put("interest", op.Interest)
	put("currency", op.Currency)
	put("people", string(people))
	if op.Source != nil {
		source, _ := json.Marshal(op.Source)
		put("source", string(source))
	} else {
		vals["source"] = nil
	}
	put("intro-via", q(op.IntroVia))
	put("last-touchpoint", q(op.LastTouchpoint))
	put("last-touchpoint-date", q(op.LastTouchpointDate))
	put("next-step", q(op.NextStep))
	put("next-step-due", q(op.NextStepDue))
	put("notes", q(op.Notes))
	put("archived", strconv.FormatBool(op.Archived))
	put("source-rows", string(rows))
	put("import-review", strconv.FormatBool(op.ImportReview))
	if op.Amount > 0 {
		put("amount", strconv.FormatFloat(op.Amount, 'f', -1, 64))
	} else {
		vals["amount"] = nil
	}
	return s.writeRecord(s.abs(op.Path), patchFrontmatter(b, vals))
}

func (s *Store) Update(id string, set map[string]any) (Opportunity, error) {
	op, ok := s.Get(id)
	if !ok {
		return Opportunity{}, fmt.Errorf("opportunity %q not found", id)
	}
	for key, raw := range set {
		switch key {
		case "firm":
			op.Firm = strings.TrimSpace(fmt.Sprint(raw))
			if op.Firm == "" {
				return op, errors.New("firm cannot be empty")
			}
		case "status":
			v := strings.ToLower(strings.TrimSpace(fmt.Sprint(raw)))
			if !validStatus(v) {
				return op, errors.New("invalid status")
			}
			op.Status = v
		case "interest":
			v := strings.ToLower(strings.TrimSpace(fmt.Sprint(raw)))
			if !validInterest(v) {
				return op, errors.New("invalid interest")
			}
			op.Interest = v
		case "amount":
			v := strings.TrimSpace(fmt.Sprint(raw))
			if v == "" {
				op.Amount = 0
			} else {
				f, e := strconv.ParseFloat(v, 64)
				if e != nil || f < 0 {
					return op, errors.New("amount must be a positive number")
				}
				op.Amount = f
			}
		case "source":
			source, err := normalizeSource(raw)
			if err != nil {
				return op, err
			}
			op.Source = source
			if source != nil && source.Contact != nil && source.Contact.NotePath == "" {
				if err := s.UpsertRegistry(RegistryPerson{Key: source.Contact.Key, Display: source.Contact.Display, Emails: source.Contact.Emails}); err != nil {
					return op, err
				}
			}
		case "introVia":
			op.IntroVia = fmt.Sprint(raw)
		case "lastTouchpoint":
			op.LastTouchpoint = fmt.Sprint(raw)
		case "lastTouchpointDate":
			op.LastTouchpointDate = fmt.Sprint(raw)
		case "nextStep":
			op.NextStep = fmt.Sprint(raw)
		case "nextStepDue":
			op.NextStepDue = fmt.Sprint(raw)
		case "notes":
			op.Notes = fmt.Sprint(raw)
		case "importReview":
			op.ImportReview = raw == true || fmt.Sprint(raw) == "true"
		default:
			return op, fmt.Errorf("field %q is not editable", key)
		}
	}
	return op, s.replaceKnown(op)
}

func normalizeSource(raw any) (*SourceRef, error) {
	if raw == nil {
		return nil, nil
	}
	if text, ok := raw.(string); ok {
		text = strings.TrimSpace(text)
		if text == "" {
			return nil, nil
		}
		return &SourceRef{Text: text}, nil
	}
	b, err := json.Marshal(raw)
	if err != nil {
		return nil, errors.New("source must be a contact or plain text")
	}
	var source SourceRef
	if err := json.Unmarshal(b, &source); err != nil {
		return nil, errors.New("source must be a contact or plain text")
	}
	source.Text = strings.TrimSpace(source.Text)
	if source.Contact != nil {
		source.Contact.Key = normalizeKey(source.Contact.Key)
		source.Contact.Display = strings.TrimSpace(source.Contact.Display)
		source.Contact.NotePath = filepath.ToSlash(strings.TrimSpace(source.Contact.NotePath))
		source.Contact.Emails = dedupeEmails(source.Contact.Emails)
		if source.Text != "" {
			return nil, errors.New("source cannot be both a contact and plain text")
		}
		if source.Contact.Key == "" || source.Contact.Display == "" {
			return nil, errors.New("source contact key and display are required")
		}
		return &source, nil
	}
	if source.Text == "" {
		return nil, nil
	}
	return &source, nil
}

func (s *Store) Archive(id string, archived bool) (Opportunity, error) {
	op, ok := s.Get(id)
	if !ok {
		return op, fmt.Errorf("opportunity %q not found", id)
	}
	op.Archived = archived
	return op, s.replaceKnown(op)
}

func normalizeKey(v string) string { return strings.ToLower(strings.TrimSpace(v)) }
func dedupeEmails(xs []string) []string {
	seen := map[string]bool{}
	out := []string{}
	for _, x := range xs {
		k := strings.ToLower(strings.TrimSpace(x))
		if k != "" && !seen[k] {
			seen[k] = true
			out = append(out, k)
		}
	}
	sort.Strings(out)
	return out
}

func (s *Store) AddPerson(id string, p PersonRef) (Opportunity, error) {
	op, ok := s.Get(id)
	if !ok {
		return op, fmt.Errorf("opportunity %q not found", id)
	}
	p.Key = normalizeKey(p.Key)
	p.Display = strings.TrimSpace(p.Display)
	p.Emails = dedupeEmails(p.Emails)
	if p.Key == "" || p.Display == "" {
		return op, errors.New("person key and display are required")
	}
	found := false
	for i := range op.People {
		if normalizeKey(op.People[i].Key) == p.Key {
			op.People[i] = p
			found = true
		}
	}
	if !found {
		op.People = append(op.People, p)
	}
	if p.NotePath == "" {
		if err := s.UpsertRegistry(RegistryPerson{Key: p.Key, Display: p.Display, Emails: p.Emails}); err != nil {
			return op, err
		}
	}
	return op, s.replaceKnown(op)
}
func (s *Store) RemovePerson(id, key string) (Opportunity, error) {
	op, ok := s.Get(id)
	if !ok {
		return op, fmt.Errorf("opportunity %q not found", id)
	}
	key = normalizeKey(key)
	out := op.People[:0]
	for _, p := range op.People {
		if normalizeKey(p.Key) != key {
			out = append(out, p)
		}
	}
	op.People = out
	return op, s.replaceKnown(op)
}

func (s *Store) registry() ([]RegistryPerson, []string, error) {
	b, err := os.ReadFile(s.abs(s.registryRel))
	if errors.Is(err, os.ErrNotExist) {
		return []RegistryPerson{}, []string{strings.TrimRight(registrySeed, "\n")}, nil
	}
	if err != nil {
		return nil, nil, err
	}
	var people []RegistryPerson
	var raw []string
	for _, ln := range strings.Split(strings.TrimRight(string(b), "\n"), "\n") {
		t := strings.TrimSpace(ln)
		if strings.HasPrefix(t, "- {") {
			var p RegistryPerson
			if json.Unmarshal([]byte(strings.TrimSpace(strings.TrimPrefix(t, "- "))), &p) == nil && p.Key != "" {
				p.Key = normalizeKey(p.Key)
				p.Emails = dedupeEmails(p.Emails)
				people = append(people, p)
				continue
			}
		}
		raw = append(raw, ln)
	}
	return people, raw, nil
}
func (s *Store) saveRegistry(people []RegistryPerson, raw []string) error {
	if s.writePeople == nil {
		return errors.New("fundraising: CRM contacts writer unavailable")
	}
	sort.Slice(people, func(i, j int) bool { return people[i].Key < people[j].Key })
	var b strings.Builder
	for _, ln := range raw {
		b.WriteString(ln)
		b.WriteByte('\n')
	}
	if len(raw) > 0 && strings.TrimSpace(raw[len(raw)-1]) != "" {
		b.WriteByte('\n')
	}
	for _, p := range people {
		x, _ := json.Marshal(p)
		b.WriteString("- ")
		b.Write(x)
		b.WriteByte('\n')
	}
	return s.writePeople(s.abs(s.registryRel), []byte(b.String()))
}
func (s *Store) RegistryPeople() []RegistryPerson { p, _, _ := s.registry(); return p }
func (s *Store) RegistryPerson(key string) (RegistryPerson, bool) {
	key = normalizeKey(key)
	for _, p := range s.RegistryPeople() {
		if p.Key == key {
			return p, true
		}
	}
	return RegistryPerson{}, false
}
func (s *Store) UpsertRegistry(in RegistryPerson) error {
	people, raw, err := s.registry()
	if err != nil {
		return err
	}
	in.Key = normalizeKey(in.Key)
	in.Display = strings.TrimSpace(in.Display)
	in.Emails = dedupeEmails(in.Emails)
	for i := range people {
		if people[i].Key == in.Key {
			if in.Display != "" {
				people[i].Display = in.Display
			}
			if in.NotePath != "" {
				people[i].NotePath = in.NotePath
			}
			people[i].Emails = dedupeEmails(append(people[i].Emails, in.Emails...))
			return s.saveRegistry(people, raw)
		}
	}
	people = append(people, in)
	return s.saveRegistry(people, raw)
}
func (s *Store) AddEmail(key, email string) error {
	p, ok := s.RegistryPerson(key)
	if !ok {
		return fmt.Errorf("CRM contact %q not found", key)
	}
	p.Emails = append(p.Emails, email)
	return s.UpsertRegistry(p)
}
func (s *Store) AttachNote(key, notePath string) error {
	p, ok := s.RegistryPerson(key)
	if !ok {
		return fmt.Errorf("CRM contact %q not found", key)
	}
	p.NotePath = filepath.ToSlash(notePath)
	return s.UpsertRegistry(p)
}

// People returns the explicit CRM contact universe: registry-only people plus
// all opportunity links. This explicit union is what keeps ordinary system
// wikilinks from leaking into Contacts.
func (s *Store) People() []RegistryPerson {
	by := map[string]RegistryPerson{}
	for _, p := range s.RegistryPeople() {
		by[p.Key] = p
	}
	ops, _ := s.List()
	for _, op := range ops {
		for _, r := range op.People {
			k := normalizeKey(r.Key)
			p := by[k]
			p.Key = k
			if p.Display == "" {
				p.Display = r.Display
			}
			if p.NotePath == "" {
				p.NotePath = r.NotePath
			}
			p.Emails = dedupeEmails(append(p.Emails, r.Emails...))
			by[k] = p
		}
		if op.Source != nil && op.Source.Contact != nil {
			r := *op.Source.Contact
			k := normalizeKey(r.Key)
			p := by[k]
			p.Key = k
			if p.Display == "" {
				p.Display = r.Display
			}
			if p.NotePath == "" {
				p.NotePath = r.NotePath
			}
			p.Emails = dedupeEmails(append(p.Emails, r.Emails...))
			by[k] = p
		}
	}
	out := make([]RegistryPerson, 0, len(by))
	for _, p := range by {
		out = append(out, p)
	}
	sort.Slice(out, func(i, j int) bool { return strings.ToLower(out[i].Display) < strings.ToLower(out[j].Display) })
	return out
}
func (s *Store) Person(key string) (RegistryPerson, bool) {
	key = normalizeKey(key)
	for _, p := range s.People() {
		if p.Key == key {
			return p, true
		}
	}
	return RegistryPerson{}, false
}
func (s *Store) OpportunitiesFor(key string) []Opportunity {
	key = normalizeKey(key)
	ops, _ := s.List()
	out := []Opportunity{}
	for _, op := range ops {
		matched := op.Source != nil && op.Source.Contact != nil && normalizeKey(op.Source.Contact.Key) == key
		for _, p := range op.People {
			if normalizeKey(p.Key) == key {
				matched = true
				break
			}
		}
		if matched {
			out = append(out, op)
		}
	}
	return out
}

func (s *Store) Resources() []Resource {
	b, err := os.ReadFile(s.abs(s.root + "/resources.md"))
	if err != nil {
		return []Resource{}
	}
	var out []Resource
	for _, ln := range strings.Split(string(b), "\n") {
		ln = strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(ln), "-"))
		if ln == "" {
			continue
		}
		if strings.HasPrefix(ln, "http://") || strings.HasPrefix(ln, "https://") {
			out = append(out, Resource{Title: ln, URL: ln})
			continue
		}
		if i := strings.Index(ln, "|"); i > 0 {
			url := strings.TrimSpace(ln[i+1:])
			if strings.HasPrefix(url, "http") {
				out = append(out, Resource{Title: strings.TrimSpace(ln[:i]), URL: url})
			}
		}
	}
	return out
}

func (s *Store) SaveResources(resources []Resource) error {
	if s.writeRecord == nil {
		return errors.New("fundraising: record writer unavailable")
	}
	var b strings.Builder
	b.WriteString("# Fundraising resources\n\n")
	for _, r := range resources {
		if strings.TrimSpace(r.URL) == "" {
			continue
		}
		title := strings.TrimSpace(r.Title)
		if title == "" || title == r.URL {
			b.WriteString("- " + r.URL + "\n")
		} else {
			b.WriteString("- " + title + " | " + r.URL + "\n")
		}
	}
	return s.writeRecord(s.abs(s.root+"/resources.md"), []byte(b.String()))
}

// patchFrontmatter updates only named scalar lines, retaining unknown fields,
// their order, comments, and the complete body byte-for-byte.
func patchFrontmatter(src []byte, updates map[string]*string) []byte {
	s := string(src)
	lines := strings.Split(s, "\n")
	if len(lines) == 0 || strings.TrimSpace(lines[0]) != "---" {
		return src
	}
	end := -1
	for i := 1; i < len(lines); i++ {
		if strings.TrimSpace(lines[i]) == "---" {
			end = i
			break
		}
	}
	if end < 0 {
		return src
	}
	done := map[string]bool{}
	out := []string{"---"}
	for _, ln := range lines[1:end] {
		k, _, ok := strings.Cut(ln, ":")
		key := strings.TrimSpace(k)
		v, wanted := updates[key]
		if ok && wanted {
			done[key] = true
			if v != nil {
				out = append(out, key+": "+*v)
			}
			continue
		}
		out = append(out, ln)
	}
	keys := make([]string, 0, len(updates))
	for k := range updates {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		if !done[k] && updates[k] != nil {
			out = append(out, k+": "+*updates[k])
		}
	}
	out = append(out, lines[end:]...)
	return []byte(strings.Join(out, "\n"))
}

// Touch is used by callers/tests to provide a deterministic updated timestamp
// without storing one in the record schema.
var Touch = time.Now
