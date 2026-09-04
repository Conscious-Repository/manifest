package server

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"regexp"
	"strings"
	"sync"
	"time"

	"manifest/hermes"
	"manifest/ledger"
	"manifest/recruiting"
	"manifest/recruiting/sources"
)

// SCAFFOLD — the model's pass, and the rule that makes it trustworthy
// (intake plan §5 stage C; owner's Q3: "c then a" — extractors first, the
// model for what is left).
//
// The extractors answer a registry: a DOI gives authors, a repo gives
// contributors, a feed gives a show. What they cannot do is read a lab page's
// prose and say "this is the Smith Lab at WashU BME, the PI is Jane Smith,
// twelve people are listed". That is what Alfred is for.
//
// ONE RULE, enforced here rather than requested in the prompt:
//
//	A filled field must appear in the material we fetched THIS TURN.
//
// Every suggestion is checked against the fetched bytes, whitespace- and
// case-normalised, and anything that does not appear is DROPPED with the
// reason recorded. A model that fills `org` from memory produces a record
// that looks sourced and is not, which is the one failure this whole package
// exists to prevent (D13/D15). The free-text note is exempt from the
// substring check because prose cannot be checked that way — so it is
// labelled as the model's words and never becomes a field.
//
// The turn is async: a hermes turn is minutes, and no HTTP handler waits for
// one. Jobs live in memory; a suggestion that dies with the process costs a
// re-ask, and nothing was written.

// scaffoldJob is one in-flight or finished ask.
type scaffoldJob struct {
	ID      string                 `json:"id"`
	Text    string                 `json:"text"`
	Status  string                 `json:"status"` // running | done | failed
	Started time.Time              `json:"started"`
	Error   string                 `json:"error,omitempty"`
	Suggest *scaffoldSuggestion    `json:"suggestion,omitempty"`
	Preview *sources.PreviewFacts  `json:"preview,omitempty"`
	Res     *recruiting.Resolution `json:"resolution,omitempty"`
	Dropped []string               `json:"dropped,omitempty"` // fields the citation rule refused
}

// scaffoldSuggestion is what the model proposes — the same shape the
// scaffold's own fields carry, so accepting it is a field copy, not a parse.
type scaffoldSuggestion struct {
	Class  string   `json:"class,omitempty"`
	Name   string   `json:"name,omitempty"`
	Org    string   `json:"org,omitempty"`
	Title  string   `json:"title,omitempty"`
	Note   string   `json:"note,omitempty"` // the model's words, labelled as such
	People []string `json:"people,omitempty"`
}

type scaffoldJobs struct {
	mu   sync.Mutex
	jobs map[string]*scaffoldJob
}

func (s *Server) scaffoldStore() *scaffoldJobs {
	s.scaffoldOnce.Do(func() { s.scaffolds = &scaffoldJobs{jobs: map[string]*scaffoldJob{}} })
	return s.scaffolds
}

// scaffoldJobTTL keeps a finished job around long enough to be read once.
const scaffoldJobTTL = 30 * time.Minute

func (j *scaffoldJobs) put(job *scaffoldJob) {
	j.mu.Lock()
	defer j.mu.Unlock()
	for id, have := range j.jobs {
		if time.Since(have.Started) > scaffoldJobTTL {
			delete(j.jobs, id)
		}
	}
	j.jobs[job.ID] = job
}

func (j *scaffoldJobs) get(id string) *scaffoldJob {
	j.mu.Lock()
	defer j.mu.Unlock()
	return j.jobs[id]
}

// POST /api/aion/recruiting/intake/ask {text} — start the model's pass.
func (s *Server) handleRecruitingScaffoldAsk(w http.ResponseWriter, r *http.Request) {
	if !s.recruitingReady(w) {
		return
	}
	if !s.hermesEnabled() {
		httpError(w, errBadRequest("the Hermes runner is not enabled here — the sources filled what they could"))
		return
	}
	var b struct {
		Text string `json:"text"`
	}
	if err := decode(r, &b); err != nil || strings.TrimSpace(b.Text) == "" {
		httpError(w, errBadRequest("paste a link or a name first"))
		return
	}
	res := recruiting.ResolveIntake(b.Text)
	job := &scaffoldJob{
		ID:      "scaffold-" + time.Now().UTC().Format("20060102-150405.000"),
		Text:    strings.TrimSpace(b.Text),
		Status:  "running",
		Started: time.Now(),
		Res:     &res,
	}
	s.scaffoldStore().put(job)
	go s.runScaffoldAsk(job)
	writeJSON(w, map[string]any{"id": job.ID, "status": job.Status})
}

