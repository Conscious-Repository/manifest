package server

import (
	"fmt"
	"net/http"
	"path"
	"strconv"
	"strings"
	"time"

	"manifest/realestate"
	"manifest/teamportal"
)

// The owner's side of the OODA bid lane (ooda-portal plan, Stage C).
//
// A partner files a bid in the portal; it lands as a `kind:"bid"` proposal in
// the derived team store and cards into the FEED. NOTHING has touched the
// vault. These two COCKPIT routes are the owner action that closes the loop:
// accepting a bid crosses vaultwriter under `re-contracts` and mints the real
// contract record, which then appears in the portal on the next revision poll,
// promoted from pending to real.
//
// The trust boundary is exactly here: a partner can propose money, only the
// owner can commit it.

// UseOodaBids wires the cockpit's view of the OODA team store.
func (s *Server) UseOodaBids(store *teamportal.Store, adminEmail string) {
	s.oodaTeam, s.oodaAdmin = store, adminEmail
}

// handleOodaBidsList — GET /api/realestate/ooda-bids: the pending bids the
// owner has not yet decided, newest first.
func (s *Server) handleOodaBidsList(w http.ResponseWriter, r *http.Request) {
	if s.oodaTeam == nil {
		writeJSON(w, map[string]any{"bids": []any{}, "enabled": false})
		return
	}
	out := []map[string]any{}
	for _, p := range s.oodaTeam.Ext().Proposals {
		if p.Kind != "bid" || p.Status != "pending" {
			continue
		}
		row := map[string]any{
			"id": p.ID, "title": p.Title, "by": p.ProposedName, "byEmail": p.ProposedBy,
			"at": p.At.Format(time.RFC3339),
		}
		for k, v := range p.Payload {
			row[k] = v
		}
		out = append(out, row)
	}
	writeJSON(w, map[string]any{"bids": out, "enabled": true})
}

// handleOodaBidDecide — POST /api/realestate/ooda-bids/{id}: `{"accept":true}`
// materializes the contract record and marks the proposal approved;
// `{"accept":false}` rejects it. THE contract write is an owner action through
// the cockpit — never the portal.
func (s *Server) handleOodaBidDecide(w http.ResponseWriter, r *http.Request) {
	if s.oodaTeam == nil || s.realestate == nil || s.vault == nil {
		http.Error(w, "ooda bids not available", http.StatusServiceUnavailable)
		return
	}
	var b struct {
		Accept bool   `json:"accept"`
		Status string `json:"status"` // contract status to mint with ("" → proposed)
	}
	if err := decode(r, &b); err != nil {
		httpError(w, err)
		return
	}
	id := r.PathValue("id")
	var bid *teamportal.Proposal
	for _, p := range s.oodaTeam.Ext().Proposals {
		if p.ID == id && p.Kind == "bid" {
			p := p
			bid = &p
			break
		}
	}
	if bid == nil {
		http.Error(w, "bid not found", http.StatusNotFound)
		return
	}
	if bid.Status != "pending" {
		httpError(w, errBadRequest("this bid was already "+bid.Status))
		return
	}
	owner := teamportal.Identity{Email: s.oodaAdmin, Name: "Benjamin"}

	if !b.Accept {
		if _, err := s.oodaTeam.Decide(owner, bid.ID, false, time.Now()); err != nil {
			httpError(w, err)
			return
		}
		writeJSON(w, map[string]any{"ok": true, "accepted": false})
		return
	}

	slug, err := s.materializeOodaBid(bid, b.Status)
	if err != nil {
		httpError(w, err)
		return
	}
	// Only after the record exists does the proposal close — if the write
	// fails the bid stays pending and can be retried, rather than vanishing.
	if _, err := s.oodaTeam.Decide(owner, bid.ID, true, time.Now()); err != nil {
		httpError(w, fmt.Errorf("contract %s was written but the bid could not be closed: %w", slug, err))
		return
	}
	writeJSON(w, map[string]any{"ok": true, "accepted": true, "contract": slug})
}

// materializeOodaBid turns a bid payload into a real contract record through
// the SAME validated path the cockpit's own contract form uses.
func (s *Server) materializeOodaBid(bid *teamportal.Proposal, status string) (string, error) {
	str := func(k string) string { v, _ := bid.Payload[k].(string); return strings.TrimSpace(v) }
	amount, _ := bid.Payload["amount"].(float64)
	property, contractor := str("property"), str("contractor")
	if property == "" || contractor == "" || amount <= 0 {
		return "", errBadRequest("this bid is missing a property, a contractor, or an amount")
	}
	if _, ok := s.realestate.Get(property); !ok {
		return "", errBadRequest("unknown property " + property)
	}
	node := str("workId")
	if node == "" {
		// a bid filed against no rock still needs a target for its allocation;
		// the property's first rock is the honest default and the owner can
		// re-point it in the contracts editor.
		if p, ok := s.realestate.Get(property); ok && len(p.Work) > 0 {
			node = p.Work[0].ID
		}
	}
	if status == "" {
		status = "proposed" // accepting the BID is not accepting the CONTRACT
	}
	// A partner naming a contractor manifest has never seen must not be a dead
	// end: accepting the bid is the owner action that mints the counterparty
	// record too, the same quiet create-inline the cockpit's own autocomplete
	// offers. Without this the owner gets "create it first" and has to leave
	// the screen to obey.
	if err := s.ensureContractorRecord(contractor); err != nil {
		return "", err
	}
	body := contractBody{
		Name:       contractor + " — " + str("scope"),
		Contractor: slugify(contractor),
		Status:     status,
		Total:      amount,
		Date:       orToday(str("date")),
		Expires:    str("expires"),
		Allocations: []string{
			property + " | " + node + " | " + strconv.FormatFloat(amount, 'f', -1, 64),
		},
	}
	if str("scope") == "" {
		body.Name = contractor + " — " + property
	}
	c, err := s.validateContract(&body)
	if err != nil {
		return "", err
	}
	if c.Name == "" {
		c.Name = c.Contractor
	}
	base := slugify(c.Name)
	slug := base
	for n := 2; ; n++ {
		if _, exists := s.realestate.GetContract(slug); !exists {
			break
		}
		slug = base + "-" + itoa(n)
	}
	rel := path.Join(s.realestateRootOr(), "contracts", slug+".md")
	if err := s.vault.WriteCap("re-contracts", rel, []byte(realestate.NewContractRecord(c))); err != nil {
		return "", err
	}
	if s.index != nil {
		_ = s.index.ReindexPaths([]string{rel})
	}
	return slug, nil
}

func orToday(v string) string {
	if strings.TrimSpace(v) != "" {
		return v
	}
	return time.Now().Format("2006-01-02")
}

// ensureContractorRecord creates system/realestate/contractors/<slug>.md when
// the bid names someone with no record yet. No-op when one exists.
func (s *Server) ensureContractorRecord(name string) error {
	slug := slugify(name)
	for _, c := range s.realestate.Contractors() {
		if strings.EqualFold(c.Slug, slug) {
			return nil
		}
	}
	rel := path.Join(s.realestateRootOr(), "contractors", slug+".md")
	content := "---\ncategories: [contractor]\nname: " + strings.TrimSpace(name) +
		"\n---\n\n# " + strings.TrimSpace(name) + "\n\nCreated when a portal bid named this counterparty.\n"
	if _, err := s.vault.CreateRecord(rel, content); err != nil {
		return err
	}
	if s.index != nil {
		_ = s.index.ReindexPaths([]string{rel})
	}
	return nil
}
