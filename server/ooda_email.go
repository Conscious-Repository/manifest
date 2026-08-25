package server

// The OODA portal's EMAIL lane (ooda-portal email plan, 2026-08-21): each
// member's own mailbox → pending candidates (gmailsync) → member/admin
// confirm → shared artifact (artifacts domain "ooda") → the extractor spooled
// with the note text INLINE (data beats instructions — intake.go's rule).
//
// Visibility is the portal's ONE deliberate break from the flat-reads
// doctrine: a PENDING candidate is the source member's mail, readable by that
// member and the admin only. Everything CONFIRMED is a shared artifact and
// flat again. TestPendingEmailNotesAreScopedToSourceAndAdmin pins it.

import (
	"fmt"
	"net/http"
	"path"
	"strings"
	"time"

	"manifest/artifacts"
	"manifest/gmailsync"
	"manifest/spirits"
	"manifest/teamportal"
)

// UseOodaEmail wires the portal email lane (nil-safe: nothing wired, no
// routes).
func (s *Server) UseOodaEmail(cands *gmailsync.Candidates, tokens *gmailsync.Tokens) {
	s.oodaEmail = cands
	s.oodaGmail = tokens
}

// OodaEmailRoster builds the gmailsync relevance filter over the live OODA
// roster: people.md partners + contractor records (via OodaLive.People) ∪ the
// emails.json map (address → initials, resolved to the person's name when the
// roster carries it). Rebuilt per sync pass so edits are live.
func (s *Server) OodaEmailRoster(live *OodaLive, store *teamportal.Store) gmailsync.Resolver {
	byEmail := map[string]string{}
	var people []portalPerson
	if live != nil {
		people = live.People()
	}
	for _, p := range people {
		if e := strings.ToLower(strings.TrimSpace(p.Email)); e != "" {
			byEmail[e] = p.Name
		}
	}
	if store != nil {
		for email, initials := range store.EmailOverrides() {
			e := strings.ToLower(strings.TrimSpace(email))
			if e == "" || byEmail[e] != "" {
				continue
			}
			name := initials
			for _, p := range people {
				if strings.EqualFold(p.Initials, initials) {
					name = p.Name
					break
				}
			}
			byEmail[e] = name
		}
	}
	return rosterResolver(byEmail)
}

type rosterResolver map[string]string

func (r rosterResolver) PersonByEmail(email string) (string, bool) {
	d, ok := r[strings.ToLower(email)]
	return d, ok
}

// registerOodaEmailRoutes mounts the lane on the portal mux (called from
// OodaReadRoutes; auth is a hard requirement — mail never rides an ungated
// portal).
func (a *oodaAPI) registerEmailRoutes(mux *http.ServeMux) {
	s := a.live.s
	if s == nil || s.oodaEmail == nil || a.opt.Auth == nil {
		return
	}
	mux.HandleFunc("GET /api/ooda/gmail", a.gmailStatus)
	mux.HandleFunc("GET /api/ooda/email", a.emailPending)
	mux.HandleFunc("POST /api/ooda/email/{id}", a.emailDecide)
}

// isOodaAdmin — the portal's entire role model is this one comparison
// (portal.go isAdmin, shared shape).
func (a *oodaAPI) isOodaAdmin(email string) bool {
	return a.opt.AdminEmail != "" && strings.EqualFold(email, a.opt.AdminEmail)
}

// gmailStatus reports the CALLER's mailbox connection. Not connected simply
// means their sign-in predates the Gmail bundling (or they unticked the
// checkbox) — reconnecting is just signing in again.
func (a *oodaAPI) gmailStatus(w http.ResponseWriter, r *http.Request) {
	id, ok := PortalIdentify(a.opt, w, r)
	if !ok {
		return
	}
	s := a.live.s
	acc, connected := s.oodaGmail.Status(id.Email)
	writeJSON(w, map[string]any{
		"connected":   connected && !acc.NeedsReauth,
		"needsReauth": acc.NeedsReauth,
		"checkedAt":   acc.CheckedAt,
	})
}

// oodaEmailCard is one FEED card: a conversation, not a mailbox's copy of
// one. The face candidate is the one a decide should target (a member's own
// copy when they hold one); Mailboxes lists every account holding a copy.
type oodaEmailCard struct {
	gmailsync.Candidate
	Mailboxes []string `json:"mailboxes,omitempty"`
	GroupSize int      `json:"groupSize"`
}