// GET /api/aion/recruiting/intake/ask/{id}
func (s *Server) handleRecruitingScaffoldPoll(w http.ResponseWriter, r *http.Request) {
	if !s.recruitingReady(w) {
		return
	}
	job := s.scaffoldStore().get(r.PathValue("id"))
	if job == nil {
		http.Error(w, "no such scaffold job", http.StatusNotFound)
		return
	}
	writeJSON(w, job)
}

// runScaffoldAsk fetches the material, asks Alfred to read it, and keeps only
// what the material supports.
func (s *Server) runScaffoldAsk(job *scaffoldJob) {
	finish := func(err error) {
		if err != nil {
			job.Status, job.Error = "failed", strings.TrimSpace(err.Error())
		} else {
			job.Status = "done"
		}
		s.scaffoldStore().put(job)
	}

	material, prev := s.scaffoldMaterial(job)
	job.Preview = prev
	if strings.TrimSpace(material) == "" {
		finish(fmt.Errorf("nothing was fetched to read — there is nothing for a model to fill from"))
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
	defer cancel()
	out, err := s.hermes.runner.Run(ctx, hermes.Request{Prompt: scaffoldPrompt(job, material)})
	if err != nil {
		finish(err)
		return
	}
	s.ledger(ledger.Entry{Source: "run", Kind: "run.completed", Actor: "agent:" + alfredAgent,
		Object:  ledger.Object{Kind: ledger.ObjJob, ID: job.ID},
		Harness: "hermes", Text: "recruiting scaffold for " + ledger.Snip(job.Text, 100),
		Meta: map[string]any{"spentUsd": out.SpentUSD, "model": out.Model, "sessionId": out.SessionID}})

	sug, err := parseScaffoldReply(out.Reply)
	if err != nil {
		log.Printf("recruiting scaffold %s: %v (reply: %s)", job.ID, err, ledger.Snip(out.Reply, 200))
		finish(err)
		return
	}
	job.Suggest, job.Dropped = enforceCitationRule(sug, material)
	finish(nil)
}

// scaffoldMaterial is everything we fetched for this paste, as text. This is
// the ONLY thing the model is given, and the only thing its answer is checked
// against.
func (s *Server) scaffoldMaterial(job *scaffoldJob) (string, *sources.PreviewFacts) {
	if s.recruitingRuns == nil || job.Res == nil {
		return "", nil
	}
	id, ref := intakePreviewTarget(*job.Res)
	if id == "" {
		return "", nil
	}
	adapter, ok := s.recruitingRuns.Adapter(id)
	if !ok {
		return "", nil
	}
	prev, ok := adapter.(sources.Previewer)
	if !ok {
		return "", nil
	}
	ctx, cancel := context.WithTimeout(context.Background(), intakePreviewTimeout)
	defer cancel()
	facts, err := prev.Preview(ctx, ref)
	if err != nil {
		return "", nil
	}
	var b strings.Builder
	fmt.Fprintf(&b, "SOURCE: %s\nURL: %s\nNAME AS FETCHED: %s\n", id, facts.URL, facts.Name)
	if facts.Org != "" {
		fmt.Fprintf(&b, "ORG AS FETCHED: %s\n", facts.Org)
	}
	for _, f := range facts.Facts {
		fmt.Fprintf(&b, "FIELD %s: %s (%s)\n", f.Field, f.Value, f.URL)
	}
	for _, p := range facts.People {
		fmt.Fprintf(&b, "PERSON: %s%s\n", p.Name, orIfSet(" — ", p.Org))
	}
	if facts.Note != "" {
		fmt.Fprintf(&b, "PAGE TEXT:\n%s\n", facts.Note)
	}
	return b.String(), &facts
}

func orIfSet(prefix, s string) string {
	if strings.TrimSpace(s) == "" {
		return ""
	}
	return prefix + s
}

// scaffoldPrompt states the job and the rule. The rule is ALSO enforced after
// the fact — a prompt is a request, not a guarantee.
func scaffoldPrompt(job *scaffoldJob, material string) string {
	classes := strings.Join(recruiting.SeedClasses, " | ")
	return "You are filling in a scaffold for a recruiting record in Manifest. " +
		"Read ONLY the material below — it is everything that was fetched for this item. " +
		"Do not use anything you know from memory or training: a field you cannot point at " +
		"in this material must be left empty, and will be dropped if you fill it anyway.\n\n" +
		"THE PASTE: " + job.Text + "\n\nMATERIAL:\n" + material + "\n\n" +
		"Answer with ONE fenced ```json block and nothing else, exactly this shape:\n" +
		"{\"class\":\"" + classes + "\",\"name\":\"…\",\"org\":\"…\",\"title\":\"…\"," +
		"\"note\":\"one sentence on what this is and why it might matter to a hiring search\"," +
		"\"people\":[\"names the material NAMES, verbatim\"]}\n\n" +
		"name / org / title / people must be COPIED from the material, character for character. " +
		"`note` is the one place you may write your own sentence. Leave any field you cannot " +
		"support as an empty string."
}

var scaffoldFenceRe = regexp.MustCompile("(?s)```(?:json)?\\s*(\\{.*?\\})\\s*```")

// parseScaffoldReply pulls the JSON out of the reply, fenced or bare.
func parseScaffoldReply(reply string) (scaffoldSuggestion, error) {
	var out scaffoldSuggestion
	raw := ""
	if m := scaffoldFenceRe.FindStringSubmatch(reply); m != nil {
		raw = m[1]
	} else if i, j := strings.Index(reply, "{"), strings.LastIndex(reply, "}"); i >= 0 && j > i {
		raw = reply[i : j+1]
	}
	if raw == "" {
		return out, fmt.Errorf("the turn answered with no JSON block")
	}
	if err := json.Unmarshal([]byte(raw), &out); err != nil {
		return out, fmt.Errorf("the turn's JSON did not parse: %v", err)
	}
	return out, nil
}

// enforceCitationRule drops every field the fetched material does not
// support, and says which. This is the whole trust model: a suggestion that
// survives it is quotable from bytes we hold.
func enforceCitationRule(in scaffoldSuggestion, material string) (*scaffoldSuggestion, []string) {
	hay := normalizeForCite(material)
	var dropped []string
	keep := func(field, value string) string {
		v := strings.TrimSpace(value)
		if v == "" {
			return ""
		}
		if !strings.Contains(hay, normalizeForCite(v)) {
			dropped = append(dropped, field+": “"+ledger.Snip(v, 60)+"” is not in what we fetched")
			return ""
		}
		return v
	}
	out := scaffoldSuggestion{
		Name:  keep("name", in.Name),
		Org:   keep("org", in.Org),
		Title: keep("title", in.Title),
		Note:  strings.TrimSpace(in.Note), // prose: the model's words, never a field
	}
	if c := strings.ToLower(strings.TrimSpace(in.Class)); recruiting.ValidSeedClass(c) {
		out.Class = c
	} else if c != "" {
		dropped = append(dropped, "class: “"+c+"” is not one of "+strings.Join(recruiting.SeedClasses, ", "))
	}
	for _, p := range in.People {
		if v := keep("person", p); v != "" {
			out.People = append(out.People, v)
		}
	}
	return &out, dropped
}

// normalizeForCite makes the substring check robust to the ways the same
// string is spelled in two places — whitespace runs, case, curly quotes —
// without making it so loose that it stops checking anything.
var citeWS = regexp.MustCompile(`\s+`)

func normalizeForCite(s string) string {
	s = strings.ToLower(s)
	s = strings.NewReplacer("’", "'", "‘", "'", "“", `"`, "”", `"`, "—", "-", "–", "-").Replace(s)
	return citeWS.ReplaceAllString(s, " ")
}
