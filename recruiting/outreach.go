package recruiting

import (
	"context"
	"errors"
	"net/mail"
	"sort"
	"strconv"
	"strings"
	"time"
)

// Approval-gated outreach (Phase 5, plan §4.8). Three things live here:
//
//  1. outreach/<slug>.md — the APPEND-ONLY log. Every draft and every send is
//     one row plus a blockquoted body; nothing here rewrites a row once it
//     is on the file. The candidate record carries only a pointer row
//     (`## outreach`: log, last, status, and the Gmail ids of the last send).
//  2. GenerateDraft — a pure, deterministic template: (kind, recipient,
//     candidate, role, evidence, network path) → subject + body. No LLM.
//  3. The store actions: DraftOutreach (writes a draft; no network),
//     PrepareOutreach (readiness; writes nothing), SendOutreach (the ONE
//     path that reaches the sender — refused without Approve, refused when
//     not ready, refused when the sender is not capable).
//
// Doctrine (D5): nothing in this package sends on its own. There is no
// ticker, no queue, no retry. One approved call sends exactly the bytes the
// owner saw, once; the log row it appends is the receipt.

// Outreach kinds — direct cold mail to the candidate, a warm intro request
// to a mutual, or a referral ask to a mutual.
const (
	OutreachDirect   = "direct"
	OutreachWarm     = "warm"
	OutreachReferral = "referral"
)

var OutreachKinds = []string{OutreachDirect, OutreachWarm, OutreachReferral}

func ValidOutreachKind(s string) bool { return inSet(OutreachKinds, s) }

// Outreach statuses. `ready` and `replied` are grammar members a hand edit
// (or the reply-sync follow-on) may write; this pass writes draft and sent.
const (
	OutreachStatusDraft   = "draft"
	OutreachStatusReady   = "ready"
	OutreachStatusSent    = "sent"
	OutreachStatusReplied = "replied"
)

var OutreachStatuses = []string{OutreachStatusDraft, OutreachStatusReady, OutreachStatusSent, OutreachStatusReplied}

func ValidOutreachStatus(s string) bool { return inSet(OutreachStatuses, s) }

// ErrOutreachNotApproved: a send without approve:true.
var ErrOutreachNotApproved = errors.New("outreach: a send needs approve:true — nothing was sent")

// ErrOutreachNotReady: the readiness check failed (the result names why).
var ErrOutreachNotReady = errors.New("outreach: not ready to send")

// ErrOutreachUnconfigured: no send-capable sender is connected.
var ErrOutreachUnconfigured = errors.New("outreach: sending is not connected — connect the sender first")

var outreachEntryKeys = []string{"seq", "at", "kind", "status", "to", "via", "sender",
	"subject", "message", "thread", "sent_at", "actor"}

// OutreachEntry is one log row: a draft or a send.
type OutreachEntry struct {
	Seq       int      `json:"seq"`
	At        string   `json:"at"`
	Kind      string   `json:"kind"`
	Status    string   `json:"status"`
	To        []string `json:"to"`
	Via       string   `json:"via,omitempty"` // the mutual (network person id) for warm/referral
	Sender    string   `json:"sender"`
	Subject   string   `json:"subject"`
	Body      string   `json:"body"`
	MessageID string   `json:"messageId,omitempty"`
	ThreadID  string   `json:"threadId,omitempty"`
	SentAt    string   `json:"sentAt,omitempty"`
	Actor     string   `json:"actor,omitempty"`
	Unknown   []Field  `json:"unknown,omitempty"`
}

// OutreachDoc is outreach/<slug>.md.
type OutreachDoc struct {
	DocFM
	Slug  string
	Lines []Line
}

// OutreachLogName is the vault-relative (to the recruiting root) log path.
func OutreachLogName(slug string) string { return "outreach/" + slug + ".md" }

func outreachRecognized(r *Row) bool { return r.Has("kind") && r.Has("status") }

// ParseOutreach reads a log. Unrecognized lines round-trip verbatim.
func ParseOutreach(content string) *OutreachDoc {
	d := &OutreachDoc{}
	d.DocFM, d.Lines = parseRows(content, outreachRecognized)
	return d
}

