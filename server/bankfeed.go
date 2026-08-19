package server

import (
	"context"
	"crypto/sha1"
	"encoding/hex"
	"fmt"
	"log"
	"net/http"
	"strings"
	"time"

	"manifest/bankfeed"
	"manifest/portals"
	"manifest/realestate"
	"manifest/vaultwriter"
)

// BANK FEEDS (bank-accounts-integration plan, 2026-08-19): linked SimpleFIN
// accounts replace manual CSV statement uploads. An account is attached to
// its owning ENTITY (the vault entity record holds the owner-facing binding;
// dataDir holds the machine linkage + access URL); synced transactions flow
// through the SAME StatementStore.Ingest as the CSV path with the entity
// pre-set; confident matches against committed work auto-apply (§6) as actor
// bank-feed; everything else waits for the owner in the $ tab.

// UseBankFeed wires the feed service (nil-safe — surfaces hide when absent).
func (s *Server) UseBankFeed(b *bankfeed.Service) { s.bankFeed = b }

func (s *Server) bankfeedOK(w http.ResponseWriter) bool {
	if s.bankFeed == nil || s.realestate == nil || s.vault == nil || s.reImport == nil || s.statements == nil {
		http.Error(w, "bank feed not available", http.StatusServiceUnavailable)
		return false
	}
	return true
}

// handleBankfeedClaim exchanges the one-time setup token for the access URL
// (stored 0600 in dataDir — this box only) and returns the account list.
func (s *Server) handleBankfeedClaim(w http.ResponseWriter, r *http.Request) {
	if !s.bankfeedOK(w) {
		return
	}
	var b struct {
		Token string `json:"token"`
	}
	if err := decode(r, &b); err != nil || strings.TrimSpace(b.Token) == "" {
		httpError(w, errBadRequest("setup token is required"))
		return
	}
	if err := s.bankFeed.Claim(r.Context(), b.Token); err != nil {
		httpError(w, fmt.Errorf("claim failed: %w", err))
		return
	}
	s.writeBankfeedAccounts(w, r.Context())
}

// handleBankfeedAccounts lists bridge accounts merged with their links.
func (s *Server) handleBankfeedAccounts(w http.ResponseWriter, r *http.Request) {
	if !s.bankfeedOK(w) {
		return
	}
	if !s.bankFeed.Claimed() {
		writeJSON(w, map[string]any{"claimed": false, "accounts": []any{}})
		return
	}
	s.writeBankfeedAccounts(w, r.Context())
}

func (s *Server) writeBankfeedAccounts(w http.ResponseWriter, ctx context.Context) {
	accounts, err := s.bankFeed.Accounts(ctx)
	if err != nil {
		httpError(w, err)
		return
	}
	type row struct {
		bankfeed.Account
		Link *bankfeed.Link `json:"link,omitempty"`
	}
	out := make([]row, 0, len(accounts))
	for _, a := range accounts {
		rr := row{Account: a}
		if l, ok := s.bankFeed.Store().LinkFor(a.ID); ok {
			l := l
			rr.Link = &l
		}
		out = append(out, rr)
	}
	writeJSON(w, map[string]any{"claimed": true, "accounts": out})
}

// handleBankfeedLink binds one bridge account to an entity's account row
// (empty entitySlug unlinks). The vault entity record's state flip is the
// CLIENT's user-action entity save — only dataDir state is written here.
func (s *Server) handleBankfeedLink(w http.ResponseWriter, r *http.Request) {
	if !s.bankfeedOK(w) {
		return
	}
	id := r.PathValue("id")
	var b struct {
		EntitySlug      string `json:"entitySlug"`
		AccountLabel    string `json:"accountLabel"`
		DefaultProperty string `json:"defaultProperty"`
		Enabled         bool   `json:"enabled"`
		OrgName         string `json:"orgName"`
		AccountName     string `json:"accountName"`
	}
	if err := decode(r, &b); err != nil || id == "" {
		httpError(w, errBadRequest("account id is required"))
		return
	}
	if b.EntitySlug != "" && strings.TrimSpace(b.AccountLabel) == "" {
		httpError(w, errBadRequest("an account label is required (names the entity's account row)"))
		return
	}
	err := s.bankFeed.Store().Upsert(bankfeed.Link{
		SimplefinID: id, EntitySlug: strings.TrimSpace(b.EntitySlug),
		AccountLabel: strings.TrimSpace(b.AccountLabel), DefaultProperty: strings.TrimSpace(b.DefaultProperty),
		Enabled: b.Enabled, OrgName: b.OrgName, AccountName: b.AccountName,
	})
	if err != nil {
		httpError(w, err)
		return
	}
	writeJSON(w, map[string]any{"ok": true})
}