// oodaPendingEmailCards is THE pending-email read, shared by /api/ooda/email
// and the FEED aggregate so the two can never disagree. One card per
// conversation (fingerprint), folded across mailboxes; the visibility rule is
// unchanged — a member gets a card iff their own mailbox holds a copy, the
// admin gets everything.
func oodaPendingEmailCards(cands []gmailsync.Candidate, admin bool, email string) []*oodaEmailCard {
	out := []*oodaEmailCard{}
	seen := map[string]int{}
	for _, c := range cands {
		fp := gmailsync.FingerprintOf(c)
		if at, ok := seen[fp]; ok {
			card := out[at]
			card.Mailboxes = append(card.Mailboxes, c.Account)
			card.GroupSize++
			// a member's own copy becomes the card face, so their decide
			// targets their mailbox's candidate id
			if !admin && strings.EqualFold(c.Account, email) {
				card.Candidate = c
			}
			continue
		}
		if admin || strings.EqualFold(c.Account, email) {
			seen[fp] = len(out)
			out = append(out, &oodaEmailCard{Candidate: c, Mailboxes: []string{c.Account}, GroupSize: 1})
		} else {
			// a foreign copy seen FIRST still anchors the group, invisibly,
			// so the member's later copy folds instead of duplicating —
			// marked by a sentinel the second pass prunes
			seen[fp] = len(out)
			out = append(out, &oodaEmailCard{Candidate: gmailsync.Candidate{}, Mailboxes: []string{c.Account}, GroupSize: 1})
		}
	}
	// prune anchors whose group never gained a visible face
	vis := out[:0]
	for _, card := range out {
		if card.ID != "" {
			vis = append(vis, card)
		}
	}
	return vis
}

// emailPending lists pending candidates — the admin sees all, a member sees
// exactly their own mailbox's conversations. The scoping happens HERE, not in
// the store: the store is shared state, the boundary is the read.
func (a *oodaAPI) emailPending(w http.ResponseWriter, r *http.Request) {
	id, ok := PortalIdentify(a.opt, w, r)
	if !ok {
		return
	}
	s := a.live.s
	admin := a.isOodaAdmin(id.Email)
	out := oodaPendingEmailCards(s.oodaEmail.List(gmailsync.StatusPending), admin, id.Email)
	writeJSON(w, map[string]any{"pending": out, "admin": admin})
}

// emailDecide confirms or dismisses ONE pending candidate — allowed for its
// source member and the admin, 403 for everyone else. Confirm turns the note
// into a shared artifact and spools the extractor; dismiss mutes the thread
// forever.
func (a *oodaAPI) emailDecide(w http.ResponseWriter, r *http.Request) {
	id, ok := PortalIdentify(a.opt, w, r)
	if !ok {
		return
	}
	s := a.live.s
	var b struct {
		Action string `json:"action"` // confirm | dismiss
	}
	if err := decode(r, &b); err != nil {
		httpError(w, err)
		return
	}
	cand, found := s.oodaEmail.Get(r.PathValue("id"))
	if !found {
		http.Error(w, "no such candidate", http.StatusNotFound)
		return
	}
	if !a.isOodaAdmin(id.Email) && !strings.EqualFold(cand.Account, id.Email) {
		http.Error(w, "not your mailbox", http.StatusForbidden)
		return
	}
	// The decision settles the CONVERSATION, not one mailbox's copy of it:
	// gmail thread ids are mailbox-local, so the same thread pends once per
	// member and deciding one copy used to leave its twins in the feed (and a
	// second confirm minted a second artifact + second extractor spool,
	// because the note text is mailbox-relative). The clicked copy is decided
	// exactly as before; its fingerprint-siblings follow.
	siblings := func() []gmailsync.Candidate {
		var out []gmailsync.Candidate
		for _, twin := range s.oodaEmail.PendingGroup(gmailsync.FingerprintOf(*cand)) {
			if twin.ID != cand.ID {
				out = append(out, twin)
			}
		}
		return out
	}
	switch b.Action {
	case "dismiss":
		decided, err := s.oodaEmail.Decide(cand.ID, id.Email, false, "", false)
		if err != nil {
			httpError(w, err)
			return
		}
		for _, twin := range siblings() {
			_, _ = s.oodaEmail.Decide(twin.ID, id.Email, false, "", false)
		}
		writeJSON(w, decided)
	case "confirm":
		if s.artifacts == nil {
			http.Error(w, "artifact pool not configured", http.StatusServiceUnavailable)
			return
		}
		ref, err := s.artifacts.Save(strings.NewReader(cand.Note), cand.Filename, "text/markdown")
		if err != nil {
			httpError(w, err)
			return
		}
		if err := s.artifacts.Add("ooda", artifacts.Entry{
			Ref: ref, By: id.Name, ByEmail: strings.ToLower(id.Email),
			At: time.Now(), Thread: "email:" + cand.ThreadID,
		}); err != nil {
			httpError(w, err)
			return
		}
		// the full text is the artifact; the extract cache serves the feed's
		// preview and any later agent read without re-fetching the blob
		_ = s.artifacts.PutExtract(ref.Hash, cand.Note)
		spooled := s.SpoolOodaEmailExtract(*cand, ref.Hash)
		decided, err := s.oodaEmail.Decide(cand.ID, id.Email, true, ref.Hash, !spooled)
		if err != nil {
			httpError(w, err)
			return
		}
		// the twins confirm pointing at the SAME artifact and never spool —
		// one conversation, one artifact, one extraction
		for _, twin := range siblings() {
			_, _ = s.oodaEmail.Decide(twin.ID, id.Email, true, ref.Hash, false)
		}
		writeJSON(w, decided)
	default:
		httpError(w, errBadRequest("action must be confirm or dismiss"))
	}
}

