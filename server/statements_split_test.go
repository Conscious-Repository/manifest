package server

import (
	"context"
	"encoding/json"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"manifest/bankfeed"
	"manifest/realestate"
)

// One bank line across two properties (the Tree Court check): filing a split
// writes one ledger row per target, preserves the owner's note under the
// split annotation, and carries per-target tokens.
func TestSplitFilingWritesEveryTarget(t *testing.T) {
	bridge := &stubBridge{txns: map[string][]bankfeed.Txn{"act-1": {
		{ID: "t1", Posted: time.Now().AddDate(0, 0, -4), Amount: -10131, Description: "CHECK 108", Payee: "Tree Court"},
	}}}
	srv, vault, _ := bankFixture(t, bridge)
	if _, _, err := srv.bankFeedSync(context.Background()); err != nil {
		t.Fatal(err)
	}
	rows, _ := srv.statements.List()
	id := rows[0].ID

	// owner note first (no file flag — must not apply anything)
	code, _ := doJSON(t, srv.handleStatementsRow, "POST", "/api/realestate/statements/row",
		`{"id":"`+id+`","note":"windows both buildings","category":"windows"}`)
	if code != 200 {
		t.Fatalf("note patch: %d", code)
	}
	// the split filing gesture: two targets, Σ = amount
	code, res := doJSON(t, srv.handleStatementsRow, "POST", "/api/realestate/statements/row",
		`{"id":"`+id+`","state":"split","file":true,"assignments":[`+
			`{"slug":"748-n-euclid","amount":5000,"workId":"shell/roof"},`+
			`{"slug":"4852-fountain-ave","amount":5131}]}`)
	if code != 200 || res["state"] != "applied" {
		t.Fatalf("split file: %d %v (fileError %v)", code, res["state"], res["fileError"])
	}
	led1, err := os.ReadFile(filepath.Join(vault, "system/realestate/properties/748-n-euclid.ledger.csv"))
	if err != nil {
		t.Fatal(err)
	}
	led2, err := os.ReadFile(filepath.Join(vault, "system/realestate/properties/4852-fountain-ave.ledger.csv"))
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"5000", "windows both buildings · split 1/2 of $10131.00 · Tree Court",
		"[work:: shell/roof]", "[paid-by:: garden-spe]", "[stmt:: garden-spe:checking]"} {
		if !strings.Contains(string(led1), want) {
			t.Fatalf("748 ledger missing %q:\n%s", want, led1)
		}
	}
	for _, want := range []string{"5131", "windows both buildings · split 2/2 of $10131.00 · Tree Court"} {
		if !strings.Contains(string(led2), want) {
			t.Fatalf("4852 ledger missing %q:\n%s", want, led2)
		}
	}
	if strings.Contains(string(led2), "[work::") {
		t.Fatal("the untethered slice must not inherit the other slice's tether")
	}
}

// An operating-class category (chart of accounts) writes [cat:: operating]:
// the row joins the property's operating lane and moves NO project figure.
func TestOperatingCategoryFilesIntoOperatingLane(t *testing.T) {
	bridge := &stubBridge{txns: map[string][]bankfeed.Txn{"act-1": {
		{ID: "t1", Posted: time.Now().AddDate(0, 0, -3), Amount: -60.96, Description: "SPECTRUM", Payee: "Spectrum"},
		{ID: "t2", Posted: time.Now().AddDate(0, 0, -2), Amount: 1750, Description: "RENT", Payee: "Tenant"},
	}}}
	srv, vault, _ := bankFixture(t, bridge)
	if _, _, err := srv.bankFeedSync(context.Background()); err != nil {
		t.Fatal(err)
	}
	rows, _ := srv.statements.List()
	byVendor := map[string]realestate.StatementRow{}
	for _, r := range rows {
		byVendor[r.Vendor] = r
	}
	before, _ := srv.realestate.Get("748-n-euclid")

	// utilities expense: category "internet" is operating-class in the registry
	code, res := doJSON(t, srv.handleStatementsRow, "POST", "/api/realestate/statements/row",
		`{"id":"`+byVendor["Spectrum"].ID+`","category":"internet","file":true,`+
			`"assignments":[{"slug":"748-n-euclid","amount":60.96}]}`)
	if code != 200 || res["state"] != "applied" {
		t.Fatalf("file: %d %v (%v)", code, res["state"], res["fileError"])
	}
	// the rent inflow files with one gesture (no category required)
	code, res = doJSON(t, srv.handleStatementsRow, "POST", "/api/realestate/statements/row",
		`{"id":"`+byVendor["Tenant"].ID+`","file":true,`+
			`"assignments":[{"slug":"748-n-euclid","amount":1750}]}`)
	if code != 200 || res["state"] != "applied" {
		t.Fatalf("inflow file: %d %v (%v)", code, res["state"], res["fileError"])
	}

	led, _ := os.ReadFile(filepath.Join(vault, "system/realestate/properties/748-n-euclid.ledger.csv"))
	if !strings.Contains(string(led), "[cat:: operating]") {
		t.Fatalf("operating token missing:\n%s", led)
	}
	if !strings.Contains(string(led), "income") {
		t.Fatalf("income row missing:\n%s", led)
	}

	after, _ := srv.realestate.Get("748-n-euclid")
	if after.Project.Paid != before.Project.Paid {
		t.Fatalf("operating expense moved project SPENT: %v → %v", before.Project.Paid, after.Project.Paid)
	}
	if after.Operating == nil || after.Operating.Income != 1750 || after.Operating.Expenses != 60.96 {
		t.Fatalf("operating view: %+v", after.Operating)
	}
	if after.Operating.Months[0].ByCategory["internet"] != 60.96 {
		t.Fatalf("byCategory: %+v", after.Operating.Months[0].ByCategory)
	}
}

