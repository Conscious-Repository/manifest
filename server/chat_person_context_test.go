package server

import (
	"encoding/json"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"manifest/artifacts"
	"manifest/contacts"
)

type contextPeopleDirectory struct{}

func (contextPeopleDirectory) People() []contacts.CRMContact {
	return []contacts.CRMContact{{Key: "crm/alice", Display: "alice", Emails: []string{"crm@example.test"}}}
}
func (d contextPeopleDirectory) Person(key string) (contacts.CRMContact, bool) {
	p := d.People()[0]
	return p, key == p.Key
}
func (contextPeopleDirectory) AddEmail(string, string) error { panic("context must not edit contacts") }
func (contextPeopleDirectory) AttachNote(string, string) error {
	panic("context must not create notes")
}
func (contextPeopleDirectory) Fundraising(string) []contacts.FundraisingSummary {
	return []contacts.FundraisingSummary{{ID: "private-business", Firm: "EXCLUDED_FUNDRAISING", Amount: 1234}}
}

type contextPeopleCalendar struct{}

func (contextPeopleCalendar) PastMeetings(time.Time, int) []contacts.Event {
	return []contacts.Event{{Start: time.Date(2026, 9, 1, 12, 0, 0, 0, time.UTC), Title: "Verified meeting", Attendees: []contacts.Attendee{{Name: "alice", Email: "alice@example.test"}}}}
}
func (contextPeopleCalendar) Upcoming(now time.Time, _ int) []contacts.Event {
	return []contacts.Event{
		{Start: now.AddDate(0, 0, 1).Truncate(24 * time.Hour), Title: "Confirmed next meeting", Attendees: []contacts.Attendee{{Name: "alice", Email: "alice@example.test"}}},
		{Start: now.AddDate(0, 0, 2).Truncate(24 * time.Hour), Title: "Possible meeting", Attendees: []contacts.Attendee{{Name: "alice", Email: "unknown@example.test"}}},
	}
}

func personContextFixture(t *testing.T) (*Server, string) {
	t.Helper()
	s, root, _ := artifactFixture(t)
	notes := map[string]string{
		"alice.md":                 "---\ncategories: [people]\naliases: [Ally]\nemail: alice@example.test\nrole: scientist\nspecialization: spectroscopy\n---\nKNOWLEDGE_SPECIALIZATION\n[trust:: high]\nColleague of [[bob]] at [[acme]].\n",
		"2026-09-02 alice sync.md": "---\ncategories: [sync]\n---\n[[alice]]\nEXCLUDED_TRANSCRIPT_BODY\n## Next steps\n- [ ] Send reviewed outline\n",
		"system/aion/recruiting/candidates/candidate-alice.md": "---\ncategories: [people]\n---\nEXCLUDED_CANDIDATE\n",
	}
	paths := []string{}
	for path, body := range notes {
		p := filepath.Join(root, path)
		if err := os.MkdirAll(filepath.Dir(p), 0755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(body), 0644); err != nil {
			t.Fatal(err)
		}
		paths = append(paths, path)
	}
	if err := s.index.ReindexPaths(paths); err != nil {
		t.Fatal(err)
	}
	store, err := contacts.NewStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	s.contacts = contacts.New(s.index, store, s.vault, contextPeopleCalendar{}, nil)
	s.contacts.UseCRMDirectory(contextPeopleDirectory{})
	return s, root
}
func TestPersonContextIdentityEvidenceAndRetention(t *testing.T) {
	s, root := personContextFixture(t)
	code, out := artifactsDo(t, s, "GET", "/api/chat/records?kind=person&q=alice", "")
	if code != 200 {
		t.Fatal(code, out)
	}
	rows := out["records"].([]any)
	if len(rows) != 2 {
		t.Fatal("equal names merged or system person leaked", rows)
	}
	if rows[0].(map[string]any)["id"] == rows[1].(map[string]any)["id"] {
		t.Fatal(rows)
	}
	code, out = artifactsDo(t, s, "GET", "/api/chat/records?kind=person&q=ally", "")
	if code != 200 || len(out["records"].([]any)) != 1 || out["records"].([]any)[0].(map[string]any)["id"] != "alice" {
		t.Fatal("alias resolution", code, out)
	}
	raw, _ := os.ReadFile(filepath.Join(root, "alice.md"))
	preview := contextPreview(t, s, "person", "alice")
	body := preview["content"].(string)
	for _, want := range []string{string(raw), "Contact key: alice", "Last met (calendar email match): 2026-09-01", "Last mentioned (dated note): 2026-09-02", "Verified meeting", "confirmed email match", "Send reviewed outline"} {
		if !strings.Contains(body, want) {
			t.Fatal(want, body)
		}
	}
	for _, excluded := range []string{"EXCLUDED_TRANSCRIPT_BODY", "EXCLUDED_CANDIDATE", "EXCLUDED_FUNDRAISING", "crm@example.test"} {
		if strings.Contains(body, excluded) {
			t.Fatal("unselected source included", excluded)
		}
	}
	crm := contextPreview(t, s, "person", "crm/alice")
	if !strings.Contains(crm["content"].(string), "crm@example.test") || !strings.Contains(crm["content"].(string), "unconfirmed candidate match") || strings.Contains(crm["content"].(string), "KNOWLEDGE_SPECIALIZATION") {
		t.Fatal("same display name changed identity", crm)
	}
	if len(s.artifactReg.List(artifacts.Filter{})) != 0 {
		t.Fatal("search/preview registered context")
	}
	code, ref := contextRetain(t, s, "person", "alice", preview["revision"].(string))
	if code != 200 {
		t.Fatal(code, ref)
	}
	a, _ := s.artifactReg.Get(ref["id"].(string))
	if a.Provenance.Task != "" || a.Provenance.Source != "person-context" {
		t.Fatal(a)
	}
	links := s.artifactSourceLinks([]artifacts.Artifact{a})[a.ID]
	if len(links) != 1 || links[0].Kind != "person" || links[0].Route != "#/contacts/alice" {
		t.Fatal(links)
	}
	after, _ := os.ReadFile(filepath.Join(root, "alice.md"))
	if string(raw) != string(after) {
		t.Fatal("context edited person")
	}
	if code, retry := contextRetain(t, s, "person", "alice", preview["revision"].(string)); code != 200 || retry["id"] != ref["id"] {
		t.Fatal(code, retry)
	}
	if err := os.WriteFile(filepath.Join(root, "alice.md"), []byte(strings.Replace(string(raw), "high", "low", 1)), 0644); err != nil {
		t.Fatal(err)
	}
	if code, _ := contextRetain(t, s, "person", "alice", preview["revision"].(string)); code != 409 {
		t.Fatal("changed relationship value accepted", code)
	}
	refs := []artifactContextRef{{ID: a.ID, Revision: a.Head}}
	if _, err := s.selectedArtifactContext(false, "", "", refs, nil); err == nil {
		t.Fatal("implicit person context accepted")
	}
	ctx, err := s.selectedArtifactContext(true, "", "", refs, nil)
	if err != nil || !strings.Contains(ctx, body) || !strings.Contains(ctx, `source-person="alice"`) {
		t.Fatal(ctx, err)
	}
}

