package server

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"golang.org/x/oauth2"
	"manifest/approvals"
	"manifest/gmailsend"
	"manifest/manifestmcp"
	"manifest/recruiting"
	"manifest/recruiting/sources"
)

// ⚠ A REJECTION IS NEVER RECORDED FOR AN EMAIL THAT WAS NOT SENT: rendering
// and preparing send nothing; completing refuses until the owner approved
// and the receipt says sent; the email delivered is byte-for-byte the one
// rendered from the template; then — and only then — the record archives.
func TestRecruitingRejectionEmailsBeforeItArchives(t *testing.T) {
	s, _, vault, data := testRecruitingServer(t)
	now := time.Date(2026, 9, 27, 12, 0, 0, 0, time.UTC)
	c, err := s.recruiting.AcceptDraft(sources.CandidateDraft{SourceID: "manual", Name: "Rae Applicant", Role: "role/mri-engineer",
		Evidence: []sources.Evidence{{SourceID: "manual", URLOrFile: "https://example.test/cv", RetrievedAt: now, Kind: sources.EvidencePage, Trust: sources.TrustMedium, Snippet: "applied"}}}, now)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = s.recruiting.UpdateCandidate(c.ID, map[string]string{"email": "rae@example.test"}); err != nil {
		t.Fatal(err)
	}

	t.Setenv("GMAIL_SEND_TOKEN", "")
	creds := filepath.Join(data, "creds.json")
	if err = os.WriteFile(creds, []byte(`{"installed":{"client_id":"fixture","client_secret":"fixture","auth_uri":"https://example.invalid/auth","token_uri":"https://example.invalid/token","redirect_uris":["http://localhost"]}}`), 0600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("GMAIL_OAUTH_CLIENT", creds)
	registry := gmailsend.NewRegistry(data)
	var sends atomic.Int32
	var delivered atomic.Value
	provider := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		sends.Add(1)
		var payload map[string]string
		_ = json.NewDecoder(r.Body).Decode(&payload)
		raw, _ := gmailsend.DecodeRaw(payload["raw"])
		delivered.Store(string(raw))
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"id":"rej-message","threadId":"rej-thread"}`))
	}))
	defer provider.Close()
	registry.Aion.UseEndpoint(provider.URL, provider.Client())
	if err = registry.Aion.SaveToken("ben@aion.bio", &oauth2.Token{AccessToken: "fixture", TokenType: "Bearer", Expiry: time.Now().Add(time.Hour)}, []string{gmailsend.SendScope}); err != nil {
		t.Fatal(err)
	}
	adapter, err := manifestmcp.New(vault, data, "system")
	if err != nil {
		t.Fatal(err)
	}
	s.UseApprovals(approvals.NewStore(filepath.Join(data, "approvals")))
	s.UseMailSenders(registry)
	s.UseManifestOperations(adapter)

	// 1 · render
	get := func() (recruiting.RejectionDraft, []map[string]any) {
		r := httptest.NewRequest("GET", "/", nil)
		r.SetPathValue("id", c.ID)
		w := httptest.NewRecorder()
		s.handleRecruitingRejection(w, r)
		var out struct {
			Draft      recruiting.RejectionDraft `json:"draft"`
			Error      string                    `json:"error"`
			Operations []map[string]any          `json:"operations"`
		}
		if w.Code != 200 || json.Unmarshal(w.Body.Bytes(), &out) != nil || out.Error != "" {
			t.Fatalf("render: %d %s", w.Code, w.Body.String())
		}
		return out.Draft, out.Operations
	}
	d, _ := get()
	if strings.Join(d.To, ",") != "rae@example.test" || !strings.HasPrefix(d.Body, "Hi Rae,") || !strings.Contains(d.Subject, "MRI") {
		t.Fatalf("rendered: %+v", d)
	}

	// 2 · nothing to complete yet
	if w := recruitingPost(t, s, s.handleRecruitingRejectionComplete, "/", c.ID, `{"operationId":"nope"}`); w.Code != http.StatusConflict {
		t.Fatalf("completed without a sent email: %d %s", w.Code, w.Body.String())
	}

	// 3 · prepare: a stale revision is refused; the real one freezes, sends nothing
	if w := recruitingPost(t, s, s.handleRecruitingRejectionPrepare, "/", c.ID, `{"revision":"`+strings.Repeat("0", 64)+`"}`); w.Code != http.StatusConflict {
		t.Fatalf("a stale review was accepted: %d", w.Code)
	}
	w := recruitingPost(t, s, s.handleRecruitingRejectionPrepare, "/", c.ID, `{"revision":"`+d.Revision+`"}`)
	var prepared struct {
		ID string `json:"operationId"`
	}
	if w.Code != 200 || json.Unmarshal(w.Body.Bytes(), &prepared) != nil || prepared.ID == "" {
		t.Fatalf("prepare: %d %s", w.Code, w.Body.String())
	}
	if sends.Load() != 0 {
		t.Fatal("preparing sent the email")
	}
	if w := recruitingPost(t, s, s.handleRecruitingRejectionComplete, "/", c.ID, `{"operationId":"`+prepared.ID+`"}`); w.Code != http.StatusConflict {
		t.Fatalf("completed before approval: %d %s", w.Code, w.Body.String())
	}
	if _, ops := get(); len(ops) != 1 {
		t.Fatalf("the prepared approval is not listed: %+v", ops)
	}

	// 4 · the owner approves the exact email
	r := httptest.NewRequest("POST", "/", strings.NewReader("{}"))
	r.SetPathValue("id", manifestmcp.ProposalID(prepared.ID))
	aw := httptest.NewRecorder()
	s.handleSpiritsApprovalConfirm(aw, r)
	if aw.Code != 200 {
		t.Fatalf("approve: %d %s", aw.Code, aw.Body.String())
	}
	sent, _ := delivered.Load().(string)
	if sends.Load() != 1 || !strings.Contains(sent, "To: rae@example.test\r\n") || !strings.Contains(sent, "Hi Rae,") {
		t.Fatalf("delivered: %d %q", sends.Load(), sent)
	}

	// 5 · only now does the record archive (no reason → no Ashby write)
	if w := recruitingPost(t, s, s.handleRecruitingRejectionComplete, "/", c.ID, `{"operationId":"`+prepared.ID+`"}`); w.Code != 200 {
		t.Fatalf("complete: %d %s", w.Code, w.Body.String())
	}
	for _, v := range s.recruiting.View().Candidates {
		if v.ID == c.ID && v.Stage != recruiting.StageArchived {
			t.Fatalf("not archived after a sent rejection: %s", v.Stage)
		}
	}
}

// The template is the owner's: a placeholder it doesn't fill is an error,
// never literal braces in a sent email.
func TestRenderRejectionRefusesUnknownPlaceholders(t *testing.T) {
	if _, _, err := recruiting.RenderRejection("---\nsubject: Hi {first_name}\n---\nDear {nickname}", "Rae", "Engineer"); err == nil {
		t.Fatal("an unknown placeholder rendered")
	}
	subj, body, err := recruiting.RenderRejection("---\nsubject: {role_title}\n---\nHi {first_name}", "Rae", "Engineer")
	if err != nil || subj != "Engineer" || body != "Hi Rae\n" {
		t.Fatalf("%q %q %v", subj, body, err)
	}
}
