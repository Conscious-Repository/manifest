package server

import (
	"context"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"manifest/bankfeed"
	"manifest/realestate"
	"manifest/record"
	"manifest/vaultindex"
	"manifest/vaultwriter"
)

// stubBridge implements bankfeed.Provider in-process (the §9.1 fake). It
// counts bridge requests (the SimpleFIN budget is requests/day) and can hold
// a Transactions call open so a second sync provably queues behind it.
type stubBridge struct {
	txns    map[string][]bankfeed.Txn
	notices []string // the bridge's advisory `errors` array

	acctCalls atomic.Int32
	txnCalls  atomic.Int32
	started   chan struct{} // receives once per Transactions call (when set)
	block     chan struct{} // Transactions waits for close (when set)
}

func (s *stubBridge) Claim(_ context.Context, _ string) (string, error) { return "stub://access", nil }
func (s *stubBridge) Accounts(_ context.Context, _ string) ([]bankfeed.Account, error) {
	s.acctCalls.Add(1)
	return []bankfeed.Account{{ID: "act-1", Name: "Checking ····4821", Org: "Midwest Bank"}}, nil
}
func (s *stubBridge) Transactions(_ context.Context, _, accountID string, start, end time.Time) ([]bankfeed.Txn, []string, error) {
	s.txnCalls.Add(1)
	if s.started != nil {
		s.started <- struct{}{}
	}
	if s.block != nil {
		<-s.block
	}
	// window-faithful like the real bridge: only [start, end) comes back
	var out []bankfeed.Txn
	for _, t := range s.txns[accountID] {
		if !t.Posted.Before(start) && (end.IsZero() || t.Posted.Before(end)) {
			out = append(out, t)
		}
	}
	return out, s.notices, nil
}