func TestPersonContextUnavailableProfilesAndSharedBoundary(t *testing.T) {
	s, root := personContextFixture(t)
	preview := contextPreview(t, s, "person", "alice")
	code, ref := contextRetain(t, s, "person", "alice", preview["revision"].(string))
	if code != 200 {
		t.Fatal(code, ref)
	}
	native, se, sends := terminalReceiptFixture(t, false, func(text string) {
		if !strings.Contains(text, preview["content"].(string)) || !strings.Contains(text, `source-person="alice"`) {
			t.Error("person context changed", text)
		}
	})
	native.artifactReg = s.artifactReg
	body, _ := json.Marshal(map[string]any{"text": "Discuss this person", "requestId": "receipt-input-001", "explicitArtifacts": true, "artifacts": []artifactContextRef{{ID: ref["id"].(string), Revision: ref["revision"].(string)}}})
	for i := 0; i < 2; i++ {
		if w := receiptInput(native, se.ID, string(body)); w.Code != 200 {
			t.Fatal(w.Code, w.Body.String())
		}
	}
	if sends.Load() != 1 {
		t.Fatal(sends.Load())
	}
	shared, sharedSE, _, sharedSends := sharedInputFixture(t, false)
	shared.artifactReg = s.artifactReg
	if w := receiptInput(shared, sharedSE.ID, string(body)); w.Code == 200 || sharedSends.Load() != 0 {
		t.Fatal("person context leaked to shared input", w.Code)
	}
	if err := os.Remove(filepath.Join(root, "alice.md")); err != nil {
		t.Fatal(err)
	}
	if code, _ := artifactsDo(t, s, "GET", "/api/chat/records/preview?kind=person&id=alice", ""); code == 200 {
		t.Fatal("missing profile treated as empty")
	}
	outside := filepath.Join(t.TempDir(), "secret.md")
	if err := os.WriteFile(outside, []byte("OUTSIDE_PRIVATE"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, filepath.Join(root, "alice.md")); err != nil {
		t.Fatal(err)
	}
	if code, out := artifactsDo(t, s, "GET", "/api/chat/records/preview?kind=person&id=alice", ""); code == 200 || strings.Contains(out["error"].(string), "OUTSIDE_PRIVATE") {
		t.Fatal("outside profile", code, out)
	}
	h, err := PortalHandler(PortalOptions{})
	if err != nil {
		t.Fatal(err)
	}
	for _, path := range []string{"/api/chat/records?kind=person&q=alice", "/api/chat/records/preview?kind=person&id=alice", "/api/chat/records/retain"} {
		for _, method := range []string{"GET", "POST"} {
			w := httptest.NewRecorder()
			h.ServeHTTP(w, httptest.NewRequest(method, path, strings.NewReader(string(body))))
			if w.Code == 200 {
				t.Fatal("portal exposed person context", path)
			}
		}
	}
	s.index.Close()
	if code, _ := artifactsDo(t, s, "GET", "/api/chat/records?kind=person&q=alice", ""); code == 200 {
		t.Fatal("index failure became empty results")
	}
}

func TestPersonContextRejectsAmbiguousProfileNames(t *testing.T) {
	s, root := personContextFixture(t)
	if err := os.Mkdir(filepath.Join(root, "other"), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "other/alice.md"), []byte("---\ncategories: [people]\n---\nDIFFERENT_PERSON\n"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := s.index.ReindexPaths([]string{"other/alice.md"}); err != nil {
		t.Fatal(err)
	}
	if code, out := artifactsDo(t, s, "GET", "/api/chat/records/preview?kind=person&id=alice", ""); code == 200 || !strings.Contains(out["error"].(string), "multiple notes") {
		t.Fatal("ambiguous name selected automatically", code, out)
	}
	for _, path := range []string{"alice.md", "other/alice.md"} {
		contextPreview(t, s, "note", path)
	}
}
