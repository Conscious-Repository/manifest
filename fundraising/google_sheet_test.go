package fundraising

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

// Schemas 2 and 3 dropped the Interest and Currency columns: a record must
// survive the cell round trip in fourteen columns with the Sync flag last,
// and only the Sync column is ignored when deciding whether a row carries
// content.
func TestSheetCellsRoundTripSchemaThree(t *testing.T) {
	if len(sheetHeaders) != sheetColumnCount || sheetColumnCount != 14 {
		t.Fatalf("headers=%d columns=%d", len(sheetHeaders), sheetColumnCount)
	}
	for _, h := range sheetHeaders {
		if h == "Interest" || h == "Currency" {
			t.Fatalf("%s column is still in the schema", h)
		}
	}
	if sheetMigrations["1"].Next != "2" || sheetMigrations["2"].Next != sheetSchemaValue {
		t.Fatalf("migration chain does not reach %s: %+v", sheetSchemaValue, sheetMigrations)
	}
	record := SharedOpportunity{
		Firm: "Fund", Website: "https://fund.example", People: []string{"A Person"}, Source: "DM",
		Status: StatusActive, Amount: 250000, LastTouchpoint: "call", LastTouchpointDate: "2026-09-01",
		ComputedLastTouchpoint: "2026-09-02", NextStep: "follow up", NextStepDue: "2026-09-10", Notes: "n", Archived: true,
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
	row[7], row[8], row[10] = "2026-09-01", "2026-09-02", "2026-09-10"
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
	blank[12] = false
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
