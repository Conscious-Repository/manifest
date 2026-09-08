package server

import (
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
)

func TestContractDeleteUnusedDuplicate(t *testing.T) {
	f := oodaPortalFixtureFull(t)
	dir := filepath.Join(f.vault, "system/realestate/contracts")
	if err := os.MkdirAll(dir, 0755); err != nil {
		t.Fatal(err)
	}
	for _, status := range []string{"closed", "proposed", "accepted"} {
		raw := "---\ncategories: [contract]\nname: Duplicate\nstatus: " + status + "\ntotal: 5500\n---\n"
		if err := os.WriteFile(filepath.Join(dir, status+".md"), []byte(raw), 0644); err != nil {
			t.Fatal(err)
		}
	}
	s := oodaCockpitFor(t, f.vault, f.store)
	for _, status := range []string{"closed", "proposed", "accepted"} {
		req := httptest.NewRequest("DELETE", "/api/realestate/contracts/"+status, nil)
		req.SetPathValue("slug", status)
		rec := httptest.NewRecorder()
		s.handleContractDelete(rec, req)
		want := 200
		if status == "accepted" {
			want = 409
		}
		if rec.Code != want {
			t.Fatalf("%s: %d %s", status, rec.Code, rec.Body.String())
		}
		_, exists := s.realestate.GetContract(status)
		if exists != (status == "accepted") {
			t.Fatalf("unexpected retained status %s", status)
		}
	}
}
