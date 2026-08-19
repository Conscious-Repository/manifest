package server

import (
	"context"
	"strings"
	"testing"
	"time"

	"manifest/bankfeed"
	"manifest/realestate"
)

// A CSV row and its bank-feed twin are the SAME transaction: the feed stores
// ISO dates, so the CSV's 08/18/2026 has to normalize on the way in or the
// dedupe key can never match and the row lands twice.
func TestCSVIngestDedupesAgainstTheBankFeed(t *testing.T) {
	posted := time.Date(2026, 8, 18, 12, 0, 0, 0, time.UTC)
	bridge := &stubBridge{txns: map[string][]bankfeed.Txn{"act-1": {
		{ID: "t1", Posted: posted, Amount: -1075, Description: "MY CPA GUY", Payee: "My Cpa Guy"},
	}}}
	srv, _, _ := bankFixture(t, bridge)
	if _, _, err := srv.bankFeedSync(context.Background()); err != nil {
		t.Fatal(err)
	}
	before, _ := srv.statements.List()

	// the same charge, as the bank's own CSV export spells it
	code, res := doJSON(t, srv.handleStatementsIngest, "POST", "/api/realestate/statements/ingest",
		`{"label":"export.csv","entity":"Garden SPE","signature":"sig-a","sign":"expense-negative",
		  "mapping":{"date":"Date","vendor":"Description","amount":"DebitCredit"},
		  "rows":[{"Date":"08/18/2026","Vendor":"My Cpa Guy","Amount":-1075}]}`)
	if code != 200 {
		t.Fatalf("ingest: %d %v", code, res)
	}
	if res["added"] != float64(0) || res["duplicates"] != float64(1) {
		t.Fatalf("the feed's own row must dedupe: added=%v duplicates=%v", res["added"], res["duplicates"])
	}
	after, _ := srv.statements.List()
	if len(after) != len(before) {
		t.Fatalf("lot grew from %d to %d on a duplicate", len(before), len(after))
	}
}

// The convention says which sign is a charge. Reading it backwards books every
// expense as income, which is worse than a duplicate — it moves money.
func TestIngestHonoursTheSignConvention(t *testing.T) {
	srv, _, _ := bankFixture(t, &stubBridge{})
	// distinct months so the second ingest is not deduped as a repeat of the first
	body := func(sign, month string) string {
		return `{"label":"e.csv","entity":"Garden SPE","signature":"sig-` + sign + `","sign":"` + sign + `",
		  "mapping":{"date":"Date"},"rows":[
		    {"Date":"` + month + `/04/2026","Vendor":"Ameren","Amount":-937.21},
		    {"Date":"` + month + `/05/2026","Vendor":"Rent","Amount":1750}]}`
	}
	code, _ := doJSON(t, srv.handleStatementsIngest, "POST", "/api/realestate/statements/ingest", body("expense-negative", "03"))
	if code != 200 {
		t.Fatal(code)
	}
	rows, _ := srv.statements.List()
	var iso bool
	for _, r := range rows {
		if r.Date == "2026-03-04" {
			iso = true
		}
	}
	if !iso {
		t.Fatalf("03/04/2026 did not normalize to ISO: %+v", rows)
	}
	for _, r := range rows {
		switch r.Vendor {
		case "Ameren":
			if r.Date != "2026-03-04" {
				continue
			}
			if r.Inflow || r.Amount != 937.21 {
				t.Errorf("negative charge should be an expense of 937.21, got inflow=%v amount=%v", r.Inflow, r.Amount)
			}
		case "Rent":
			if !r.Inflow {
				t.Error("positive amount should be a deposit under expense-negative")
			}
		}
	}
	// the same rows read the other way round flip direction
	code, _ = doJSON(t, srv.handleStatementsIngest, "POST", "/api/realestate/statements/ingest", body("expense-positive", "04"))
	if code != 200 {
		t.Fatal(code)
	}
	rows, _ = srv.statements.List()
	var seen bool
	for _, r := range rows {
		if r.Vendor == "Ameren" && r.Date == "2026-04-04" && r.Inflow {
			seen = true
		}
	}
	if !seen {
		t.Error("expense-positive should read the negative row as a deposit")
	}
	// and the convention is remembered for the format
	if got := srv.reImport.SignFor("sig-expense-positive"); got != realestate.SignExpensePositive {
		t.Errorf("sign not remembered, got %q", got)
	}
}

// An ingest with no stated convention must refuse rather than fall back to a
// default and silently invert the file.
func TestIngestRefusesWithoutASignConvention(t *testing.T) {
	srv, _, _ := bankFixture(t, &stubBridge{})
	code, _ := doJSON(t, srv.handleStatementsIngest, "POST", "/api/realestate/statements/ingest",
		`{"label":"e.csv","entity":"Garden SPE","rows":[{"Date":"03/04/2026","Vendor":"X","Amount":-10}]}`)
	if code == 200 {
		t.Fatal("missing sign convention must refuse")
	}
}