// SerializeOutreach is the fixpoint emitter.
func SerializeOutreach(d *OutreachDoc) string { return serializeRows(d.DocFM, d.Lines) }

// NewOutreachDoc is the shell for a candidate's first draft.
func NewOutreachDoc(slug, name string) *OutreachDoc {
	d := &OutreachDoc{Slug: slug}
	d.FMBlank = true
	d.Set("id", "outreach/"+slug)
	d.Set("candidate", CandidateID(slug))
	d.Set("pii", "true")
	d.Lines = []Line{{Raw: "# outreach — " + strings.TrimSpace(name)}, {Raw: ""}}
	return d
}

// Entries collects the rows in file order.
func (d *OutreachDoc) Entries() []OutreachEntry {
	out := []OutreachEntry{}
	for _, ln := range d.Lines {
		if ln.Row == nil {
			continue
		}
		r := ln.Row
		seq, _ := strconv.Atoi(strings.TrimSpace(r.Get("seq")))
		out = append(out, OutreachEntry{
			Seq: seq, At: r.Get("at"), Kind: r.Get("kind"), Status: r.Get("status"),
			To: r.GetAll("to"), Via: r.Get("via"), Sender: r.Get("sender"),
			Subject: r.Get("subject"), Body: snippetOf(r),
			MessageID: r.Get("message"), ThreadID: r.Get("thread"),
			SentAt: r.Get("sent_at"), Actor: r.Get("actor"),
			Unknown: unknownFields(r, outreachEntryKeys...),
		})
	}
	return out
}

// Latest is the last row, if any.
func (d *OutreachDoc) Latest() (OutreachEntry, bool) {
	es := d.Entries()
	if len(es) == 0 {
		return OutreachEntry{}, false
	}
	return es[len(es)-1], true
}

// CurrentDraft is the last row when it is still a draft (or marked ready):
// once a send has been appended after it, there is no current draft.
func (d *OutreachDoc) CurrentDraft() (OutreachEntry, bool) {
	e, ok := d.Latest()
	if !ok || (e.Status != OutreachStatusDraft && e.Status != OutreachStatusReady) {
		return OutreachEntry{}, false
	}
	return e, true
}

// Append adds one row. It is the ONLY mutator: there is no method that finds
// an existing row and changes it, which is what makes the log append-only
// in code rather than by convention. Seq is assigned (max+1).
func (d *OutreachDoc) Append(e OutreachEntry) (OutreachEntry, error) {
	if !ValidOutreachKind(e.Kind) {
		return OutreachEntry{}, errf("outreach kind must be one of %s", strings.Join(OutreachKinds, ", "))
	}
	if !ValidOutreachStatus(e.Status) {
		return OutreachEntry{}, errf("outreach status must be one of %s", strings.Join(OutreachStatuses, ", "))
	}
	if strings.TrimSpace(e.Sender) == "" {
		return OutreachEntry{}, errf("an outreach row needs a sender")
	}
	if e.Status == OutreachStatusSent {
		if len(cleanRecipients(e.To)) == 0 || strings.TrimSpace(e.MessageID) == "" {
			return OutreachEntry{}, errf("a sent row needs recipients and the Gmail message id")
		}
	}
	max := 0
	for _, have := range d.Entries() {
		if have.Seq > max {
			max = have.Seq
		}
	}
	e.Seq = max + 1
	e.To = cleanRecipients(e.To)
	r := newRow("seq", strconv.Itoa(e.Seq), "at", e.At, "kind", e.Kind, "status", e.Status)
	r.SetAll("to", e.To)
	if e.Via != "" {
		r.Set("via", e.Via)
	}
	r.Set("sender", e.Sender)
	r.Set("subject", e.Subject)
	if e.MessageID != "" {
		r.Set("message", e.MessageID)
	}
	if e.ThreadID != "" {
		r.Set("thread", e.ThreadID)
	}
	if e.SentAt != "" {
		r.Set("sent_at", e.SentAt)
	}
	if e.Actor != "" {
		r.Set("actor", e.Actor)
	}
	for _, f := range e.Unknown {
		r.Fields = append(r.Fields, f)
	}
	r.Sub = bodyQuoteLines(e.Body)
	// keep one blank line between rows so the file reads as entries
	if n := len(d.Lines); n > 0 && (d.Lines[n-1].Row != nil || strings.TrimSpace(d.Lines[n-1].Raw) != "") {
		d.Lines = append(d.Lines, Line{Raw: ""})
	}
	d.Lines = append(d.Lines, Line{Row: r})
	return e, nil
}

