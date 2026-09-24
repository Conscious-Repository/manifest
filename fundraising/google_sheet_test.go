package fundraising

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// Schemas 2–4 dropped Interest, Currency and the computed touch column: a
// record must survive the cell round trip in thirteen columns with the Sync
// flag last, the migration chain must walk 1 → 4, and only the Sync column
// is ignored when deciding whether a row carries content.
func TestSheetCellsRoundTripSchemaFour(t *testing.T) {
	if len(sheetHeaders) != sheetColumnCount || sheetColumnCount != 13 {
		t.Fatalf("headers=%d columns=%d", len(sheetHeaders), sheetColumnCount)
	}
	for _, h := range sheetHeaders {
		if h == "Interest" || h == "Currency" || h == "Computed Last Touchpoint" {
			t.Fatalf("%s column is still in the schema", h)
		}
	}
	version, hops := "1", 0
	for version != sheetSchemaValue {
		step, ok := sheetMigrations[version]
		if !ok || hops > 10 {
			t.Fatalf("migration chain breaks at %q: %+v", version, sheetMigrations)
		}
		version, hops = step.Next, hops+1
	}
	if hops != 3 {
		t.Fatalf("expected three migration steps, walked %d", hops)
	}
	if got := sheetMigrations["3"].Rename[7]; got != "Last Touch Date" {
		t.Fatalf("schema 4 header rename = %q", got)
	}
	record := SharedOpportunity{
		Firm: "Fund", Website: "https://fund.example", People: []string{"A Person"}, Source: "DM",
		Status: StatusActive, Amount: 250000, LastTouchpoint: "call", LastTouchpointDate: "2026-09-01",
		NextStep: "follow up", NextStepDue: "2026-09-10", Notes: "n", Archived: true,
	}
	cells := sharedCells(record, "synced")
	if len(cells) != sheetColumnCount {
		t.Fatalf("cells=%d", len(cells))
	}
	row := make([]any, 0, len(cells))
	for i, c := range cells {
		switch {
		case c.UserEnteredValue == nil:
			row = append(row, nil)
		case c.UserEnteredValue.NumberValue != nil && i == 5:
			row = append(row, *c.UserEnteredValue.NumberValue)
		case c.UserEnteredValue.BoolValue != nil:
			row = append(row, *c.UserEnteredValue.BoolValue)
		case c.UserEnteredValue.StringValue != nil:
			row = append(row, *c.UserEnteredValue.StringValue)
		default:
			row = append(row, nil)
		}
	}
	// Date cells travel as serials; feed the formatted strings back the way
	// the Values API renders them.
	row[7], row[9] = "2026-09-01", "2026-09-10"
	got := sharedFromCells(row)
	if !sharedEqual(got, record) {
		t.Fatalf("round trip\n got=%+v\nwant=%+v", got, record)
	}
	if cellString(row, sheetColumnCount-1) != "synced" {
		t.Fatalf("sync column = %q", cellString(row, sheetColumnCount-1))
	}
	blank := make([]any, sheetColumnCount)
	blank[sheetColumnCount-1] = "synced"
	if cellsHaveContent(blank) {
		t.Fatal("a row with only a Sync flag counts as content")
	}
	blank[11] = false
	if !cellsHaveContent(blank) {
		t.Fatal("an Archived flag is content")
	}
}

// A state file written under an older schema names "interest" and
// "currency" in every base map; loading it must drop the keys so a retired
// field never surfaces as a conflict against a sheet that no longer has the
// column.
func TestSheetSyncLoadDropsRetiredFields(t *testing.T) {
	path := filepath.Join(t.TempDir(), "state.json")
	state := syncDiskState{Version: 1, Records: map[string]syncRecordState{
		"fr/fund": {Base: map[string]string{"firm": "Fund", "interest": "high", "currency": "USD"}, Conflicts: map[string]SyncConflict{"interest": {Field: "interest"}, "currency": {Field: "currency"}}},
	}}
	b, _ := json.Marshal(state)
	if err := os.WriteFile(path, b, 0o600); err != nil {
		t.Fatal(err)
	}
	s := &SheetSync{statePath: path}
	if err := s.load(); err != nil {
		t.Fatal(err)
	}
	rec := s.state.Records["fr/fund"]
	for _, retired := range []string{"interest", "currency"} {
		if _, ok := rec.Base[retired]; ok {
			t.Fatalf("base still carries %s: %+v", retired, rec.Base)
		}
		if _, ok := rec.Conflicts[retired]; ok {
			t.Fatalf("conflicts still carry %s: %+v", retired, rec.Conflicts)
		}
	}
	if rec.Base["firm"] != "Fund" {
		t.Fatalf("firm lost: %+v", rec.Base)
	}
}

// A workbook at a version this build does not know is never read with this
// build's column map; the sync refuses instead of pulling shifted cells.
func TestUnknownSheetSchemaIsRefused(t *testing.T) {
	g := &GoogleSheetBackend{}
	if err := g.migrateSchema(context.Background(), schemaMark{Found: true, Version: "99"}); err == nil || !strings.Contains(err.Error(), "schema \"99\"") {
		t.Fatalf("a newer schema must be refused: %v", err)
	}
	if err := g.migrateSchema(context.Background(), schemaMark{Found: true, Version: sheetSchemaValue}); err != nil {
		t.Fatalf("the current schema needs no step: %v", err)
	}
}
