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

	update := httptest.NewRequest(http.MethodPost, "/api/aion/fundraising/update/"+id, strings.NewReader(`{"status":"active","nextStep":"Send deck","website":"acme.vc","source":{"text":"DM"}}`))
	update.SetPathValue("id", id)
	updated := httptest.NewRecorder()
	s.handleFundraisingUpdate(updated, update)
	if updated.Code != http.StatusOK {
		t.Fatalf("update status=%d body=%s", updated.Code, updated.Body.String())
	}
	if strings.Contains(updated.Body.String(), "introVia") {
		t.Fatalf("legacy introVia leaked into response: %s", updated.Body.String())
	}
	op, ok := store.Get(id)
	if !ok || op.Status != fundraising.StatusActive || op.NextStep != "Send deck" || op.Website != "https://acme.vc" || op.Source == nil || op.Source.Text != "DM" {
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

	remove := httptest.NewRequest(http.MethodPost, "/api/aion/fundraising/delete/"+id, nil)
	remove.SetPathValue("id", id)
	deleted := httptest.NewRecorder()
	s.handleFundraisingDelete(deleted, remove)
	if deleted.Code != http.StatusOK {
		t.Fatalf("delete status=%d body=%s", deleted.Code, deleted.Body.String())
	}
	if _, ok := store.Get(id); ok {
		t.Fatal("deleted opportunity remains in fundraising view")
	}
}

func TestFundraisingPeopleCanCreateAndLinkMultipleContacts(t *testing.T) {
	store := testFundraisingStore(t)
	s := &Server{fundraising: store}
	op, err := store.Create("Multi Person Fund")
	if err != nil {
		t.Fatal(err)
	}
	for _, body := range []string{
		`{"key":"jane doe","display":"Jane Doe"}`,
		`{"key":"john smith","display":"John Smith"}`,
	} {
		req := httptest.NewRequest(http.MethodPost, "/api/aion/fundraising/person/"+op.ID, strings.NewReader(body))
		req.SetPathValue("id", op.ID)
		w := httptest.NewRecorder()
		s.handleFundraisingPersonAdd(w, req)
		if w.Code != http.StatusOK {
			t.Fatalf("add person status=%d body=%s", w.Code, w.Body.String())
		}
	}
	got, _ := store.Get(op.ID)
	if len(got.People) != 2 || got.People[0].Key != "jane doe" || got.People[1].Key != "john smith" {
		t.Fatalf("people=%+v", got.People)
	}
	if _, ok := store.RegistryPerson("jane doe"); !ok {
		t.Fatal("typed person was not created in CRM registry")
	}
}

func TestFundraisingExcludedFromGlobalAionContract(t *testing.T) {
	for _, path := range aion.ContractPaths() {
		if strings.Contains(strings.ToLower(path), "fundrais") || strings.Contains(strings.ToLower(path), "crm") {
			t.Fatalf("private fundraising path leaked into global Aion contract: %s", path)
		}
	}
}