// Same day, same amount, different payee text: not provably the same charge,
// so it lands — but the response says so instead of quietly doubling.
func TestIngestWarnsOnUnprovableOverlap(t *testing.T) {
	posted := time.Date(2026, 8, 17, 12, 0, 0, 0, time.UTC)
	bridge := &stubBridge{txns: map[string][]bankfeed.Txn{"act-1": {
		{ID: "t1", Posted: posted, Amount: -937.21, Description: "AMEREN", Payee: "Ameren"},
	}}}
	srv, _, _ := bankFixture(t, bridge)
	if _, _, err := srv.bankFeedSync(context.Background()); err != nil {
		t.Fatal(err)
	}
	_, res := doJSON(t, srv.handleStatementsIngest, "POST", "/api/realestate/statements/ingest",
		`{"label":"e.csv","entity":"Garden SPE","signature":"s","sign":"expense-negative","mapping":{"date":"Date"},
		  "rows":[{"Date":"08/17/2026","Vendor":"SPI*AMERENUE      SAINT LOU IS  MOSPI*AMERE NU","Amount":-937.21}]}`)
	if res["added"] != float64(1) || res["suspects"] != float64(1) {
		t.Fatalf("expected 1 added and 1 flagged suspect, got added=%v suspects=%v", res["added"], res["suspects"])
	}
}

// The raw description is the vendor AND the note: the workbench shows a name a
// human can categorize, nothing the bank sent is lost.
func TestIngestTidiesVendorAndKeepsTheRawDescription(t *testing.T) {
	srv, _, _ := bankFixture(t, &stubBridge{})
	code, _ := doJSON(t, srv.handleStatementsIngest, "POST", "/api/realestate/statements/ingest",
		`{"label":"e.csv","entity":"Garden SPE","signature":"s","sign":"expense-negative","mapping":{"date":"Date"},
		  "rows":[{"Date":"02/10/2026","Vendor":"THE HOME DEPOT THE HOME DEPOT #   BRENTWOOD     MO","Amount":-39.83}]}`)
	if code != 200 {
		t.Fatal(code)
	}
	rows, _ := srv.statements.List()
	if len(rows) != 1 {
		t.Fatalf("want 1 row, got %d", len(rows))
	}
	if rows[0].Vendor != "THE HOME DEPOT # BRENTWOOD MO" {
		t.Errorf("vendor not tidied: %q", rows[0].Vendor)
	}
	if !strings.Contains(rows[0].Note, "THE HOME DEPOT THE HOME DEPOT # BRENTWOOD MO") {
		t.Errorf("raw description lost: %q", rows[0].Note)
	}
}

// A bank can post the same charge to the same payee twice in one day — the
// owner's export shows two identical $26.55 lines with running balances
// $26.55 apart. Both are real, so both must land; re-uploading that same file
// must still add nothing.
func TestIngestKeepsGenuineSameDayRepeatsButNotReuploads(t *testing.T) {
	srv, _, _ := bankFixture(t, &stubBridge{})
	body := `{"label":"e.csv","entity":"Garden SPE","signature":"s","sign":"expense-negative","mapping":{"date":"Date"},
	  "rows":[{"Date":"07/08/2026","Vendor":"4TE*CITY OF ST. LOS","Amount":-26.55},
	          {"Date":"07/08/2026","Vendor":"4TE*CITY OF ST. LOS","Amount":-26.55}]}`
	if code, res := doJSON(t, srv.handleStatementsIngest, "POST", "/api/realestate/statements/ingest", body); code != 200 ||
		res["added"] != float64(2) || res["duplicates"] != float64(0) {
		t.Fatalf("both real charges must land: %d added=%v dups=%v", code, res["added"], res["duplicates"])
	}
	// the same file again adds nothing
	if _, res := doJSON(t, srv.handleStatementsIngest, "POST", "/api/realestate/statements/ingest", body); res["added"] != float64(0) ||
		res["duplicates"] != float64(2) {
		t.Fatalf("re-upload must be a no-op: added=%v dups=%v", res["added"], res["duplicates"])
	}
	rows, _ := srv.statements.List()
	if len(rows) != 2 {
		t.Fatalf("want exactly 2 rows in the lot, got %d", len(rows))
	}
	// they are distinct rows, not one row written twice
	if rows[0].ID == rows[1].ID {
		t.Fatal("the two charges collapsed onto one id")
	}
}