// A filing gesture that can't complete reports WHY (the 736 lesson — five
// rows sat behind a success-shaped 200 for weeks).
func TestFilingErrorSurfaces(t *testing.T) {
	bridge := &stubBridge{txns: map[string][]bankfeed.Txn{"act-1": {
		{ID: "t1", Posted: time.Now().AddDate(0, 0, -1), Amount: -50, Description: "GAS", Payee: "QT"},
	}}}
	srv, _, _ := bankFixture(t, bridge)
	if _, _, err := srv.bankFeedSync(context.Background()); err != nil {
		t.Fatal(err)
	}
	rows, _ := srv.statements.List()
	code, res := doJSON(t, srv.handleStatementsRow, "POST", "/api/realestate/statements/row",
		`{"id":"`+rows[0].ID+`","file":true,"assignments":[{"slug":"748-n-euclid","amount":50}]}`)
	if code != 200 {
		t.Fatalf("patch: %d", code)
	}
	if res["state"] == "applied" {
		t.Fatal("uncategorized expense must not file")
	}
	if fe, _ := res["fileError"].(string); !strings.Contains(fe, "no category") {
		t.Fatalf("fileError = %q, want the no-category reason", res["fileError"])
	}
}

// A split slice left at $0 must refuse to file (the live 2026-09-03 masonry
// filing: the editor seeds the second row at 0, and 12750+0 still sums, so a
// 50/50 intent filed as 12750/0 — the Σ check alone can't see it).
func TestSplitRefusesZeroSlice(t *testing.T) {
	bridge := &stubBridge{txns: map[string][]bankfeed.Txn{"act-1": {
		{ID: "t1", Posted: time.Now().AddDate(0, 0, -4), Amount: -12750, Description: "CHECK 1001", Payee: "Twisted Brick"},
	}}}
	srv, vault, _ := bankFixture(t, bridge)
	if _, _, err := srv.bankFeedSync(context.Background()); err != nil {
		t.Fatal(err)
	}
	rows, _ := srv.statements.List()
	code, res := doJSON(t, srv.handleStatementsRow, "POST", "/api/realestate/statements/row",
		`{"id":"`+rows[0].ID+`","category":"labor","state":"split","file":true,"assignments":[`+
			`{"slug":"748-n-euclid","amount":12750},`+
			`{"slug":"4852-fountain-ave","amount":0}]}`)
	if code != 200 {
		t.Fatalf("patch: %d", code)
	}
	if res["state"] == "applied" {
		t.Fatal("a $0 slice must not file")
	}
	if fe, _ := res["fileError"].(string); !strings.Contains(fe, "$0 slice") {
		t.Fatalf("fileError = %q, want the $0-slice reason", res["fileError"])
	}
	if _, err := os.ReadFile(filepath.Join(vault, "system/realestate/properties/748-n-euclid.ledger.csv")); err == nil {
		t.Fatal("refused filing must write no ledger rows")
	}
}