// SpoolOodaEmailExtract hands one confirmed email note to the extractor, text
// inline. Returns false when the engine is busy (the sync loop retries) or
// unconfigured. The ooda-email ritual never reads the vault — everything it
// needs rides the request (intake.go's "data beats instructions").
func (s *Server) SpoolOodaEmailExtract(cand gmailsync.Candidate, hash string) bool {
	if s.spirits == nil || s.realestate == nil {
		return false
	}
	req := s.oodaEmailRequest(cand, hash)
	if err := s.spirits.SpoolRunNow("extractor", "ooda-email", req, ""); err != nil {
		if err != spirits.ErrAlreadyActive {
			// visible but non-fatal: the confirm stands, the spool retries
			fmt.Printf("ooda email spool: %v\n", err)
		}
		return false
	}
	return true
}

// oodaEmailRequest composes the extraction request: the note text inline,
// then the matching context (roster, properties, existing records) — the
// exact intakeRequest pattern, sized to the 60k spool cap.
func (s *Server) oodaEmailRequest(cand gmailsync.Candidate, hash string) string {
	var b strings.Builder
	b.WriteString("Extract real-estate items from ONE email thread note (text below).\n")
	b.WriteString("source: portal email artifact sha256:" + hash + " (account " + cand.Account + ")\n")
	b.WriteString("subject: " + cand.Subject + "\n")
	b.WriteString("contractor records (slug — name — scopes):\n")
	for _, c := range s.realestate.Contractors() {
		b.WriteString("- " + c.Slug + " — " + c.Name)
		if len(c.Scopes) > 0 {
			b.WriteString(" — " + strings.Join(c.Scopes, ", "))
		}
		b.WriteString("\n")
	}
	b.WriteString("active properties (slug — address):\n")
	if props, err := s.realestate.Properties(); err == nil {
		for _, p := range props {
			if p.Hidden || (p.Control != "owned" && p.Entity == "") {
				continue
			}
			b.WriteString("- " + p.Slug + " — " + p.Address + "\n")
		}
	}
	b.WriteString("existing contract records (slug — contractor — status — total — properties):\n")
	for _, c := range s.realestate.Contracts() {
		var props []string
		for _, a := range c.Allocations {
			props = append(props, a.Property)
		}
		b.WriteString(fmt.Sprintf("- %s — %s — %s — $%.0f — %s\n",
			c.Slug, c.Contractor, c.Status, c.Total, strings.Join(props, ", ")))
	}
	b.WriteString("\n--- THE NOTE (" + path.Base(cand.Filename) + ") ---\n")
	note := cand.Note
	// keep the whole request under the spool cap; the artifact keeps full text
	if budget := spirits.MaxRequestChars - b.Len() - 400; len(note) > budget && budget > 0 {
		note = "[oldest sections truncated — full text is the artifact]\n\n" + note[len(note)-budget:]
	}
	b.WriteString(note)
	return b.String()
}