// The bank feed stamps rows with the entity slug and the upload strip used to
// send the display name, so one entity split into two folds in the filed
// history — and wrote two different [paid-by::] tokens into the ledger.
func TestIngestCanonicalizesTheEntityToItsSlug(t *testing.T) {
	srv, _, _ := bankFixture(t, &stubBridge{})
	for i, sent := range []string{"Garden SPE", "garden-spe", "GARDEN SPE"} {
		code, _ := doJSON(t, srv.handleStatementsIngest, "POST", "/api/realestate/statements/ingest",
			`{"label":"e.csv","entity":"`+sent+`","signature":"s","sign":"expense-negative","mapping":{"date":"Date"},
			  "rows":[{"Date":"0`+string(rune('1'+i))+`/10/2026","Vendor":"Ameren","Amount":-10}]}`)
		if code != 200 {
			t.Fatalf("ingest %q: %d", sent, code)
		}
	}
	rows, _ := srv.statements.List()
	if len(rows) != 3 {
		t.Fatalf("want 3 rows, got %d", len(rows))
	}
	for _, r := range rows {
		if r.Entity != "garden-spe" {
			t.Errorf("entity not canonicalized: %q", r.Entity)
		}
	}
	// an entity with no record still resolves to a stable slug, never to ""
	doJSON(t, srv.handleStatementsIngest, "POST", "/api/realestate/statements/ingest",
		`{"label":"e.csv","entity":"Some New LLC","signature":"s","sign":"expense-negative","mapping":{"date":"Date"},
		  "rows":[{"Date":"09/10/2026","Vendor":"X","Amount":-10}]}`)
	rows, _ = srv.statements.List()
	var found bool
	for _, r := range rows {
		if r.Entity == "some-new-llc" {
			found = true
		}
	}
	if !found {
		t.Error("an unknown entity should still slugify")
	}
}

// The bulk categorize lane: one category across many rows, never touching
// applied/skipped rows, never filing.
func TestCategorizeBulkSetsOnlyLiveRows(t *testing.T) {
	srv, _, _ := bankFixture(t, &stubBridge{})
	code, _ := doJSON(t, srv.handleStatementsIngest, "POST", "/api/realestate/statements/ingest",
		`{"label":"e.csv","entity":"Garden SPE","signature":"s","sign":"expense-negative","mapping":{"date":"Date"},
		  "rows":[{"Date":"02/14/2026","Vendor":"Ozark Electric WEB PMTS 3BJ92S","Amount":-198.45},
		          {"Date":"03/14/2026","Vendor":"Ozark Electric WEB PMTS 5H216S","Amount":-237.07},
		          {"Date":"04/13/2026","Vendor":"Ozark Electric WEB PMTS 7CLLNS","Amount":-201.19}]}`)
	if code != 200 {
		t.Fatal(code)
	}
	rows, _ := srv.statements.List()
	if len(rows) != 3 {
		t.Fatalf("want 3 rows, got %d", len(rows))
	}
	// every copy List returns carries the derived merchant key
	for _, r := range rows {
		if r.MerchantKey != "OZARK ELECTRIC" {
			t.Fatalf("merchantKey not stamped: %q on %q", r.MerchantKey, r.Vendor)
		}
	}
	// skip one row (with a reason), then categorize all three ids
	skip, reason := "skipped", "personal"
	if _, err := srv.statements.Update(rows[2].ID, nil, nil, nil, &skip, &reason); err != nil {
		t.Fatal(err)
	}
	ids := `["` + rows[0].ID + `","` + rows[1].ID + `","` + rows[2].ID + `"]`
	code, res := doJSON(t, srv.handleStatementsCategorize, "POST", "/api/realestate/statements/categorize",
		`{"ids":`+ids+`,"category":"electric"}`)
	if code != 200 || res["updated"] != float64(2) {
		t.Fatalf("want 2 updated (skipped row untouched): %d %v", code, res["updated"])
	}
	after, _ := srv.statements.List()
	for _, r := range after {
		if r.State == "skipped" {
			if r.Category == "electric" {
				t.Error("bulk categorize touched a skipped row")
			}
			continue
		}
		if r.Category != "electric" {
			t.Errorf("live row not categorized: %+v", r.State)
		}
		if r.State != "pending" {
			t.Errorf("bulk categorize must not file or advance state, got %q", r.State)
		}
	}
	// refusals: no ids, no category
	if code, _ := doJSON(t, srv.handleStatementsCategorize, "POST", "/api/realestate/statements/categorize",
		`{"ids":[],"category":"x"}`); code == 200 {
		t.Error("empty ids must refuse")
	}
	if code, _ := doJSON(t, srv.handleStatementsCategorize, "POST", "/api/realestate/statements/categorize",
		`{"ids":["stmt-x"],"category":"  "}`); code == 200 {
		t.Error("blank category must refuse")
	}
}
