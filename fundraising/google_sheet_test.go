package fundraising

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

// Schema 2 dropped the Interest column: a record must survive the cell
// round trip in fifteen columns with the Sync flag last, and only the Sync
// column is ignored when deciding whether a row carries content.
func TestSheetCellsRoundTripSchemaTwo(t *testing.T) {
	if len(sheetHeaders) != sheetColumnCount || sheetColumnCount != 15 {
		t.Fatalf("headers=%d columns=%d", len(sheetHeaders), sheetColumnCount)
	}
	for _, h := range sheetHeaders {
		if h == "Interest" {
			t.Fatal("Interest column is still in the schema")
		}
	}
	record := SharedOpportunity{
		Firm: "Fund", Website: "https://fund.example", People: []string{"A Person"}, Source: "DM",
		Status: StatusActive, Amount: 250000, Currency: "USD", LastTouchpoint: "call", LastTouchpointDate: "2026-09-01",
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
	row[8], row[9], row[11] = "2026-09-01", "2026-09-02", "2026-09-10"
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
	blank[13] = false
	if !cellsHaveContent(blank) {
		t.Fatal("an Archived flag is content")
	}
}

// A state file written under schema 1 names "interest" in every base map;
// loading it must drop the key so the retired field never surfaces as a
// conflict against a sheet that no longer has the column.
func TestSheetSyncLoadDropsRetiredInterest(t *testing.T) {
	path := filepath.Join(t.TempDir(), "state.json")
	state := syncDiskState{Version: 1, Records: map[string]syncRecordState{
		"fr/fund": {Base: map[string]string{"firm": "Fund", "interest": "high"}, Conflicts: map[string]SyncConflict{"interest": {Field: "interest"}}},
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
	if _, ok := rec.Base["interest"]; ok {
		t.Fatalf("base still carries interest: %+v", rec.Base)
	}
	if _, ok := rec.Conflicts["interest"]; ok {
		t.Fatalf("conflicts still carry interest: %+v", rec.Conflicts)
	}
	if rec.Base["firm"] != "Fund" {
		t.Fatalf("firm lost: %+v", rec.Base)
	}
}
