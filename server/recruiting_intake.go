package server

import (
	"context"
	"net/http"
	"strings"
	"time"

	"manifest/recruiting"
	"manifest/recruiting/sources"
)

// RECRUITING INTAKE — the one front door (intake plan §5).
//
// Three boxes used to write three different thin rows: a rail seed that
// demanded a `class: name` prefix and landed in a file nothing read, a board
// paste that made an EMPTY candidate record, and a network add that promised
// an editor which does not exist. This pair of routes replaces all three.
//
//	resolve  — pure, no network: what the paste IS, where it would land, and
//	           which adapters can speak about it. The client shows that as a
//	           correctable scaffold instead of guessing silently.
//	commit   — writes it where the (possibly corrected) resolution says.
//
// D15 is unchanged here: nothing on this path can set an email or a phone
// except the owner typing one, and no adapter runs — a commit stores what the
// owner saw and approved.

// POST /api/aion/recruiting/intake/resolve {text}
func (s *Server) handleRecruitingIntakeResolve(w http.ResponseWriter, r *http.Request) {
	if !s.recruitingReady(w) {
		return
	}
	var b struct {
		Text string `json:"text"`
	}
	if err := decode(r, &b); err != nil {
		httpError(w, errBadRequest("paste a link or a name"))
		return
	}
	res := recruiting.ResolveIntake(b.Text)
	if strings.TrimSpace(res.Text) == "" {
		httpError(w, errBadRequest("paste a link or a name"))
		return
	}
	writeJSON(w, map[string]any{
		"resolution": res,
		"classes":    recruiting.SeedClasses,
		"people":     s.recruiting.View().Network.People,
	})
}

// intakePreviewTimeout bounds the one or two GETs a preview costs. It is well
// under the request's own patience: a preview that has to be waited on is a
// run, and runs have a queue.
const intakePreviewTimeout = 20 * time.Second

// POST /api/aion/recruiting/intake/preview {text, class?}
//
// The deterministic scaffold fill (owner's Q3: extractors first, a model only
// for what is left). When the paste names ONE thing — this DOI, this account,
// this repo — the source's own record answers what it is, field by field,
// each with the URL it came from. When the paste names a SEARCH (a bare name,
// a lab site), there is nothing to preview: the answer is a sweep with a
// queue to triage, and the scaffold says so rather than pretending.
func (s *Server) handleRecruitingIntakePreview(w http.ResponseWriter, r *http.Request) {
	if !s.recruitingReady(w) {
		return
	}
	var b struct {
		Text string `json:"text"`
	}
	if err := decode(r, &b); err != nil {
		httpError(w, errBadRequest("paste a link or a name"))
		return
	}
	res := recruiting.ResolveIntake(b.Text)
	out := map[string]any{"resolution": res}
	if s.recruitingRuns == nil {
		out["note"] = "the source adapters are not wired here — nothing to look up"
		writeJSON(w, out)
		return
	}

	id, ref := intakePreviewTarget(res)
	if id == "" {
		out["note"] = intakeNoPreviewNote(res)
		writeJSON(w, out)
		return
	}
	adapter, ok := s.recruitingRuns.Adapter(id)
	if !ok {
		out["note"] = "the " + id + " source is not wired here"
		writeJSON(w, out)
		return
	}
	prev, ok := adapter.(sources.Previewer)
	if !ok {
		out["note"] = "the " + id + " source answers searches, not single references"
		writeJSON(w, out)
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), intakePreviewTimeout)
	defer cancel()
	facts, err := prev.Preview(ctx, ref)
	if err != nil {
		// a lookup that fails is said out loud and the scaffold still opens:
		// the owner can name the thing by hand, which is how this worked
		// before anything could be looked up at all
		out["error"] = strings.TrimSpace(err.Error())
		writeJSON(w, out)
		return
	}
	out["preview"] = facts
	out["source"] = id
	writeJSON(w, out)
}

// intakePreviewTarget maps a resolution onto the adapter that can answer it
// with ONE record, and the reference to ask about. ("" = nothing to preview.)
func intakePreviewTarget(res recruiting.Resolution) (id, ref string) {
	switch res.Kind {
	case "doi", "pubmed":
		if res.DOI != "" {
			return "openalex", res.DOI
		}
		return "openalex", res.URL
	case "openalex":
		return "openalex", res.URL
	case "orcid":
		if res.ORCID != "" {
			return "openalex", res.ORCID
		}
		return "openalex", res.URL
	case "grant":
		return "", "" // RePORTER answers project searches, not one project by id
	case "github-user":
		return "github", res.Handle
	case "github-repo":
		return "github", res.Name
	case "feed":
		return "feed", res.URL
	case "site":
		// ONE page, not a crawl: enough to name the lab from its own title
		return "web", res.URL
	}
	return "", ""
}

