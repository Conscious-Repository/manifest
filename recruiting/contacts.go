package recruiting

import (
	"context"
	"encoding/json"
	"net/url"
	"sort"
	"strings"
	"time"

	"manifest/recruiting/sources"
)

// PUBLISHED CONTACTS — the bounded address pass over a web run's queue
// (owner ask 2026-09-21). A lab sweep lands people with no way to reach
// them; the lab's own people page prints their addresses. This reads the
// pages the queue's drafts were FOUND ON, asks the web adapter which
// printed address the page binds to which printed name
// (sources.Web.LookupContacts — the rule lives there), and files each
// binding on the draft as an evidence row of kind contact_published: the
// page's URL, the name line and the address quoted verbatim. That row is
// where D15 says a published address may live, and it shows wherever draft
// evidence already renders.
//
// Like a lookup, this enriches the QUEUE and nothing else. No record is
// created, no status changes, no tombstone moves; the run cache is written
// through writeRun like every other queue mutation. A draft that cannot be
// bound to an address with confidence is left exactly as it was — a blank
// is correct, a guess is a defect.
//
// A page is recorded UNAVAILABLE, and nothing is taken from it, when it
// cannot be read, when its bytes are the same body another URL already
// served in this pass (a challenge page wears many URLs), or when it prints
// none of the queued names (it is not the roster it was meant to be).

// contactsMaxPages bounds the pages read for one run: the roster page and a
// few person pages, never a crawl.
const contactsMaxPages = 8

// contactsBudget bounds the wall time one run's pass may spend.
const contactsBudget = 90 * time.Second

// Contacts page statuses.
const (
	ContactsPageRead        = "read"
	ContactsPageUnavailable = "unavailable"
	ContactsPageError       = "error"
)

// ContactsPage is one page the pass touched and what came of it.
type ContactsPage struct {
	URL    string `json:"url"`
	Status string `json:"status"`
	Why    string `json:"why,omitempty"`
	// Bound counts the (name, address) pairs the page's markup tied to
	// queued drafts, whether or not they were new to the queue.
	Bound int `json:"bound"`
}

// ContactsResult is what one pass over one run reports, and what run.json
// keeps of it (RunState.Contacts) so the queue can say the pass ran, when,
// and whether the lab's page was readable at all.
type ContactsResult struct {
	RunID string    `json:"run"`
	At    time.Time `json:"at"`
	// Seen is the drafts considered — those still `new`, the ones a
	// published address would help triage.
	Seen int `json:"seen"`
	// Resolved is how many of them hold at least one published address
	// after the pass; Unset the rest. Added is the evidence rows this pass
	// appended (a second pass over the same page adds none).
	Resolved int `json:"resolved"`
	Unset    int `json:"unset"`
	Added    int `json:"added"`
	// Unavailable is set when no page could be read as a roster.
	Unavailable bool           `json:"unavailable"`
	Pages       []ContactsPage `json:"pages"`
}

// contactSource is the one adapter method the pass needs. Only the web
// adapter implements it; the run's pages are web pages.
type contactSource interface {
	LookupContacts(ctx context.Context, pageURL string, roster []string) (sources.ContactPage, error)
}

// ContactPass reads people pages for one run at a time and remembers, across
// runs, which bodies it has seen: the same bytes under two URLs is the
// boilerplate signal, and the same URL asked twice (three sweeps of one lab)
// is read once.
type ContactPass struct {
	r      *RunStore
	source contactSource
	// Persist false reports what the pass would file without writing it.
	Persist bool
	pages   map[string]contactRead // URL → what it read
	bodies  map[string]string      // body hash → first URL that served it
}

type contactRead struct {
	page sources.ContactPage
	err  error
}

// NewContactPass builds a pass over this store's registered web adapter.
func (r *RunStore) NewContactPass(persist bool) (*ContactPass, error) {
	r.mu.Lock()
	a, ok := r.adapters["web"]
	r.mu.Unlock()
	src, has := a.(contactSource)
	if !ok || !has {
		return nil, errf("the web source is not registered; nothing can read a lab page")
	}
	return &ContactPass{r: r, source: src, Persist: persist, pages: map[string]contactRead{}, bodies: map[string]string{}}, nil
}