// bodyQuoteLines renders a body as the indented blockquote beneath its row.
// A blank body line is `  >` (no trailing space) so it still reads as a
// continuation — a bare blank line would end the row.
func bodyQuoteLines(body string) []string {
	body = strings.TrimRight(strings.ReplaceAll(body, "\r\n", "\n"), "\n")
	if body == "" {
		return nil
	}
	var out []string
	for _, ln := range strings.Split(body, "\n") {
		if strings.TrimSpace(ln) == "" {
			out = append(out, "  >")
			continue
		}
		out = append(out, "  > "+ln)
	}
	return out
}

func cleanRecipients(in []string) []string {
	var out []string
	seen := map[string]bool{}
	for _, a := range in {
		for _, part := range strings.Split(a, ",") {
			part = strings.TrimSpace(part)
			key := strings.ToLower(part)
			if part == "" || seen[key] {
				continue
			}
			seen[key] = true
			out = append(out, part)
		}
	}
	return out
}

func validRecipients(to []string) error {
	if len(to) == 0 {
		return errf("no recipient — the record carries no address for this outreach")
	}
	for _, a := range to {
		if _, err := mail.ParseAddress(a); err != nil {
			return errf("recipient %q is not an email address", a)
		}
	}
	return nil
}

// ---- the draft generator ----

// DraftInput is everything a template may read. It is a value, and
// GenerateDraft reads nothing else, which is what makes the output a pure
// function of the record.
type DraftInput struct {
	Kind       string
	Recipient  string // display name of the addressee (candidate, or the mutual)
	Candidate  Candidate
	Role       Role
	Evidence   []Evidence
	Path       string // the intro path (warm/referral)
	Sender     string
	SenderName string
}

// OutreachDraft is the generated subject + body.
type OutreachDraft struct {
	Subject string `json:"subject"`
	Body    string `json:"body"`
}

// GenerateDraft renders one of the three templates. Deterministic: same
// input → identical bytes. It never invents a fact: the evidence line is
// the first citation on the record with a quote (else a url), the role is
// the record's title, the path is the one passed in.
func GenerateDraft(in DraftInput) (OutreachDraft, error) {
	if !ValidOutreachKind(in.Kind) {
		return OutreachDraft{}, errf("outreach kind must be one of %s", strings.Join(OutreachKinds, ", "))
	}
	first := firstName(in.Recipient)
	if first == "" {
		first = "there"
	}
	signer := strings.TrimSpace(in.SenderName)
	if signer == "" {
		signer = senderName(in.Sender)
	}
	roleTitle := strings.TrimSpace(in.Role.Title)
	roleLine := roleTitle
	if roleLine == "" {
		roleLine = "a role"
	}
	where := ""
	if loc := strings.TrimSpace(in.Role.Location); loc != "" {
		where = " (" + loc + ")"
	}
	cand := strings.TrimSpace(in.Candidate.Name)
	candDesc := cand
	if p := in.Candidate.Profile; p != nil {
		var bits []string
		if t := strings.TrimSpace(p["title"]); t != "" {
			bits = append(bits, t)
		}
		if o := strings.TrimSpace(p["org"]); o != "" {
			bits = append(bits, o)
		}
		if len(bits) > 0 {
			candDesc += " (" + strings.Join(bits, ", ") + ")"
		}
	}
	ev := evidenceLine(in.Evidence)
	sig := "\n\nBest,\n" + signer + "\n" + strings.TrimSpace(in.Sender) + "\n"

	switch in.Kind {
	case OutreachDirect:
		subject := roleLine + " at AION"
		if roleTitle == "" {
			subject = "A conversation about AION"
		}
		body := "Hi " + first + ",\n\n" +
			"I'm " + signer + " at AION Biosciences. "
		if ev != "" {
			body += "I came across your work — " + ev + " — and it stood out for the " + roleLine + " role we're hiring for" + where + ".\n\n"
		} else {
			body += "We're hiring a " + roleLine + where + " and your background looks like a strong fit.\n\n"
		}
		body += "Would you be open to a 20-minute call in the next couple of weeks? Happy to share more about the team and the work first if that's useful."
		return OutreachDraft{Subject: subject, Body: body + sig}, nil

	case OutreachWarm:
		subject := "Intro to " + cand + "?"
		body := "Hi " + first + ",\n\n" +
			"Quick ask: would you be willing to introduce me to " + candDesc + "? We're hiring a " + roleLine + " at AION" + where
		if ev != "" {
			body += ", and their work — " + ev + " — looks like a strong fit.\n\n"
		} else {
			body += ", and they look like a strong fit.\n\n"
		}
		if p := strings.TrimSpace(in.Path); p != "" {
			body += "The path I see is " + p + ". "
		}
		body += "Happy to send a short forwardable blurb if that's easier — and no worries at all if it's not a good ask."
		return OutreachDraft{Subject: subject, Body: body + sig}, nil

	case OutreachReferral:
		subject := "Referral ask — " + roleLine + " at AION"
		body := "Hi " + first + ",\n\n" +
			"We're hiring a " + roleLine + " at AION" + where + ". " + candDesc + " came up in our search"
		if ev != "" {
			body += " — " + ev
		}
		if p := strings.TrimSpace(in.Path); p != "" {
			body += " — and you're connected through " + p + "."
		} else {
			body += "."
		}
		body += "\n\nWould you vouch for them, or point me to anyone else you'd rate for this? A one-line reply is plenty."
		return OutreachDraft{Subject: subject, Body: body + sig}, nil
	}
	return OutreachDraft{}, errf("unhandled outreach kind %q", in.Kind)
}