// handleBankfeedSync is the SETTINGS "sync now" button.
func (s *Server) handleBankfeedSync(w http.ResponseWriter, r *http.Request) {
	if !s.bankfeedOK(w) {
		return
	}
	added, applied, err := s.bankFeedSync(r.Context())
	if err != nil {
		httpError(w, err)
		return
	}
	writeJSON(w, map[string]any{"added": added, "autoApplied": applied, "links": s.bankFeed.Store().Links()})
}

// StartBankFeed launches the daily poll ticker (boot poll after a short warmup;
// portals Start idiom). Call once from main; no-op when the feed is unwired.
func (s *Server) StartBankFeed(ctx context.Context) {
	if s.bankFeed == nil {
		return
	}
	go func() {
		select {
		case <-ctx.Done():
			return
		case <-time.After(30 * time.Second): // let the index warm before the boot poll
		}
		if added, applied, err := s.bankFeedSync(ctx); err == nil && (added > 0 || applied > 0) {
			log.Printf("bankfeed: boot sync — %d new row(s), %d auto-applied", added, applied)
		}
		t := time.NewTicker(24 * time.Hour)
		defer t.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-t.C:
				if added, applied, err := s.bankFeedSync(ctx); err != nil {
					log.Printf("bankfeed: sync failed: %v", err)
				} else if added > 0 || applied > 0 {
					log.Printf("bankfeed: %d new row(s), %d auto-applied", added, applied)
				}
			}
		}
	}()
}

// bankFeedSync pulls every enabled link, ingests through the statement
// workbench (same dedupe + vendor prefill as the CSV path), auto-applies
// confident matches, and files one FEED digest per non-empty sync.
func (s *Server) bankFeedSync(ctx context.Context) (added, autoApplied int, err error) {
	if !s.bankFeed.Claimed() {
		return 0, 0, errBadRequest("no bank feed claimed yet")
	}
	s.bankfeedMu.Lock()
	defer s.bankfeedMu.Unlock()
	hauls := s.bankFeed.FetchNew(ctx, time.Now())
	if len(hauls) == 0 {
		return 0, 0, nil
	}
	// every ledger line across the portfolio — no double entry, ever (mirrors
	// handleStatementsIngest)
	ledgerKeys := map[string]bool{}
	props, _ := s.realestate.Properties()
	for _, p := range props {
		for _, lr := range p.Ledger {
			ledgerKeys[realestate.DedupeKey(lr.Date, lr.Amount, lr.Vendor)] = true
		}
	}
	_, vendorCat, vendorProp := s.reImport.Lookup("")
	var receipts []string
	for _, haul := range hauls {
		label := haul.Link.EntitySlug + ":" + haul.Link.AccountLabel
		rows := make([]realestate.StatementRow, 0, len(haul.Txns))
		for _, t := range haul.Txns {
			if t.Amount == 0 {
				continue
			}
			// SimpleFIN sign: negative = money out (expense), positive = deposit
			amt, inflow := -t.Amount, false
			if amt < 0 {
				amt, inflow = -amt, true
			}
			vendor := strings.TrimSpace(t.Payee)
			if vendor == "" {
				vendor = strings.TrimSpace(t.Description)
			}
			row := realestate.StatementRow{
				Date: t.Posted.Format("2006-01-02"), Vendor: vendor,
				Note: strings.TrimSpace(t.Description), Amount: amt, Inflow: inflow,
				Entity: haul.Link.EntitySlug, Source: "feed",
			}
			// the link's default property prefills ONE overridable assignment —
			// it never auto-applies by itself (vendor memory may still win inside
			// Ingest, same as the CSV path)
			if haul.Link.DefaultProperty != "" {
				row.Assignments = []realestate.Alloc{{Slug: haul.Link.DefaultProperty, Amount: amt}}
			}
			rows = append(rows, row)
		}
		a, _ := s.statements.Ingest(label, rows, ledgerKeys, vendorCat, vendorProp)
		added += a
		if a == 0 {
			continue
		}
		applied := s.bankAutoApply(haul.Link, vendorProp, &receipts)
		autoApplied += applied
	}
	if added > 0 || autoApplied > 0 {
		s.bankFeedDigest(added, autoApplied, receipts)
	}
	return added, autoApplied, nil
}