// WebRunIDs lists the ids of every web run in the cache, oldest first —
// the set a whole-cache pass walks.
func (r *RunStore) WebRunIDs() []string {
	r.mu.Lock()
	defer r.mu.Unlock()
	var out []string
	for _, id := range r.ids() {
		run, err := r.load(id)
		if err == nil && run.Source == "web" {
			out = append(out, id)
		}
	}
	return out
}

// Run performs the pass over one run: reads its pages, binds addresses to
// its `new` drafts, files them as evidence, and records the result on the
// run. The queue is compared before and after the network work; a draft
// decided meanwhile makes the pass refuse rather than overwrite a decision.
func (p *ContactPass) Run(ctx context.Context, runID string, now time.Time) (Run, ContactsResult, error) {
	ctx, cancel := context.WithTimeout(ctx, contactsBudget)
	defer cancel()
	p.r.mu.Lock()
	run, err := p.r.load(runID)
	p.r.mu.Unlock()
	if err != nil {
		return Run{}, ContactsResult{}, err
	}
	if run.Source != "web" {
		return Run{}, ContactsResult{}, errf("run %s is a %s run; only a web run has a lab page to read", runID, run.Source)
	}
	original, _ := json.Marshal(run.Drafts)

	res := ContactsResult{RunID: runID, At: now.UTC(), Pages: []ContactsPage{}}
	roster := contactRoster(run)
	readable := 0
	for _, pageURL := range contactPages(run, "") {
		read := p.read(ctx, pageURL, roster)
		res.Pages = append(res.Pages, read)
		if read.Status != ContactsPageRead {
			continue
		}
		readable++
		added, bound := applyContactPage(&run, p.pages[pageURL].page, "", now)
		res.Added += added
		res.Pages[len(res.Pages)-1].Bound = bound
	}
	res.Unavailable = readable == 0
	for _, d := range run.Drafts {
		if d.Status != DraftNew {
			continue
		}
		res.Seen++
		if hasPublishedContact(d.Draft) {
			res.Resolved++
		} else {
			res.Unset++
		}
	}
	if !p.Persist {
		return p.r.project(run, nil), res, nil
	}

	p.r.mu.Lock()
	defer p.r.mu.Unlock()
	latest, err := p.r.load(runID)
	if err != nil {
		return Run{}, ContactsResult{}, err
	}
	if current, _ := json.Marshal(latest.Drafts); string(current) != string(original) {
		return Run{}, ContactsResult{}, errf("the queue of run %s changed during the pass; retry from its current state", runID)
	}
	latest.Drafts = run.Drafts
	latest.Contacts = &res
	if err := p.r.writeRun(latest, nil); err != nil {
		return Run{}, ContactsResult{}, err
	}
	return p.r.project(latest, nil), res, nil
}

// read fetches one page once per pass and classifies it.
func (p *ContactPass) read(ctx context.Context, pageURL string, roster []string) ContactsPage {
	out := ContactsPage{URL: pageURL, Status: ContactsPageRead}
	got, cached := p.pages[pageURL]
	if !cached {
		page, err := p.source.LookupContacts(ctx, pageURL, roster)
		got = contactRead{page: page, err: err}
		p.pages[pageURL] = got
	}
	if got.err != nil {
		out.Status, out.Why = ContactsPageError, got.err.Error()
		return out
	}
	page := got.page
	if first, seen := p.bodies[page.BodyHash]; seen && first != pageURL && first != page.URL {
		out.Status, out.Why = ContactsPageUnavailable, "identical body to "+first+" — a boilerplate page, not a roster"
		return out
	}
	if !cached {
		p.bodies[page.BodyHash] = pageURL
	}
	if len(page.Named) == 0 {
		out.Status, out.Why = ContactsPageUnavailable, "prints none of the queued names — not the roster it was meant to be"
		return out
	}
	return out
}

// contactRoster is every name the run holds, any status: a card that prints
// two of them is ambiguous whichever is still queued.
func contactRoster(run Run) []string {
	seen := map[string]bool{}
	var out []string
	for _, d := range run.Drafts {
		name := strings.TrimSpace(d.Draft.Name)
		if name == "" || seen[name] {
			continue
		}
		seen[name] = true
		out = append(out, name)
	}
	return out
}