// evidenceLine picks the first evidence with a quote, else the first with a
// url, and renders it as one inline fragment.
func evidenceLine(evs []Evidence) string {
	for _, ev := range evs {
		s := oneLine(ev.Snippet)
		if s == "" || strings.HasPrefix(strings.ToLower(s), "http") {
			continue // a bare url is a citation, not a quote
		}
		if u := strings.TrimSpace(ev.URL); u != "" {
			return "\"" + s + "\" (" + u + ")"
		}
		return "\"" + s + "\""
	}
	for _, ev := range evs {
		if u := strings.TrimSpace(ev.URL); u != "" {
			return u
		}
	}
	return ""
}

func oneLine(s string) string {
	s = strings.Join(strings.Fields(s), " ")
	if len(s) > 240 {
		s = strings.TrimSpace(s[:240]) + "…"
	}
	return s
}

func firstName(name string) string {
	f := strings.Fields(strings.TrimSpace(name))
	if len(f) == 0 {
		return ""
	}
	return f[0]
}

// senderName is the sign-off for a sender address: the local part,
// capitalized ("ben@aion.bio" → "Ben").
func senderName(sender string) string {
	local := strings.TrimSpace(sender)
	if i := strings.Index(local, "@"); i >= 0 {
		local = local[:i]
	}
	if local == "" {
		return "AION"
	}
	return strings.ToUpper(local[:1]) + local[1:]
}

// ---- the sender boundary ----

// OutreachSender is the one-method-shaped surface the store sends through.
// The recruiting package does not import gmailsend; main binds the adapter.
// A nil sender is the unconfigured posture: drafts work, sends refuse.
type OutreachSender interface {
	Sender() string
	SendCapable() bool
	Send(ctx context.Context, msg OutreachMessage) (OutreachSendRef, error)
}

// OutreachMessage is what crosses the boundary: recipients, subject, body.
type OutreachMessage struct {
	To      []string
	Subject string
	Body    string
}

// OutreachSendRef is what comes back: Gmail's message and thread ids.
type OutreachSendRef struct {
	MessageID string
	ThreadID  string
}

// ---- store actions ----

// OutreachDraftRequest is the body of POST …/outreach/draft/{id}. Empty
// subject/body → generated; either supplied → captured as written (the
// owner's edit). Empty To → resolved from the record (direct: the profile
// email; warm/referral: the mutual's email) — never guessed.
type OutreachDraftRequest struct {
	Kind    string   `json:"kind"`
	To      []string `json:"to"`
	Via     string   `json:"via"`
	Subject string   `json:"subject"`
	Body    string   `json:"body"`
	Actor   string   `json:"actor"`
}