// bankAutoApply reconciles this link's freshly-synced rows that PROVABLY
// match committed work (§6): on the candidate property (link default, else
// vendor memory), a DONE node with an accepted-contract slice whose remaining
// draw equals the row amount ±1% — contractor equal-fold match when the
// contract names one. Legacy fallback (zero-allocation properties): accepted
// BID rows, mirroring JoinWorkLedger. The write carries [work::]+[contract::]
// so it draws the contract down and closes the Unreconciled gap. Everything
// else stays in the $ tab.
func (s *Server) bankAutoApply(link bankfeed.Link, vendorProp map[string]string, receipts *[]string) int {
	list, _ := s.statements.List()
	applied := 0
	for _, row := range list {
		if row.Source != "feed" || row.Inflow || row.Entity != link.EntitySlug {
			continue
		}
		// machine-prefilled rows only — anything the owner touched stays manual
		if row.State != "pending" && !(row.State == "assigned" && row.Remembered) {
			continue
		}
		if strings.TrimSpace(row.Category) == "" {
			continue // Applicable requires a category; no guessing
		}
		slug := link.DefaultProperty
		if slug == "" {
			slug = vendorProp[strings.ToLower(strings.TrimSpace(row.Vendor))]
		}
		if slug == "" {
			continue
		}
		alloc, ok := s.bankMatchWork(slug, row)
		if !ok {
			continue
		}
		allocs := []realestate.Alloc{alloc}
		if _, err := s.statements.Update(row.ID, nil, nil, &allocs, nil, nil); err != nil {
			continue
		}
		rows, err := s.statements.Applicable([]string{row.ID})
		if err != nil {
			continue
		}
		if _, _, err := s.applyStatementRows(rows, vaultwriter.ActorBankFeed); err != nil {
			log.Printf("bankfeed: auto-apply write failed for %s: %v", row.ID, err)
			continue
		}
		s.statements.MarkApplied([]string{row.ID})
		applied++
		*receipts = append(*receipts, fmt.Sprintf("$%s · %s → %s / %s ✓ reconciled",
			moneyStr(row.Amount), row.Vendor, slug, alloc.WorkID))
	}
	return applied
}