// contactPages lists the pages a run's `new` drafts were found on — their
// `page` evidence from the web source — on the seed's own host only, the
// page most drafts came from first, capped at contactsMaxPages. With
// draftID set, only that draft's pages. A lookup's GitHub or Scholar page
// is somebody else's site and is not read for addresses.
func contactPages(run Run, draftID string) []string {
	host := ""
	if seed, err := url.Parse(strings.TrimSpace(run.Scope.Fields["seed_url"])); err == nil {
		host = strings.ToLower(seed.Hostname())
	}
	count := map[string]int{}
	first := map[string]int{}
	for _, d := range run.Drafts {
		if d.Status != DraftNew || (draftID != "" && d.ID != draftID) {
			continue
		}
		for _, e := range d.Draft.Evidence {
			if e.Kind != sources.EvidencePage || e.SourceID != "web" {
				continue
			}
			u := strings.TrimSpace(e.URLOrFile)
			pu, err := url.Parse(u)
			if err != nil || pu.Host == "" || (host != "" && !strings.EqualFold(pu.Hostname(), host)) {
				continue
			}
			if _, ok := first[u]; !ok {
				first[u] = len(first)
			}
			count[u]++
		}
	}
	out := make([]string, 0, len(count))
	for u := range count {
		out = append(out, u)
	}
	sort.Slice(out, func(i, j int) bool {
		if count[out[i]] != count[out[j]] {
			return count[out[i]] > count[out[j]]
		}
		return first[out[i]] < first[out[j]]
	})
	if len(out) > contactsMaxPages {
		out = out[:contactsMaxPages]
	}
	return out
}

// applyContactPage files a read page's bindings on the run's `new` drafts
// that carry the bound name (the web identity rule: the same printed name in
// one run is one person, however many queue entries repeat it). With draftID
// set, only that draft. Returns rows added and bindings that matched a
// draft; a row already on the draft is not added twice.
func applyContactPage(run *Run, page sources.ContactPage, draftID string, now time.Time) (added, bound int) {
	byKey := map[string][]int{}
	for i, d := range run.Drafts {
		if d.Status != DraftNew || (draftID != "" && d.ID != draftID) {
			continue
		}
		key := sources.WebPersonKey(d.Draft.Name)
		byKey[key] = append(byKey[key], i)
	}
	for _, b := range page.Bound {
		targets := byKey[sources.WebPersonKey(b.Name)]
		if len(targets) == 0 {
			continue
		}
		bound++
		row := sources.Evidence{
			SourceID: "web", URLOrFile: page.URL, RetrievedAt: now.UTC(),
			Snippet: b.Printed + " · " + b.Address, Kind: sources.EvidenceContactPublished, Trust: sources.TrustMedium,
		}
		for _, i := range targets {
			d := &run.Drafts[i]
			dup := false
			for _, e := range d.Draft.Evidence {
				if lookupCitationKey(e) == lookupCitationKey(row) {
					dup = true
					break
				}
			}
			if dup {
				continue
			}
			d.Draft.Evidence = append(d.Draft.Evidence, row)
			added++
		}
	}
	return added, bound
}

// hasPublishedContact reports whether a draft carries a contact_published
// row that actually quotes an address.
func hasPublishedContact(d sources.CandidateDraft) bool {
	for _, e := range d.Evidence {
		if e.Kind == sources.EvidenceContactPublished && strings.Contains(e.Snippet, "@") {
			return true
		}
	}
	return false
}

// lookupContacts is the per-draft hook the cross-source Lookup calls: the
// draft's own found-on pages, the run's whole roster, filed on this draft
// only. Failures are reported through the pages, never fatal — the rest of
// the lookup already landed.
func (p *ContactPass) lookupContacts(ctx context.Context, run *Run, draftID string, now time.Time) (int, []ContactsPage) {
	roster := contactRoster(*run)
	var pages []ContactsPage
	added := 0
	for _, pageURL := range contactPages(*run, draftID) {
		read := p.read(ctx, pageURL, roster)
		if read.Status == ContactsPageRead {
			n, bound := applyContactPage(run, p.pages[pageURL].page, draftID, now)
			added += n
			read.Bound = bound
		}
		pages = append(pages, read)
	}
	return added, pages
}
