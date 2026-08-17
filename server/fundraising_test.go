package server

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"manifest/aion"
	"manifest/fundraising"
)

func testFundraisingStore(t *testing.T) *fundraising.Store {
	t.Helper()
	root := t.TempDir()
	write := func(abs string, b []byte) error {
		if err := os.MkdirAll(filepath.Dir(abs), 0o755); err != nil {
			return err
		}
		return os.WriteFile(abs, b, 0o644)
	}
	s := fundraising.NewStore(root, "system/crm/fundraising", "system/crm/contacts.md", write, write)
	if err := s.Ensure(); err != nil {
		t.Fatal(err)
	}
	return s
}

func TestFundraisingPrivateCRUD(t *testing.T) {
	store := testFundraisingStore(t)
	s := &Server{fundraising: store}

	create := httptest.NewRequest(http.MethodPost, "/api/aion/fundraising/item", strings.NewReader(`{"firm":"Acme Ventures"}`))
	created := httptest.NewRecorder()
	s.handleFundraisingCreate(created, create)
	if created.Code != http.StatusOK {
		t.Fatalf("create status=%d body=%s", created.Code, created.Body.String())
	}
	var view struct {
		Opportunities []fundraising.Opportunity `json:"opportunities"`
	}
	if err := json.Unmarshal(created.Body.Bytes(), &view); err != nil || len(view.Opportunities) != 1 {
		t.Fatalf("create response err=%v view=%+v", err, view)
	}
	id := view.Opportunities[0].ID

	update := httptest.NewRequest(http.MethodPost, "/api/aion/fundraising/update/"+id, strings.NewReader(`{"status":"active","nextStep":"Send deck"}`))
	update.SetPathValue("id", id)
	updated := httptest.NewRecorder()
	s.handleFundraisingUpdate(updated, update)
	if updated.Code != http.StatusOK {
		t.Fatalf("update status=%d body=%s", updated.Code, updated.Body.String())
	}
	op, ok := store.Get(id)
	if !ok || op.Status != fundraising.StatusActive || op.NextStep != "Send deck" {
		t.Fatalf("updated opportunity=%+v ok=%v", op, ok)
	}

	archive := httptest.NewRequest(http.MethodPost, "/api/aion/fundraising/archive/"+id, strings.NewReader(`{"archived":true}`))
	archive.SetPathValue("id", id)
	archived := httptest.NewRecorder()
	s.handleFundraisingArchive(archived, archive)
	op, _ = store.Get(id)
	if archived.Code != http.StatusOK || !op.Archived {
		t.Fatalf("archive status=%d opportunity=%+v", archived.Code, op)
	}
}

func TestFundraisingExcludedFromGlobalAionContract(t *testing.T) {
	for _, path := range aion.ContractPaths() {
		if strings.Contains(strings.ToLower(path), "fundrais") || strings.Contains(strings.ToLower(path), "crm") {
			t.Fatalf("private fundraising path leaked into global Aion contract: %s", path)
		}
	}
}