// Renaming a chart-of-accounts category sweeps everywhere the name lives:
// the registry, written ledger rows, and workbench rows. Renaming onto an
// existing name is refused (never a silent merge).
func TestCategoryRenameSweeps(t *testing.T) {
	bridge := &stubBridge{txns: map[string][]bankfeed.Txn{"act-1": {
		{ID: "t1", Posted: time.Now().AddDate(0, 0, -4), Amount: -500, Description: "CHECK 9", Payee: "Crew Co"},
		{ID: "t2", Posted: time.Now().AddDate(0, 0, -3), Amount: -80, Description: "CHECK 10", Payee: "Crew Co"},
	}}}
	srv, vault, _ := bankFixture(t, bridge)
	if _, _, err := srv.bankFeedSync(context.Background()); err != nil {
		t.Fatal(err)
	}
	rows, _ := srv.statements.List()
	byAmount := map[float64]string{}
	for _, r := range rows {
		byAmount[r.Amount] = r.ID
	}
	// file the $500 row under "windows" (a written ledger row); leave the $80
	// row categorized but unfiled (a workbench row)
	code, res := doJSON(t, srv.handleStatementsRow, "POST", "/api/realestate/statements/row",
		`{"id":"`+byAmount[500]+`","category":"windows","file":true,"assignments":[{"slug":"748-n-euclid","amount":500}]}`)
	if code != 200 || res["state"] != "applied" {
		t.Fatalf("file: %d %v (%v)", code, res["state"], res["fileError"])
	}
	code, _ = doJSON(t, srv.handleStatementsRow, "POST", "/api/realestate/statements/row",
		`{"id":"`+byAmount[80]+`","category":"windows"}`)
	if code != 200 {
		t.Fatalf("categorize: %d", code)
	}

	code, res = doJSON(t, srv.handleCategoryRename, "POST", "/api/realestate/categories/rename",
		`{"from":"windows","to":"crew"}`)
	if code != 200 {
		t.Fatalf("rename: %d %v", code, res)
	}
	if res["ledgerRows"] != float64(1) || res["workbenchRows"] != float64(2) {
		t.Fatalf("counts: ledgerRows=%v workbenchRows=%v", res["ledgerRows"], res["workbenchRows"])
	}
	led, err := os.ReadFile(filepath.Join(vault, "system/realestate/properties/748-n-euclid.ledger.csv"))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(led), "windows") || !strings.Contains(string(led), ",crew,") {
		t.Fatalf("ledger not renamed:\n%s", led)
	}
	reg, err := os.ReadFile(filepath.Join(vault, "system/realestate/categories.md"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(reg), "crew | expense | project") || strings.Contains(string(reg), `"windows`) {
		t.Fatalf("registry not renamed:\n%s", reg)
	}
	after, _ := srv.statements.List()
	for _, r := range after {
		if r.Category == "windows" {
			t.Fatal("workbench row kept the old name")
		}
	}
	// renaming onto an existing name is refused
	if code, _ = doJSON(t, srv.handleCategoryRename, "POST", "/api/realestate/categories/rename",
		`{"from":"materials","to":"crew"}`); code == 200 {
		t.Fatal("rename onto an existing category must be refused")
	}
}