// bankMatchWork finds the one done node whose committed-but-unpaid money
// matches the row. Ambiguity (two matches) fails closed.
func (s *Server) bankMatchWork(slug string, row realestate.StatementRow) (realestate.Alloc, bool) {
	p, ok := s.realestate.Get(slug)
	if !ok {
		return realestate.Alloc{}, false
	}
	// legacy detection mirrors JoinWorkLedger: zero contract allocations →
	// accepted-bid rows carry the committed money
	hasAllocs := false
	realestate.WalkNodes(p.Work, func(_ *realestate.WorkStage, n *realestate.WorkNode) {
		if len(n.Contracts) > 0 {
			hasAllocs = true
		}
	})
	var match realestate.Alloc
	found := 0
	within := func(remaining float64) bool {
		diff := remaining - row.Amount
		if diff < 0 {
			diff = -diff
		}
		return diff <= 0.01*row.Amount
	}
	// contractor names come in two shapes: the record's slug and the bank's
	// payee string — slugify both sides so "Olga Sobkiv" matches "olga-sobkiv"
	sameParty := func(a, b string) bool {
		return strings.EqualFold(strings.TrimSpace(a), strings.TrimSpace(b)) || slugify(a) == slugify(b)
	}
	realestate.WalkNodes(p.Work, func(_ *realestate.WorkStage, n *realestate.WorkNode) {
		if n.Task == nil || !n.Task.Checked || n.Decision {
			return
		}
		if hasAllocs {
			for _, c := range n.Contracts {
				drawn := 0.0
				for _, lr := range p.Ledger {
					if strings.EqualFold(lr.Type, "expense") && lr.WorkID == n.ID && strings.EqualFold(lr.Contract, c.Slug) {
						drawn += lr.Amount
					}
				}
				if !within(c.Amount - drawn) {
					continue
				}
				if c.Contractor != "" && !sameParty(c.Contractor, row.Vendor) {
					continue
				}
				match = realestate.Alloc{Slug: slug, Amount: row.Amount, WorkID: n.ID, Contract: c.Slug}
				found++
			}
			return
		}
		for _, b := range n.Bids {
			if !strings.EqualFold(b.Status, "accepted") {
				continue
			}
			paid := 0.0
			for _, lr := range p.Ledger {
				if strings.EqualFold(lr.Type, "expense") && lr.WorkID == n.ID {
					paid += lr.Amount
				}
			}
			if !within(b.Amount - paid) {
				continue
			}
			if b.Who != "" && !sameParty(b.Who, row.Vendor) {
				continue
			}
			match = realestate.Alloc{Slug: slug, Amount: row.Amount, WorkID: n.ID}
			found++
		}
	})
	return match, found == 1
}

// bankFeedDigest files one FEED card for a non-empty sync.
func (s *Server) bankFeedDigest(added, autoApplied int, receipts []string) {
	title := fmt.Sprintf("Bank feed · %d new row(s)", added)
	if autoApplied > 0 {
		title += fmt.Sprintf(" · %d reconciled", autoApplied)
	}
	detail := "Review in PROPERTIES → $"
	if len(receipts) > 0 {
		detail = strings.Join(receipts, "\n")
	}
	h := sha1.Sum([]byte(title + "|" + detail + "|" + time.Now().Format("2006-01-02T15")))
	s.bankFeed.Store().AddDigest(bankfeed.Digest{
		ID: "bank:" + hex.EncodeToString(h[:6]), Title: title, Detail: detail,
		Date: time.Now().Format(time.RFC3339),
	})
}

// bankFeedCards adapts digest entries into feed cards (portals card slice).
func (s *Server) bankFeedCards() []portals.Card {
	if s.bankFeed == nil {
		return nil
	}
	today := time.Now().Format("2006-01-02")
	var cards []portals.Card
	for _, d := range s.bankFeed.Store().Digests() {
		cards = append(cards, portals.Card{
			ID: d.ID, Type: "portal-item", Portal: "bank",
			Title: d.Title, Detail: d.Detail, Date: d.Date,
			Pinned: strings.HasPrefix(d.Date, today),
		})
	}
	return cards
}

// moneyStr renders 5500 as "5,500" (digest receipts).
func moneyStr(v float64) string {
	s := fmt.Sprintf("%.0f", v)
	if v != float64(int64(v)) {
		s = fmt.Sprintf("%.2f", v)
	}
	whole, frac, _ := strings.Cut(s, ".")
	for i := len(whole) - 3; i > 0; i -= 3 {
		whole = whole[:i] + "," + whole[i:]
	}
	if frac != "" {
		return whole + "." + frac
	}
	return whole
}