// bankFixture: one owned property with a DONE node under an accepted $5,500
// contract (olga-sobkiv), one entity, vendor memory warm for the contractor.
func bankFixture(t *testing.T, bridge *stubBridge) (*Server, string, string) {
	t.Helper()
	vault, dataDir := t.TempDir(), t.TempDir()
	write := func(rel, content string) {
		t.Helper()
		full := filepath.Join(vault, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(full, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	write("system/realestate/properties/748-n-euclid.md", `---
categories: [property]
address: 748 N Euclid Ave, St. Louis
entity: garden-spe
status: construction
control: owned
---

## rocks
- [ ] Stabilize shell [work:: shell]
    - [x] Permit drawings [est:: 5500] [work:: shell/permit-drawings]
    - [ ] Roof [est:: 20000] [work:: shell/roof]
`)
	write("system/realestate/contracts/olga-drawings.md", `---
categories: [contract]
status: accepted
contractor: "[[olga-sobkiv]]"
total: 5500
date: 2026-08-01
allocations: ["748-n-euclid | shell/permit-drawings | 5500"]
---

# Olga drawings
`)
	write("system/realestate/entities/garden-spe.md", "---\ncategories: [entity]\nname: Garden SPE\n---\n")
	// a second property (split targets) + the chart of accounts (class lookups)
	write("system/realestate/properties/4852-fountain-ave.md", `---
categories: [property]
address: 4852 Fountain Ave, St. Louis
entity: garden-spe
status: construction
control: owned
---
`)
	write("system/realestate/categories.md", `---
categories: [money-categories]
items: ["internet | expense | operating", "rent | income | operating", "windows | expense | project", "materials | expense | project", "closing | expense | acquisition"]
---
`)

	ix, err := vaultindex.Open(vaultindex.Config{VaultRoot: vault})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { ix.Close() })
	if _, err := ix.Rebuild(); err != nil {
		t.Fatal(err)
	}
	vw := vaultwriter.New(vault).WithZoneRoots("system", "extrinsic").WithAudit(dataDir).Grant(
		vaultwriter.Capability{Name: "realestate", Zone: record.ZoneSystem,
			Pattern: "system/realestate/**", Actor: vaultwriter.ActorUserAction},
	)
	srv := &Server{index: ix}
	srv.UseVault(vw)
	srv.UseRealestate(realestate.New(ix), "system/realestate", dataDir)
	// vendor memory: the contractor has been categorized + placed before
	srv.reImport.Remember("", nil,
		map[string]string{"olga sobkiv": "drawings"},
		map[string]string{"olga sobkiv": "748-n-euclid"})

	svc := bankfeed.New(dataDir, bridge)
	srv.UseBankFeed(svc)
	if err := svc.Claim(context.Background(), "any-token"); err != nil {
		t.Fatal(err)
	}
	if err := svc.Store().Upsert(bankfeed.Link{
		SimplefinID: "act-1", EntitySlug: "garden-spe", AccountLabel: "checking", Enabled: true,
	}); err != nil {
		t.Fatal(err)
	}
	return srv, vault, dataDir
}

// syncNow is the (added, applied, err) shape the ingest tests were written
// against — FORCED, since the sync window would otherwise skip the immediate
// re-syncs they probe (dedupe, paused links).
func syncNow(srv *Server) (int, int, error) {
	res, err := srv.bankFeedSync(context.Background(), true)
	return res.Added, res.AutoApplied, err
}

// SimpleFIN's request budget (2026-09-10 notice: data once a day, ≤24
// requests/day). An unforced sync inside the window makes NO bridge request
// and says so; the owner's forced sync goes through; the window is read from
// the persisted stamp, so a restart cannot forget it.
func TestBankFeedSyncWindowSkipsUnforcedRepeats(t *testing.T) {
	bridge := &stubBridge{txns: map[string][]bankfeed.Txn{"act-1": {
		{ID: "t1", Posted: time.Now().AddDate(0, 0, -2), Amount: -10, Payee: "X"},
	}}}
	srv, _, dataDir := bankFixture(t, bridge)
	// steady state: a cursor exists, so one poll is ONE bridge request (a
	// cursor-less first poll walks the 90-day backfill in 30-day windows)
	srv.bankFeed.Store().MarkSeen("act-1", nil, time.Now().AddDate(0, 0, -1))

	// first sync after boot: nothing stamped yet → polls
	res, err := srv.bankFeedSync(context.Background(), false)
	if err != nil || res.Skipped || res.Added != 1 {
		t.Fatalf("first unforced sync: %+v %v", res, err)
	}
	if n := bridge.txnCalls.Load(); n != 1 {
		t.Fatalf("bridge polled %d times, want 1", n)
	}
	// inside the window: skipped, no request, no fabricated counts
	res, err = srv.bankFeedSync(context.Background(), false)
	if err != nil || !res.Skipped || res.Added != 0 || res.SyncedAt.IsZero() {
		t.Fatalf("second unforced sync: %+v %v — want skipped with the poll stamp", res, err)
	}
	if n := bridge.txnCalls.Load(); n != 1 {
		t.Fatalf("a skipped sync still hit the bridge (%d calls)", n)
	}
	// the owner's click bypasses the window
	res, err = srv.bankFeedSync(context.Background(), true)
	if err != nil || res.Skipped {
		t.Fatalf("forced sync: %+v %v", res, err)
	}
	if n := bridge.txnCalls.Load(); n != 2 {
		t.Fatalf("forced sync did not poll (%d calls)", n)
	}
	// a fresh process over the same dataDir inherits the stamp
	svc2 := bankfeed.New(dataDir, bridge)
	if last := svc2.Store().LastSyncAt(); last.IsZero() || time.Since(last) > time.Minute {
		t.Fatalf("poll stamp not persisted: %v", last)
	}
	// once the window has passed, the ticker's unforced sync polls again
	srv.bankFeed.Store().SetLastSyncAt(time.Now().Add(-bankFeedSyncWindow - time.Minute))
	res, err = srv.bankFeedSync(context.Background(), false)
	if err != nil || res.Skipped {
		t.Fatalf("post-window sync: %+v %v", res, err)
	}
	if n := bridge.txnCalls.Load(); n != 3 {
		t.Fatalf("post-window sync did not poll (%d calls)", n)
	}
	// the handler's sync-now is forced and reports the outcome shape
	code, body := doJSON(t, srv.handleBankfeedSync, "POST", "/api/bankfeed/sync", "{}")
	if code != 200 || body["skipped"] != false || body["syncedAt"] == "" {
		t.Fatalf("sync handler: %d %v", code, body)
	}
	if n := bridge.txnCalls.Load(); n != 4 {
		t.Fatalf("handler sync did not poll (%d calls)", n)
	}
}

// Two syncs racing (boot poll vs "sync now", or a double click) make ONE
// bridge request: the second waits for the first and reports its result as
// coalesced, never polling again.
func TestBankFeedSyncCoalescesConcurrentCalls(t *testing.T) {
	bridge := &stubBridge{
		txns:    map[string][]bankfeed.Txn{"act-1": {{ID: "t1", Posted: time.Now().AddDate(0, 0, -1), Amount: -7, Payee: "Y"}}},
		started: make(chan struct{}, 4),
		block:   make(chan struct{}),
	}
	srv, _, _ := bankFixture(t, bridge)
	srv.bankFeed.Store().MarkSeen("act-1", nil, time.Now().AddDate(0, 0, -1)) // one window per poll

	var wg sync.WaitGroup
	results := make([]bankSyncResult, 2)
	run := func(i int) {
		defer wg.Done()
		res, err := srv.bankFeedSync(context.Background(), true)
		if err != nil {
			t.Errorf("sync %d: %v", i, err)
		}
		results[i] = res
	}
	wg.Add(1)
	go run(0)
	<-bridge.started // the first poll is in flight at the bridge
	wg.Add(1)
	go run(1)
	time.Sleep(150 * time.Millisecond) // the second is queued on the sync mutex
	close(bridge.block)
	wg.Wait()

	if n := bridge.txnCalls.Load(); n != 1 {
		t.Fatalf("two racing syncs made %d bridge requests, want 1", n)
	}
	if results[0].Coalesced || !results[1].Coalesced {
		t.Fatalf("coalescing flags wrong: first=%+v second=%+v", results[0], results[1])
	}
	if results[1].Added != results[0].Added || results[1].SyncedAt != results[0].SyncedAt {
		t.Fatalf("coalesced result must report the in-flight poll: %+v vs %+v", results[0], results[1])
	}
}

// The account listing behind the SETTINGS/money panel is served from the
// cache inside the TTL — a render is not a bridge request. ?refresh=1 is the
// owner's live fetch, and every answer carries asOf (when it was REALLY
// fetched), never an implied "live".
func TestBankfeedAccountsHandlerServesCachedListing(t *testing.T) {
	bridge := &stubBridge{txns: map[string][]bankfeed.Txn{}}
	srv, _, _ := bankFixture(t, bridge)
	bridge.acctCalls.Store(0) // the fixture's claim does not list accounts

	get := func(q string) map[string]any {
		t.Helper()
		code, body := doJSON(t, srv.handleBankfeedAccounts, "GET", "/api/bankfeed/accounts"+q, "")
		if code != 200 {
			t.Fatalf("accounts %q: %d %v", q, code, body)
		}
		return body
	}
	first := get("")
	if n := bridge.acctCalls.Load(); n != 1 {
		t.Fatalf("first render: %d bridge calls, want 1", n)
	}
	asOf, _ := first["asOf"].(string)
	if _, err := time.Parse(time.RFC3339, asOf); err != nil {
		t.Fatalf("asOf missing/bad: %v", first["asOf"])
	}
	second := get("")
	get("")
	if n := bridge.acctCalls.Load(); n != 1 {
		t.Fatalf("re-renders inside the TTL hit the bridge (%d calls)", n)
	}
	if second["asOf"] != asOf {
		t.Fatalf("cached answer must keep the original fetch time: %v vs %v", second["asOf"], asOf)
	}
	if accts, _ := second["accounts"].([]any); len(accts) != 1 {
		t.Fatalf("cached listing lost the accounts: %v", second["accounts"])
	}
	get("?refresh=1")
	if n := bridge.acctCalls.Load(); n != 2 {
		t.Fatalf("?refresh=1 did not go live (%d calls)", n)
	}
	// a fresh claim invalidates the cache → it answers with a live listing
	code, _ := doJSON(t, srv.handleBankfeedClaim, "POST", "/api/bankfeed/claim", `{"token":"again"}`)
	if code != 200 {
		t.Fatalf("claim: %d", code)
	}
	if n := bridge.acctCalls.Load(); n != 3 {
		t.Fatalf("claim must return a fresh listing (%d calls)", n)
	}
}

func TestBankFeedSyncIngestsAutoAppliesAndDedupes(t *testing.T) {
	day := func(s string) time.Time { d, _ := time.Parse("2006-01-02", s); return d }
	bridge := &stubBridge{txns: map[string][]bankfeed.Txn{"act-1": {
		{ID: "t1", Posted: day("2026-08-14"), Amount: -5500, Description: "CHECK 1041", Payee: "Olga Sobkiv"},
		{ID: "t2", Posted: day("2026-08-15"), Amount: -123.45, Description: "HOME DEPOT #55", Payee: "Home Depot"},
		{ID: "t3", Posted: day("2026-08-15"), Amount: 2000, Description: "ZELLE DEPOSIT", Payee: "Tenant A"},
	}}}
	srv, vault, dataDir := bankFixture(t, bridge)

	added, applied, err := syncNow(srv)
	if err != nil {
		t.Fatal(err)
	}
	if added != 3 || applied != 1 {
		t.Fatalf("sync: added=%d applied=%d, want 3/1", added, applied)
	}

	// the contract-matched row landed in the ledger with the full token set,
	// audited as bank-feed
	led, err := os.ReadFile(filepath.Join(vault, "system/realestate/properties/748-n-euclid.ledger.csv"))
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"Olga Sobkiv", "5500", "[work:: shell/permit-drawings]",
		"[contract:: olga-drawings]", "[paid-by:: garden-spe]", "[stmt:: garden-spe:checking]"} {
		if !strings.Contains(string(led), want) {
			t.Fatalf("ledger missing %q:\n%s", want, led)
		}
	}
	audit, _ := os.ReadFile(filepath.Join(dataDir, "write-audit.log"))
	if !strings.Contains(string(audit), "bank-feed") {
		t.Fatalf("audit log has no bank-feed line:\n%s", audit)
	}
	// the draw closed: committed == paid on the node, nothing unreconciled
	p, _ := srv.realestate.Get("748-n-euclid")
	node, _ := realestate.FindWorkNode(p.Work, "shell/permit-drawings")
	if node == nil || node.Paid != 5500 || node.Unreconciled != 0 {
		t.Fatalf("node money after auto-apply: %+v", node)
	}

	// everything else waits in the $ tab: entity pre-set, feed badge, inflow
	rows, _ := srv.statements.List()
	byVendor := map[string]realestate.StatementRow{}
	for _, r := range rows {
		byVendor[r.Vendor] = r
	}
	if r := byVendor["Home Depot"]; r.State != "pending" || r.Entity != "garden-spe" || r.Source != "feed" {
		t.Fatalf("home depot row: %+v", r)
	}
	if r := byVendor["Tenant A"]; !r.Inflow || r.State != "pending" {
		t.Fatalf("tenant row: %+v", r)
	}
	if r := byVendor["Olga Sobkiv"]; r.State != "applied" {
		t.Fatalf("olga row: %+v", r)
	}

	// one digest card, bank: prefix, dismissable through the portal lane
	cards := srv.bankFeedCards()
	if len(cards) != 1 || !strings.HasPrefix(cards[0].ID, "bank:") || cards[0].Portal != "bank" {
		t.Fatalf("cards: %+v", cards)
	}
	if !strings.Contains(cards[0].Detail, "✓ reconciled") {
		t.Fatalf("digest detail: %q", cards[0].Detail)
	}

	// re-sync is free — feed-level dedupe, no new rows, no double writes
	added, applied, err = syncNow(srv)
	if err != nil || added != 0 || applied != 0 {
		t.Fatalf("re-sync: %d/%d/%v, want 0/0", added, applied, err)
	}

	// §5: the owner's note lands verbatim in the ledger with tokens intact
	hd := byVendor["Home Depot"]
	code, _ := doJSON(t, srv.handleStatementsRow, "POST", "/api/realestate/statements/row",
		`{"id":"`+hd.ID+`","note":"material run — 748 kitchen","category":"materials",`+
			`"assignments":[{"slug":"748-n-euclid","amount":123.45}]}`)
	if code != 200 {
		t.Fatalf("row patch: %d", code)
	}
	code, _ = doJSON(t, srv.handleStatementsApply, "POST", "/api/realestate/statements/apply",
		`{"ids":["`+hd.ID+`"]}`)
	if code != 200 {
		t.Fatalf("apply: %d", code)
	}
	led, _ = os.ReadFile(filepath.Join(vault, "system/realestate/properties/748-n-euclid.ledger.csv"))
	if !strings.Contains(string(led), "material run — 748 kitchen [paid-by:: garden-spe] [stmt:: garden-spe:checking]") {
		t.Fatalf("owner note didn't ride into the ledger:\n%s", led)
	}

	// a paused link syncs nothing even with fresh txns waiting
	bridge.txns["act-1"] = append(bridge.txns["act-1"],
		bankfeed.Txn{ID: "t4", Posted: day("2026-08-16"), Amount: -50, Description: "GAS", Payee: "QT"})
	if err := srv.bankFeed.Store().Upsert(bankfeed.Link{
		SimplefinID: "act-1", EntitySlug: "garden-spe", AccountLabel: "checking", Enabled: false,
	}); err != nil {
		t.Fatal(err)
	}
	added, _, _ = syncNow(srv)
	if added != 0 {
		t.Fatalf("disabled link ingested %d row(s)", added)
	}
}

// A near-miss amount (outside ±1%) must NOT auto-apply — it waits for the
// owner even when vendor memory knows the contractor.
func TestBankFeedNoAutoApplyOnAmountMismatch(t *testing.T) {
	day := func(s string) time.Time { d, _ := time.Parse("2006-01-02", s); return d }
	bridge := &stubBridge{txns: map[string][]bankfeed.Txn{"act-1": {
		{ID: "t1", Posted: day("2026-08-14"), Amount: -5000, Description: "CHECK 1042", Payee: "Olga Sobkiv"},
	}}}
	srv, vault, _ := bankFixture(t, bridge)

	added, applied, err := syncNow(srv)
	if err != nil || added != 1 || applied != 0 {
		t.Fatalf("sync: %d/%d/%v, want 1 added, 0 applied", added, applied, err)
	}
	if _, err := os.Stat(filepath.Join(vault, "system/realestate/properties/748-n-euclid.ledger.csv")); !os.IsNotExist(err) {
		t.Fatal("a non-matching row must not touch the ledger")
	}
}

// The full-history backfill is a bulk import for hand categorization: even a
// perfect contract match stays pending, and the daily sync never re-hauls
// what the backfill marked seen.
func TestBankFeedBackfillIngestsWithoutAutoApply(t *testing.T) {
	// posted inside the bridge's 90-day window (it can serve nothing older)
	bridge := &stubBridge{txns: map[string][]bankfeed.Txn{"act-1": {
		{ID: "h1", Posted: time.Now().AddDate(0, 0, -80), Amount: -5500, Description: "CHECK 900", Payee: "Olga Sobkiv"},
		{ID: "h2", Posted: time.Now().AddDate(0, 0, -70), Amount: 1200, Description: "DEPOSIT", Payee: "Tenant A"},
	}}}
	srv, vault, _ := bankFixture(t, bridge)

	req := httptest.NewRequest("POST", "/api/bankfeed/accounts/act-1/backfill", strings.NewReader("{}"))
	req.SetPathValue("id", "act-1")
	rec := httptest.NewRecorder()
	srv.handleBankfeedBackfill(rec, req)
	if rec.Code != 200 {
		t.Fatalf("backfill: %d %s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), `"added":2`) {
		t.Fatalf("backfill body: %s", rec.Body.String())
	}
	rows, _ := srv.statements.List()
	for _, r := range rows {
		if r.State == "applied" {
			t.Fatalf("backfill auto-applied %+v", r)
		}
	}
	if _, err := os.Stat(filepath.Join(vault, "system/realestate/properties/748-n-euclid.ledger.csv")); !os.IsNotExist(err) {
		t.Fatal("backfill must not write the ledger")
	}
	// the daily sync sees nothing new — backfill marked the ids seen
	if added, _, _ := syncNow(srv); added != 0 {
		t.Fatalf("sync re-hauled %d backfilled row(s)", added)
	}
}

// Filing IS the write (owner call 2026-08-19): a PATCH carrying file:true
// applies a fully-filed row straight to the ledger; intermediate patches
// (hop tethers, notes — no file flag) never apply early.
func TestStatementsRowFilingGestureAutoApplies(t *testing.T) {
	bridge := &stubBridge{txns: map[string][]bankfeed.Txn{"act-1": {
		{ID: "t1", Posted: time.Now().AddDate(0, 0, -5), Amount: -321.09, Description: "SUPPLIES", Payee: "Ace Hardware"},
	}}}
	srv, vault, dataDir := bankFixture(t, bridge)
	if _, _, err := syncNow(srv); err != nil {
		t.Fatal(err)
	}
	rows, _ := srv.statements.List()
	id := rows[0].ID

	// hop-style patch WITHOUT the file flag: assigned, categorized — no write
	code, res := doJSON(t, srv.handleStatementsRow, "POST", "/api/realestate/statements/row",
		`{"id":"`+id+`","category":"materials","assignments":[{"slug":"748-n-euclid","amount":321.09}]}`)
	if code != 200 || res["state"] == "applied" {
		t.Fatalf("no-file patch: %d %v — must stay unapplied", code, res["state"])
	}
	if _, err := os.Stat(filepath.Join(vault, "system/realestate/properties/748-n-euclid.ledger.csv")); !os.IsNotExist(err) {
		t.Fatal("ledger written without a filing gesture")
	}

	// the filing gesture: file:true alone applies the now-complete row
	code, res = doJSON(t, srv.handleStatementsRow, "POST", "/api/realestate/statements/row",
		`{"id":"`+id+`","file":true}`)
	if code != 200 || res["state"] != "applied" {
		t.Fatalf("file gesture: %d %v — want applied", code, res["state"])
	}
	led, err := os.ReadFile(filepath.Join(vault, "system/realestate/properties/748-n-euclid.ledger.csv"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(led), "321.09") || !strings.Contains(string(led), "materials") {
		t.Fatalf("filed row missing from ledger:\n%s", led)
	}
	// audit actor stays user-action for owner filings
	raw, err := os.ReadFile(filepath.Join(dataDir, "write-audit.log"))
	if err != nil || !strings.Contains(string(raw), "user-action") {
		t.Fatalf("audit log missing the user-action line: %v\n%s", err, raw)
	}
	if strings.Contains(string(raw), "bank-feed") {
		t.Fatal("owner filing must not audit as bank-feed")
	}
}

// SimpleFIN answers 200 with an `errors` advisory when the BANK connection
// expired — while still serving the stale account. Dropping that made an
// 11-day auth outage look like healthy zero-row syncs (2026-08-31). The
// advisory now lands in link health and pages as a signal; a clean sync
// clears both.
func TestBridgeAdvisorySurfacesAndClears(t *testing.T) {
	bridge := &stubBridge{txns: map[string][]bankfeed.Txn{"act-1": {
		{ID: "t1", Posted: time.Now().AddDate(0, 0, -20), Amount: -10, Payee: "Old"},
	}}, notices: []string{"Connection to Central Bank Business may need attention. Auth required"}}
	srv, _, _ := bankFixture(t, bridge)
	if _, _, err := syncNow(srv); err != nil {
		t.Fatal(err)
	}
	links := srv.bankFeed.Store().Links()
	if len(links) != 1 || !strings.Contains(links[0].LastError, "Auth required") {
		t.Fatalf("advisory did not land in link health: %+v", links)
	}
	if links[0].LastSync == "" {
		t.Fatal("lastSync must still advance — the bridge WAS reachable")
	}
	// the signal pages, pointing at settings
	sigs, err := srv.BankFeedAttentionEmitter().Emit(time.Now())
	if err != nil || len(sigs) != 1 {
		t.Fatalf("want 1 attention signal, got %d (%v)", len(sigs), err)
	}
	if sigs[0].Kind != "bank-feed-attention" || !strings.Contains(sigs[0].Label, "Auth required") {
		t.Fatalf("signal wrong: %+v", sigs[0])
	}
	// the bank fixes their side; the next sync clears health and the signal
	bridge.notices = nil
	if _, _, err := syncNow(srv); err != nil {
		t.Fatal(err)
	}
	if links := srv.bankFeed.Store().Links(); links[0].LastError != "" {
		t.Fatalf("clean sync should clear the advisory, got %q", links[0].LastError)
	}
	if sigs, _ := srv.BankFeedAttentionEmitter().Emit(time.Now()); len(sigs) != 0 {
		t.Fatalf("signal should clear, got %+v", sigs)
	}
}

// A LIVE link that has synced nothing for days pages too — a feed that
// quietly stopped is the same emergency with no advisory attached.
func TestStaleBankFeedPages(t *testing.T) {
	bridge := &stubBridge{txns: map[string][]bankfeed.Txn{}}
	srv, _, _ := bankFixture(t, bridge)
	srv.bankFeed.Store().SetLinkHealth("act-1", time.Now().AddDate(0, 0, -5).Format(time.RFC3339), "")
	sigs, _ := srv.BankFeedAttentionEmitter().Emit(time.Now())
	if len(sigs) != 1 || sigs[0].Kind != "bank-feed-stale" {
		t.Fatalf("want a stale signal, got %+v", sigs)
	}
}

// The FEED's bank lane serves the compact unfiled slice — feed-source rows
// only, and a filed row leaves it.
func TestBankPendingRowsFeedTheGlobalFeed(t *testing.T) {
	posted := time.Now().AddDate(0, 0, -2)
	bridge := &stubBridge{txns: map[string][]bankfeed.Txn{"act-1": {
		{ID: "t1", Posted: posted, Amount: -63.20, Description: "HOME DEPOT", Payee: "Home Depot X"},
	}}}
	srv, _, _ := bankFixture(t, bridge)
	if _, _, err := syncNow(srv); err != nil {
		t.Fatal(err)
	}
	rows := srv.bankPendingRows()
	if len(rows) == 0 {
		t.Fatal("synced row missing from the feed slice")
	}
	var hd *bankPendingRow
	for i := range rows {
		if rows[i].Vendor == "Home Depot X" {
			hd = &rows[i]
		}
	}
	if hd == nil || hd.Amount != 63.20 || hd.Entity == "" || hd.ID == "" {
		t.Fatalf("row shape wrong: %+v", rows)
	}
	// filing it through the same PATCH the FEED card uses removes it
	code, res := doJSON(t, srv.handleStatementsRow, "POST", "/api/realestate/statements/row",
		`{"id":"`+hd.ID+`","category":"materials","assignments":[{"slug":"748-n-euclid","amount":63.20}],"state":"assigned","file":true}`)
	if code != 200 || res["state"] != "applied" {
		t.Fatalf("file from feed: %d %v (%v)", code, res["state"], res["fileError"])
	}
	for _, r := range srv.bankPendingRows() {
		if r.ID == hd.ID {
			t.Fatal("filed row still in the feed slice")
		}
	}
}