// An acquisition-class category (chart of accounts) writes [cat:: acquisition]
// so the row lands in the property's acquisition line, not the hard lane.
func TestAcquisitionClassWritesToken(t *testing.T) {
	bridge := &stubBridge{txns: map[string][]bankfeed.Txn{"act-1": {
		{ID: "t1", Posted: time.Now().AddDate(0, 0, -2), Amount: -18297.84, Description: "WIRE OUT", Payee: "Title Co"},
	}}}
	srv, vault, _ := bankFixture(t, bridge)
	if _, _, err := srv.bankFeedSync(context.Background()); err != nil {
		t.Fatal(err)
	}
	rows, _ := srv.statements.List()
	code, res := doJSON(t, srv.handleStatementsRow, "POST", "/api/realestate/statements/row",
		`{"id":"`+rows[0].ID+`","category":"closing","file":true,"assignments":[{"slug":"748-n-euclid","amount":18297.84}]}`)
	if code != 200 || res["state"] != "applied" {
		t.Fatalf("file: %d %v (%v)", code, res["state"], res["fileError"])
	}
	led, err := os.ReadFile(filepath.Join(vault, "system/realestate/properties/748-n-euclid.ledger.csv"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(led), "[cat:: acquisition]") {
		t.Fatalf("acquisition token missing:\n%s", led)
	}
}

// A cross-entity mirror pair (same amount, opposite direction, shared bank
// REF) is suggested on the list, and one approval books BOTH sides into their
// entity admin ledgers as transfers. A non-pair is refused.
func TestTransferMatchSuggestsAndLinksBothSides(t *testing.T) {
	day := time.Now().AddDate(0, 0, -2)
	bridge := &stubBridge{txns: map[string][]bankfeed.Txn{
		"act-1": {{ID: "d1", Posted: day, Amount: 12750,
			Description: "Online banking Deposit Transfer from LP OODA Development fund I REF # 1662260"}},
		"act-2": {{ID: "w1", Posted: day.AddDate(0, 0, -1), Amount: -12750,
			Description: "Online banking Withdrawal Transfer to THE GARDEN SPE LLC REF # 1662260"}},
	}}
	srv, vault, _ := bankFixture(t, bridge)
	entPath := filepath.Join(vault, "system/realestate/entities/ooda-fund.md")
	if err := os.WriteFile(entPath, []byte("---\ncategories: [entity]\nname: OODA Fund I\n---\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := srv.index.Rebuild(); err != nil {
		t.Fatal(err)
	}
	if err := srv.bankFeed.Store().Upsert(bankfeed.Link{
		SimplefinID: "act-2", EntitySlug: "ooda-fund", AccountLabel: "checking", Enabled: true,
	}); err != nil {
		t.Fatal(err)
	}
	if _, _, err := srv.bankFeedSync(context.Background()); err != nil {
		t.Fatal(err)
	}

	code, res := doJSON(t, srv.handleStatementsList, "GET", "/api/realestate/statements", "")
	if code != 200 {
		t.Fatalf("list: %d", code)
	}
	ids := map[string]string{} // entity → row id
	peers := map[string]string{}
	for _, raw := range res["rows"].([]any) {
		row := raw.(map[string]any)
		ent, _ := row["entity"].(string)
		ids[ent] = row["id"].(string)
		if tr, ok := row["transfer"].(map[string]any); ok {
			peers[ent] = tr["peerId"].(string)
			if why, _ := tr["why"].(string); !strings.Contains(why, "REF") {
				t.Fatalf("match should cite the shared REF, got %q", why)
			}
		}
	}
	if peers["garden-spe"] != ids["ooda-fund"] || peers["ooda-fund"] != ids["garden-spe"] {
		t.Fatalf("pair not mutual: ids=%v peers=%v", ids, peers)
	}

	code, res = doJSON(t, srv.handleStatementsLinkTransfer, "POST", "/api/realestate/statements/link-transfer",
		`{"id":"`+ids["garden-spe"]+`","peerId":"`+ids["ooda-fund"]+`"}`)
	if code != 200 || res["linked"] != true {
		t.Fatalf("link: %d %v", code, res)
	}
	garden, err := os.ReadFile(filepath.Join(vault, "system/realestate/entities/garden-spe.ledger.csv"))
	if err != nil {
		t.Fatal(err)
	}
	ooda, err := os.ReadFile(filepath.Join(vault, "system/realestate/entities/ooda-fund.ledger.csv"))
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"income", "transfer", "[xfer:: ooda-fund]", "12750"} {
		if !strings.Contains(string(garden), want) {
			t.Fatalf("garden admin ledger missing %q:\n%s", want, garden)
		}
	}
	for _, want := range []string{"expense", "transfer", "[xfer:: garden-spe]", "12750"} {
		if !strings.Contains(string(ooda), want) {
			t.Fatalf("ooda admin ledger missing %q:\n%s", want, ooda)
		}
	}
	for _, id := range ids {
		if row, _ := srv.statements.Get(id); row.State != "applied" {
			t.Fatalf("row %s = %s, want applied", id, row.State)
		}
	}
	// an already-booked row can never link again
	if code, _ = doJSON(t, srv.handleStatementsLinkTransfer, "POST", "/api/realestate/statements/link-transfer",
		`{"id":"`+ids["garden-spe"]+`","peerId":"`+ids["ooda-fund"]+`"}`); code == 200 {
		t.Fatal("linking applied rows must be refused")
	}
}

// refilePost drives handleStatementsRefile with the {id} path value set.
func refilePost(t *testing.T, srv *Server, id, body string) (int, map[string]any) {
	t.Helper()
	req := httptest.NewRequest("POST", "/api/realestate/statements/"+id+"/refile", strings.NewReader(body))
	req.SetPathValue("id", id)
	rec := httptest.NewRecorder()
	srv.handleStatementsRefile(rec, req)
	var out map[string]any
	_ = json.Unmarshal(rec.Body.Bytes(), &out)
	return rec.Code, out
}

// The filed-edit lane (owner call 2026-08-19): category/note/bid links on a
// FILED row rewrite the written ledger row(s) in place; unfile deletes them
// and returns the row to the lot. Splits edit every slice.
func TestRefileEditsAndUnfilesWrittenRows(t *testing.T) {
	bridge := &stubBridge{txns: map[string][]bankfeed.Txn{"act-1": {
		{ID: "t1", Posted: time.Now().AddDate(0, 0, -4), Amount: -10131, Description: "CHECK 108", Payee: "Tree Court"},
	}}}
	srv, vault, _ := bankFixture(t, bridge)
	if _, _, err := srv.bankFeedSync(context.Background()); err != nil {
		t.Fatal(err)
	}
	rows, _ := srv.statements.List()
	id := rows[0].ID
	// file as a 2-way split
	code, res := doJSON(t, srv.handleStatementsRow, "POST", "/api/realestate/statements/row",
		`{"id":"`+id+`","category":"windows","note":"tree court check","state":"split","file":true,"assignments":[`+
			`{"slug":"748-n-euclid","amount":5000},{"slug":"4852-fountain-ave","amount":5131}]}`)
	if code != 200 || res["state"] != "applied" {
		t.Fatalf("file: %d %v (%v)", code, res["state"], res["fileError"])
	}

	// edit in place: category + a bid link on slice 1 (the written rows rewrite)
	code, res = refilePost(t, srv, id,
		`{"category":"materials","assignments":[`+
			`{"slug":"748-n-euclid","amount":5000,"workId":"shell/roof","contract":"olga-drawings"},`+
			`{"slug":"4852-fountain-ave","amount":5131}]}`)
	if code != 200 {
		t.Fatalf("refile: %d %v", code, res)
	}
	led1, _ := os.ReadFile(filepath.Join(vault, "system/realestate/properties/748-n-euclid.ledger.csv"))
	if !strings.Contains(string(led1), "materials") || !strings.Contains(string(led1), "[contract:: olga-drawings]") ||
		!strings.Contains(string(led1), "[work:: shell/roof]") {
		t.Fatalf("refile didn't rewrite slice 1:\n%s", led1)
	}
	if strings.Contains(string(led1), "windows") {
		t.Fatalf("old category survived the rewrite:\n%s", led1)
	}
	led2, _ := os.ReadFile(filepath.Join(vault, "system/realestate/properties/4852-fountain-ave.ledger.csv"))
	if !strings.Contains(string(led2), "materials") || strings.Contains(string(led2), "[contract::") {
		t.Fatalf("slice 2 wrong after refile:\n%s", led2)
	}

	// identity is immutable through refile — moving money means unfile
	code, _ = refilePost(t, srv, id,
		`{"assignments":[{"slug":"748-n-euclid","amount":9000},{"slug":"4852-fountain-ave","amount":1131}]}`)
	if code == 200 {
		t.Fatal("amount change must be refused (unfile first)")
	}

	// unfile: both written rows deleted, the row returns to the lot assigned
	code, res = refilePost(t, srv, id, `{"unfile":true}`)
	if code != 200 || res["unfiled"] != true {
		t.Fatalf("unfile: %d %v", code, res)
	}
	led1, _ = os.ReadFile(filepath.Join(vault, "system/realestate/properties/748-n-euclid.ledger.csv"))
	led2, _ = os.ReadFile(filepath.Join(vault, "system/realestate/properties/4852-fountain-ave.ledger.csv"))
	if strings.Contains(string(led1), "Tree Court") || strings.Contains(string(led2), "Tree Court") {
		t.Fatalf("unfile left ledger rows behind:\n%s\n%s", led1, led2)
	}
	row, _ := srv.statements.Get(id)
	if row.State != "assigned" || len(row.Assignments) != 2 {
		t.Fatalf("unfiled row: %+v", row)
	}
	// and it refiles cleanly (the round trip)
	code, res = doJSON(t, srv.handleStatementsRow, "POST", "/api/realestate/statements/row",
		`{"id":"`+id+`","file":true}`)
	if code != 200 || res["state"] != "applied" {
		t.Fatalf("refile after unfile: %d %v (%v)", code, res["state"], res["fileError"])
	}
}
