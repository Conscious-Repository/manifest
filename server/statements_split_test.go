package server

import (
	"context"
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