// OutreachReadiness is the prepare/preflight answer and the shape a refused
// send carries: what would go, and every reason it cannot yet.
type OutreachReadiness struct {
	Candidate   string         `json:"candidate"`
	Ready       bool           `json:"ready"`
	Reasons     []string       `json:"reasons"`
	Draft       *OutreachEntry `json:"draft,omitempty"`
	Gate        GateState      `json:"gate"`
	Evidence    int            `json:"evidence"`
	SendCapable bool           `json:"sendCapable"`
	Sender      string         `json:"sender,omitempty"`
}

// OutreachSendRequest is the body of POST …/outreach/send/{id}. Subject and
// Body, when non-empty, are the owner's final edit and are what is sent and
// logged; the draft row stays as it was (append-only).
type OutreachSendRequest struct {
	Approve bool   `json:"approve"`
	Subject string `json:"subject"`
	Body    string `json:"body"`
	Actor   string `json:"actor"`
}

// OutreachSendResult is what an approved send did.
type OutreachSendResult struct {
	Readiness OutreachReadiness `json:"readiness"`
	Entry     OutreachEntry     `json:"entry"`
	MessageID string            `json:"messageId"`
	ThreadID  string            `json:"threadId"`
	Record    Candidate         `json:"record"`
}

func (s *Store) loadOutreach(slug string) *OutreachDoc {
	d := ParseOutreach(s.raw(OutreachLogName(slug)))
	d.Slug = slug
	return d
}

func (s *Store) saveOutreach(slug string, d *OutreachDoc) error {
	return s.save(OutreachLogName(slug), SerializeOutreach(d))
}

// Outreach returns a candidate's log entries (the GET route).
func (s *Store) Outreach(id string) ([]OutreachEntry, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	slug, _, err := s.resolve(id)
	if err != nil {
		return nil, err
	}
	return s.loadOutreach(slug).Entries(), nil
}

// DraftOutreach appends a draft row. No network is reached.
func (s *Store) DraftOutreach(id string, req OutreachDraftRequest, sender string, now time.Time) (OutreachEntry, Candidate, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	slug, doc, err := s.resolve(id)
	if err != nil {
		return OutreachEntry{}, Candidate{}, err
	}
	if doc.Get("stage") == StageArchived {
		return OutreachEntry{}, Candidate{}, errf("restore %s before drafting outreach", id)
	}
	kind := strings.ToLower(strings.TrimSpace(req.Kind))
	if kind == "" {
		kind = OutreachDirect
	}
	if !ValidOutreachKind(kind) {
		return OutreachEntry{}, Candidate{}, errf("outreach kind must be one of %s", strings.Join(OutreachKinds, ", "))
	}
	sender = strings.TrimSpace(sender)
	if sender == "" {
		return OutreachEntry{}, Candidate{}, errf("outreach needs a sender address")
	}
	c := s.candidateView(slug, doc)

	// the addressee and the recipients
	var via NetworkPerson
	recipient := c.Name
	if kind != OutreachDirect {
		vid := strings.TrimSpace(req.Via)
		if vid == "" {
			return OutreachEntry{}, Candidate{}, errf("a %s outreach needs the mutual (via) it goes through", kind)
		}
		p, ok := s.findPerson(vid)
		if !ok {
			return OutreachEntry{}, Candidate{}, errf("unknown network person %q", vid)
		}
		via = p
		recipient = p.Name
	}
	to := cleanRecipients(req.To)
	if len(to) == 0 {
		switch kind {
		case OutreachDirect:
			to = cleanRecipients([]string{c.Profile["email"]})
		default:
			to = cleanRecipients([]string{via.Email})
		}
	}

	subject, body := strings.TrimSpace(req.Subject), strings.TrimRight(req.Body, "\n")
	if subject == "" || strings.TrimSpace(body) == "" {
		gen, err := GenerateDraft(DraftInput{
			Kind: kind, Recipient: recipient, Candidate: c, Role: s.roleView(c.Role),
			Evidence: c.Evidence, Path: pathThrough(c.Paths, via.ID), Sender: sender,
		})
		if err != nil {
			return OutreachEntry{}, Candidate{}, err
		}
		if subject == "" {
			subject = gen.Subject
		}
		if strings.TrimSpace(body) == "" {
			body = gen.Body
		}
	}

	log := s.loadOutreach(slug)
	if len(log.Lines) == 0 {
		log = NewOutreachDoc(slug, c.Name)
	}
	entry, err := log.Append(OutreachEntry{
		At: now.UTC().Format("2006-01-02"), Kind: kind, Status: OutreachStatusDraft,
		To: to, Via: via.ID, Sender: sender, Subject: subject, Body: body, Actor: strings.TrimSpace(req.Actor),
	})
	if err != nil {
		return OutreachEntry{}, Candidate{}, err
	}
	if err := s.saveOutreach(slug, log); err != nil {
		return OutreachEntry{}, Candidate{}, err
	}
	doc.SetOutreach(OutreachRef{Log: OutreachLogName(slug), Last: entry.At, Status: OutreachStatusDraft})
	if err := s.SaveCandidate(slug, doc); err != nil {
		return OutreachEntry{}, Candidate{}, err
	}
	return entry, s.candidateView(slug, doc), nil
}

