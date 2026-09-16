package personalemail

import (
	"context"
	"encoding/json"
	"manifest/approvals"
	"strings"
	"testing"
)

const legacy = `{"watermark":"2026-09-01T00:00:00Z","threads":{"t1":{"status":"proposed","proposal_id":"p1","last_msg_id":"m1","last_internal_ms":123,"filename":"PRIVATE SUBJECT"}}}`

func inventory() approvals.ConnectorInventory {
	return approvals.ConnectorInventory{Hash: strings.Repeat("a", 64), Items: []approvals.ConnectorApproval{{ID: "p1", Source: "gmail-thread", SourceID: "t1", Status: "pending", Type: approvals.TypeCreateVaultNote, Hash: strings.Repeat("b", 64)}}}
}

type reader func(context.Context, string, string, int64) error

func (f reader) ThreadAnchor(c context.Context, t, m string, n int64) error { return f(c, t, m, n) }
func TestContinuityQuarantineAndCleanReads(t *testing.T) {
	for _, tc := range []struct {
		name, raw string
		mutate    func(*approvals.ConnectorInventory)
		clean     bool
	}{
		{"clean", legacy, nil, true},
		{"wrong-thread", legacy, func(i *approvals.ConnectorInventory) { i.Items[0].SourceID = "t2" }, false},
		{"missing", legacy, func(i *approvals.ConnectorInventory) { i.Items = nil }, false},
		{"duplicate-proposal", legacy, func(i *approvals.ConnectorInventory) { i.Items = append(i.Items, i.Items[0]) }, false},
		{"duplicate-thread", legacy, func(i *approvals.ConnectorInventory) { p := i.Items[0]; p.ID = "p2"; i.Items = append(i.Items, p) }, false},
		{"duplicate-state-reference", strings.Replace(legacy, `"threads":{`, `"threads":{"t2":{"status":"proposed","proposal_id":"p1"},`, 1), nil, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			inv := inventory()
			if tc.mutate != nil {
				tc.mutate(&inv)
			}
			reads := 0
			r, err := Check(context.Background(), Options{Enabled: true, Account: "owner@example.com"}, []byte(tc.raw), inv, func(_ context.Context, a string) (AnchorReader, error) {
				if a != "owner@example.com" {
					t.Fatal(a)
				}
				return reader(func(_ context.Context, id, msg string, ms int64) error {
					reads++
					if id != "t1" || msg != "m1" || ms != 123 {
						t.Fatal(id, msg, ms)
					}
					return nil
				}), nil
			})
			if err != nil {
				t.Fatal(err)
			}
			if r.Identity.IdentityComplete != tc.clean || (reads == 1) != tc.clean {
				t.Fatalf("%+v reads=%d", r, reads)
			}
			if tc.clean && string(r.Identity.ImportedLegacy) != tc.raw {
				t.Fatal("import not verbatim")
			}
			if !tc.clean && r.Identity.ImportedLegacy != nil {
				t.Fatal("ambiguous raw imported")
			}
			b, _ := json.Marshal(r)
			if strings.Contains(string(b), "PRIVATE") || strings.Contains(string(b), "owner@example.com") || strings.Contains(string(b), `"replay":true`) {
				t.Fatal(string(b))
			}
			for _, row := range r.Identity.Threads {
				if !tc.clean && (row.Disposition != approvals.ReconciledUncertain || len(row.StopReasons) == 0) {
					t.Fatalf("not quarantined: %+v", row)
				}
			}
		})
	}
}
func TestDefaultOffAndInvalidJSON(t *testing.T) {
	open := func(context.Context, string) (AnchorReader, error) {
		t.Fatal("disabled opened credentials")
		return nil, nil
	}
	if _, err := Check(context.Background(), Options{Account: "a"}, []byte(legacy), inventory(), open); err != nil {
		t.Fatal(err)
	}
	for _, s := range []string{legacy + `{}`, strings.Replace(legacy, `"threads":`, `"threads":{},"threads":`, 1)} {
		if _, err := Check(context.Background(), Options{Account: "a"}, []byte(s), inventory(), open); err == nil {
			t.Fatal("accepted duplicate/trailing state")
		}
	}
}
func TestMismatchDoesNotBlockIndependentCleanThread(t *testing.T) {
	raw := strings.Replace(legacy, `"threads":{`, `"threads":{"bad":{"status":"proposed","proposal_id":"wrong"},`, 1)
	r, err := Check(context.Background(), Options{Enabled: true, Account: "a"}, []byte(raw), inventory(), func(context.Context, string) (AnchorReader, error) {
		return reader(func(context.Context, string, string, int64) error { return nil }), nil
	})
	if err != nil || r.Identity.IdentityComplete || len(r.Observations) != 2 || r.Observations[0].Status != "quarantined" || r.Observations[1].Status != "anchor-verified" {
		t.Fatal(r, err)
	}
}