// intakeNoPreviewNote says WHY there is nothing to look up, in the terms the
// owner is thinking in.
func intakeNoPreviewNote(res recruiting.Resolution) string {
	switch {
	case res.LinkOnly:
		return "nothing here reads " + res.Kind + " profiles — the link is recorded on the person, and that is all it is"
	case res.Kind == "name":
		return "a name is a search — add it, then sweep the sources for it"
	case res.Kind == "grant":
		return "RePORTER answers project searches, not one project by id — sweep it"
	}
	return "nothing to look up for this one"
}

// POST /api/aion/recruiting/intake — commit a (corrected) resolution.
func (s *Server) handleRecruitingIntake(w http.ResponseWriter, r *http.Request) {
	if !s.recruitingReady(w) {
		return
	}
	var b struct {
		Text     string `json:"text"`
		Dest     string `json:"dest"`
		Class    string `json:"class"`
		Name     string `json:"name"`
		Org      string `json:"org"`
		URL      string `json:"url"`
		Feed     string `json:"feed"`
		Role     string `json:"role"`
		Profile  string `json:"profile"` // which profile field the URL is (linkedin|github|x|website)
		Known    bool   `json:"known"`
		KnownVia string `json:"knownVia"`
	}
	if err := decode(r, &b); err != nil {
		httpError(w, errBadRequest("paste a link or a name"))
		return
	}
	name := strings.TrimSpace(b.Name)
	text := strings.TrimSpace(b.Text)
	if name == "" {
		name = text
	}
	if name == "" {
		httpError(w, errBadRequest("this needs a name — the one derived from the link is a slug, not a name"))
		return
	}
	now := time.Now()

	switch strings.TrimSpace(b.Dest) {
	case recruiting.DestSeed:
		class := strings.ToLower(strings.TrimSpace(b.Class))
		if !recruiting.ValidSeedClass(class) {
			httpError(w, errBadRequest("say what this is: "+strings.Join(recruiting.SeedClasses, ", ")))
			return
		}
		seed := recruiting.Seed{Class: class, Name: name,
			Org: strings.TrimSpace(b.Org), URL: strings.TrimSpace(b.URL)}
		// a show or blog keeps its feed on the row, so a later sweep needs no
		// second discovery pass
		if feed := strings.TrimSpace(b.Feed); feed != "" {
			seed.Unknown = append(seed.Unknown, recruiting.Field{Key: "feed", Value: feed})
		}
		stored, err := s.recruiting.AddSeed(seed, now)
		if err != nil {
			httpError(w, err)
			return
		}
		s.recruitingIntakeDone(w, "seed", stored.ID, stored.Name)

	case recruiting.DestNetwork:
		if err := s.recruiting.AddNetworkPerson(recruiting.NetworkPerson{
			Name: name, Org: strings.TrimSpace(b.Org), Source: "owner",
			Consent: "owner", Added: now.UTC().Format("2006-01-02"),
		}); err != nil {
			httpError(w, err)
			return
		}
		s.recruitingIntakeDone(w, "network", "", name)

	default: // DestCandidate
		if b.Known && strings.TrimSpace(b.KnownVia) == "" {
			httpError(w, errBadRequest("say who knows them — the edge is only worth as much as the person asserting it"))
			return
		}
		c, err := s.recruiting.AddCandidate(recruiting.QuickAdd{
			Text: text, Name: name, Org: strings.TrimSpace(b.Org),
			Role: strings.TrimSpace(b.Role), Known: b.Known,
			KnownVia: strings.TrimSpace(b.KnownVia),
		}, now)
		if err != nil {
			httpError(w, err)
			return
		}
		// a profile URL belongs in its own slot, not only in the note — this
		// is how an X or LinkedIn link is RECORDED without anything crawling it
		if field, url := strings.TrimSpace(b.Profile), strings.TrimSpace(b.URL); field != "" && url != "" {
			if _, err := s.recruiting.UpdateCandidate(c.ID, map[string]string{field: url}); err != nil {
				httpError(w, err)
				return
			}
		}
		s.recruitingIntakeDone(w, "candidate", c.ID, c.Name)
	}
}

// recruitingIntakeDone answers with the fresh view plus what was created, so
// the client can select it without a second fetch.
func (s *Server) recruitingIntakeDone(w http.ResponseWriter, kind, id, name string) {
	writeJSON(w, map[string]any{
		"created": map[string]string{"kind": kind, "id": id, "name": name},
		"view":    s.recruiting.View(),
	})
}
