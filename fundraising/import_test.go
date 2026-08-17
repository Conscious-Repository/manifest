package fundraising

import (
	"encoding/csv"
	"strings"
	"testing"
)

func TestSheetImportCountMergeAndConservativeMapping(t *testing.T) {
	var b strings.Builder
	w := csv.NewWriter(&b)
	_ = w.Write([]string{"Firm", "Warm Contact", "Last Touchoint", "Next Step", "Interest Level", "Notes"})
	for i := 0; i < 50; i++ {
		row := []string{"Firm " + string(rune('A'+i%26)) + " " + string(rune('a'+i/26)), "", "Call", "Follow up", "", ""}
		if i == 0 {
			row = []string{"8VC", "Drew", "intro call", "Follow-up", "", ""}
		}
		if i == 1 {
			row = []string{"Angel/LP", "A Person", "450k confirmed", "", "Closed", ""}
		}
		if i == 2 {
			row = []string{"Angel/LP", "B Person", "Call", "100k", "Closed", ""}
		}
		if i == 3 {
			row = []string{"Pass Co", "", "", "", "Pass", ""}
		}
		_ = w.Write(row)
	}
	_ = w.Write([]string{"HitList"})
	for i := 0; i < 13; i++ {
		row := []string{"Hit " + string(rune('A'+i))}
		if i == 0 {
			row = []string{"8vc", "Francisco Gimenez"}
		}
		_ = w.Write(row)
	}
	_ = w.Write([]string{"Resources"})
	_ = w.Write([]string{"https://www.blackflag.vc/investors"})
	w.Flush()
	rows, res, err := ReadSheetCSV(strings.NewReader(b.String()))
	if err != nil {
		t.Fatal(err)
	}
	ops := NormalizeSheet(rows, func(name string) (PersonRef, bool) {
		if name == "Drew" {
			return PersonRef{Key: "drew", Display: "Drew", NotePath: "drew.md"}, true
		}
		return PersonRef{}, false
	})
	if len(rows) != 63 || len(ops) != 62 || len(res) != 1 {
		t.Fatalf("rows=%d ops=%d resources=%d", len(rows), len(ops), len(res))
	}
	var eight *Opportunity
	angels := 0
	for i := range ops {
		if strings.EqualFold(ops[i].Firm, "8vc") {
			eight = &ops[i]
		}
		if ops[i].Firm == "Angel/LP" {
			angels++
		}
	}
	if eight == nil || len(eight.SourceRows) != 2 || len(eight.People) != 1 {
		t.Fatalf("8VC merge=%+v", eight)
	}
	if eight.Source == nil || eight.Source.Contact != nil || eight.Source.Text != "Drew; Francisco Gimenez" {
		t.Fatalf("8VC source=%+v", eight.Source)
	}
	if angels != 2 {
		t.Fatalf("Angel/LP rows merged unexpectedly: %d", angels)
	}
	if ops[1].Status != StatusCommitted || ops[1].Amount != 450000 {
		t.Fatalf("clear closed mapping=%+v", ops[1])
	}
	if ops[2].Status != StatusActive || !ops[2].ImportReview || ops[2].Amount != 100000 {
		t.Fatalf("ambiguous closed mapping=%+v", ops[2])
	}
	if ops[3].Status != StatusPassed {
		t.Fatalf("pass mapping=%+v", ops[3])
	}
}

func TestAmountParserRejectsValuationAndBareNumbers(t *testing.T) {
	if v, _ := clearAmount("Angel check at 24m - 25", false); v != 0 {
		t.Fatalf("valuation parsed as amount: %v", v)
	}
	if v, _ := clearAmount("24m pre-money valuation", false); v != 0 {
		t.Fatalf("pre-money valuation parsed as amount: %v", v)
	}
	if v, _ := clearAmount("valuation of 30m", false); v != 0 {
		t.Fatalf("valuation phrase parsed as amount: %v", v)
	}
	if v, _ := clearAmount("20", false); v != 0 {
		t.Fatalf("bare number parsed: %v", v)
	}
	if v, _ := clearAmount("Wants to invest 10k", false); v != 10000 {
		t.Fatalf("10k=%v", v)
	}
}

func TestImportUpsertIsIdempotent(t *testing.T) {
	s, _ := testStore(t)
	op := Opportunity{
		Firm:       "Repeat Ventures",
		Status:     StatusActive,
		Interest:   InterestMedium,
		Currency:   "USD",
		People:     []PersonRef{},
		SourceRows: []int{17},
	}
	first, err := s.ImportUpsert(op)
	if err != nil {
		t.Fatal(err)
	}
	op.NextStep = "Updated on rerun"
	second, err := s.ImportUpsert(op)
	if err != nil {
		t.Fatal(err)
	}
	all, err := s.List()
	if err != nil {
		t.Fatal(err)
	}
	if len(all) != 1 || first.ID != second.ID || all[0].NextStep != "Updated on rerun" {
		t.Fatalf("idempotent import first=%+v second=%+v all=%+v", first, second, all)
	}
}