// PrepareOutreach is the preflight: it computes readiness and writes nothing.
func (s *Store) PrepareOutreach(id string, sender OutreachSender) (OutreachReadiness, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	slug, doc, err := s.resolve(id)
	if err != nil {
		return OutreachReadiness{}, err
	}
	return s.readiness(slug, doc, sender), nil
}

func (s *Store) readiness(slug string, doc *CandidateDoc, sender OutreachSender) OutreachReadiness {
	c := s.candidateView(slug, doc)
	r := OutreachReadiness{Candidate: c.ID, Reasons: []string{}, Gate: c.Gate, Evidence: len(c.Evidence)}
	if sender != nil {
		r.Sender = sender.Sender()
		r.SendCapable = sender.SendCapable()
	}
	if !r.SendCapable {
		r.Reasons = append(r.Reasons, "sending is not connected")
	}
	if c.Stage == StageArchived {
		r.Reasons = append(r.Reasons, "candidate is archived")
	}
	draft, ok := s.loadOutreach(slug).CurrentDraft()
	if !ok {
		r.Reasons = append(r.Reasons, "no draft")
	} else {
		d := draft
		r.Draft = &d
		if err := validRecipients(draft.To); err != nil {
			r.Reasons = append(r.Reasons, err.Error())
		}
		if strings.TrimSpace(draft.Subject) == "" || strings.TrimSpace(draft.Body) == "" {
			r.Reasons = append(r.Reasons, "draft has no subject or body")
		}
	}
	if len(c.Evidence) == 0 {
		r.Reasons = append(r.Reasons, "no evidence on the record")
	}
	if !c.Gate.Passed {
		r.Reasons = append(r.Reasons, "fit gate: "+c.Gate.Reason)
	}
	r.Ready = len(r.Reasons) == 0
	return r
}

// SendOutreach is the approved send. It refuses — in this order, and before
// any network call — when no capable sender is connected, when the request
// does not carry approve:true, and when readiness fails. On success it
// appends the sent row (with Gmail's ids), updates the candidate's pointer
// row, and moves the candidate to `outreach` unless it is already past it.
// The store lock is held across the send so a double click cannot send
// twice: the second call sees a log whose last row is `sent`, not a draft.
func (s *Store) SendOutreach(ctx context.Context, id string, req OutreachSendRequest, sender OutreachSender, now time.Time) (OutreachSendResult, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	slug, doc, err := s.resolve(id)
	if err != nil {
		return OutreachSendResult{}, err
	}
	res := OutreachSendResult{Readiness: s.readiness(slug, doc, sender)}
	if sender == nil || !sender.SendCapable() {
		return res, ErrOutreachUnconfigured
	}
	if !req.Approve {
		return res, ErrOutreachNotApproved
	}
	if !res.Readiness.Ready {
		return res, errf("%w: %s", ErrOutreachNotReady, strings.Join(res.Readiness.Reasons, "; "))
	}
	draft := *res.Readiness.Draft
	subject, body := draft.Subject, draft.Body
	if v := strings.TrimSpace(req.Subject); v != "" {
		subject = v
	}
	if v := strings.TrimRight(req.Body, "\n"); strings.TrimSpace(v) != "" {
		body = v
	}

	ref, err := sender.Send(ctx, OutreachMessage{To: draft.To, Subject: subject, Body: body})
	if err != nil {
		return res, err
	}

	stamp := now.UTC().Format(time.RFC3339)
	log := s.loadOutreach(slug)
	entry, err := log.Append(OutreachEntry{
		At: now.UTC().Format("2006-01-02"), Kind: draft.Kind, Status: OutreachStatusSent,
		To: draft.To, Via: draft.Via, Sender: draft.Sender, Subject: subject, Body: body,
		MessageID: ref.MessageID, ThreadID: ref.ThreadID, SentAt: stamp, Actor: strings.TrimSpace(req.Actor),
	})
	if err != nil {
		return res, err
	}
	if err := s.saveOutreach(slug, log); err != nil {
		return res, err
	}
	doc.SetOutreach(OutreachRef{Log: OutreachLogName(slug), Last: entry.At, Status: OutreachStatusSent,
		MessageID: ref.MessageID, ThreadID: ref.ThreadID})
	if stageBefore(doc.Get("stage"), StageOutreach) {
		doc.Set("stage", StageOutreach)
	}
	if err := s.SaveCandidate(slug, doc); err != nil {
		return res, err
	}
	res.Entry = entry
	res.MessageID, res.ThreadID = ref.MessageID, ref.ThreadID
	res.Record = s.candidateView(slug, doc)
	return res, nil
}

// stageBefore reports whether stage sits earlier on the board than target
// (an empty stage is `new`). Archived is never "before" anything.
func stageBefore(stage, target string) bool {
	if stage == "" {
		stage = StageNew
	}
	if stage == StageArchived {
		return false
	}
	order := map[string]int{}
	for i, st := range Stages {
		order[st] = i
	}
	a, ok := order[stage]
	b, ok2 := order[target]
	return ok && ok2 && a < b
}

// findPerson resolves a network person by id (or, failing that, by name).
func (s *Store) findPerson(ref string) (NetworkPerson, bool) {
	ref = strings.TrimSpace(ref)
	people := s.LoadNetworkPeople().People()
	for _, p := range people {
		if strings.EqualFold(strings.TrimSpace(p.ID), ref) {
			return p, true
		}
	}
	for _, p := range people {
		if strings.EqualFold(strings.TrimSpace(p.Name), ref) {
			return p, true
		}
	}
	return NetworkPerson{}, false
}

// roleView is the role projection for a candidate's tether (zero when none).
func (s *Store) roleView(role string) Role {
	d := s.roleDocFor(role)
	if d == nil {
		return Role{}
	}
	return d.View(roleKey(role), 0)
}

// pathThrough picks the intro path that runs through the mutual, else the
// first path. Paths are already ranked, so "first" is "best".
func pathThrough(paths []PathClaim, via string) string {
	via = strings.TrimSpace(via)
	if via != "" {
		for _, p := range paths {
			for _, hop := range strings.Split(p.Path, pathSep) {
				if strings.EqualFold(strings.TrimSpace(hop), via) {
					return p.Path
				}
			}
		}
	}
	if len(paths) > 0 {
		return paths[0].Path
	}
	return ""
}

// OutreachPeople lists the network people a warm/referral draft may go
// through: everyone with an address, sorted by name — the inspector's `via`
// picker reads it. A person without an email is listed too (the owner may
// type the address), flagged by the empty Email.
func (s *Store) OutreachPeople() []NetworkPerson {
	people := s.LoadNetworkPeople().People()
	out := make([]NetworkPerson, 0, len(people))
	for _, p := range people {
		if strings.HasPrefix(strings.TrimSpace(p.ID), "cand/") {
			continue
		}
		out = append(out, p)
	}
	sort.SliceStable(out, func(i, j int) bool { return strings.ToLower(out[i].Name) < strings.ToLower(out[j].Name) })
	return out
}
