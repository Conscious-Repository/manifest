package server

import (
	"context"
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
	"manifest/gmailsync"
	"manifest/manifestmcp"
	"manifest/recruiting"
	"manifest/recruiting/sources"
)

// This journey carries reviewed sourcing evidence through canonical approval,
// delivery recovery, explicit recruiting outcome recording and reply notices.
func TestWorkbenchSourcedCandidateToCanonicalOutreach(t *testing.T) {
	for _, lostAck := range []bool{false, true} {
		name := "confirmed"
		if lostAck {
			name = "lost-ack"
		}
		t.Run(name, func(t *testing.T) { workbenchSourcedOutreachJourney(t, lostAck) })
	}
}
func workbenchSourcedOutreachJourney(t *testing.T, lostAck bool) {
	s, _, vault, data := testRecruitingServer(t)
	now := time.Date(2026, 9, 24, 12, 0, 0, 0, time.UTC)
	candidate, err := s.recruiting.AcceptDraft(sources.CandidateDraft{SourceID: "manual", Name: "Journey Candidate", Role: "role/mri-engineer", Evidence: []sources.Evidence{{SourceID: "manual", URLOrFile: "https://example.test/source", RetrievedAt: now, Kind: sources.EvidencePage, Trust: sources.TrustMedium, Snippet: "Low-field MRI hardware, pulse sequence and coil design; available on-site in Saint Louis"}}}, now)
	if err != nil {
		t.Fatal(err)
	}
	if len(candidate.Evidence) != 1 || candidate.Evidence[0].Snippet != "Low-field MRI hardware, pulse sequence and coil design; available on-site in Saint Louis" {
		t.Fatal(candidate)
	}
	if _, err = s.recruiting.UpdateCandidate(candidate.ID, map[string]string{"email": "candidate@example.test"}); err != nil {
		t.Fatal(err)
	}
	for _, criterion := range []string{"low-field MRI hardware", "pulse sequence or coil design", "on-site Saint Louis"} {
		candidate, err = s.recruiting.ScoreFit(candidate.ID, criterion, "4", []string{candidate.Evidence[0].ID}, false)
		if err != nil {
			t.Fatal(err)
		}
	}
	if !candidate.Gate.Passed {
		t.Fatal("reviewed candidate did not pass readiness gate", candidate.Gate)
	}
	private, chats, _ := agentChatFixture(t, echoStub)
	s.agentChat = private.agentChat
	conversation, err := chats.Create("alfred", "", "Recruiting journey", "")
	if err != nil {
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
	var deliveredBody atomic.Value
	provider := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		attempt := sends.Add(1)
		var payload map[string]string
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
			t.Error(err)
		}
		raw, err := gmailsend.DecodeRaw(payload["raw"])
		if err != nil {
			t.Error(err)
		}
		deliveredBody.Store(string(raw))
		if lostAck && attempt == 2 {
			// The fake provider accepted these bytes, but its acknowledgement is lost.
			connection, _, err := w.(http.Hijacker).Hijack()
			if err != nil {
				t.Error(err)
				return
			}
			connection.Close()
			return
		}
		w.Header().Set("Content-Type", "application/json")
		if attempt == 1 {
			w.Write([]byte(`{"id":"journey-message","threadId":"journey-thread"}`))
		} else {
			w.Write([]byte(`{"id":"bridge-message","threadId":"bridge-thread"}`))
		}
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
	approvalRoot := filepath.Join(data, "approvals")
	s.UseApprovals(approvals.NewStore(approvalRoot))
	s.UseMailSenders(registry)
	s.UseManifestOperations(adapter)
	draftResponse := recruitingPost(t, s, s.handleRecruitingOutreachDraft, "/api/aion/recruiting/outreach/draft/"+candidate.ID, candidate.ID, `{"kind":"direct","subject":"Coil research role","body":"Reviewed invitation referencing documented coil design experience."}`)
	if draftResponse.Code != 200 {
		t.Fatal(draftResponse.Code, draftResponse.Body.String())
	}
	var drafted struct {
		Entry recruiting.OutreachEntry `json:"entry"`
	}
	if err = json.Unmarshal(draftResponse.Body.Bytes(), &drafted); err != nil {
		t.Fatal(err)
	}
	draft := drafted.Entry
	readinessResponse := recruitingPost(t, s, s.handleRecruitingOutreachPrepare, "/", candidate.ID, `{}`)
	var readiness struct {
		Readiness recruiting.OutreachReadiness `json:"readiness"`
	}
	if readinessResponse.Code != 200 || json.Unmarshal(readinessResponse.Body.Bytes(), &readiness) != nil || !readiness.Readiness.Ready {
		t.Fatal("reviewed draft not ready", readinessResponse.Code, readinessResponse.Body.String())
	}

	input := manifestmcp.EmailInput{Domain: "aion", To: draft.To, Subject: draft.Subject, Body: draft.Body, Conversation: conversation, Turn: "reviewed-sourcing-turn", IdempotencyKey: "sourced-outreach-journey"}
	prepare := func(q manifestmcp.EmailInput) *httptest.ResponseRecorder {
		b, _ := json.Marshal(q)
		w := httptest.NewRecorder()
		s.handleEmailPrepare(w, httptest.NewRequest("POST", "/api/email/prepare", strings.NewReader(string(b))))
		return w
	}
	prepared := prepare(input)
	if prepared.Code != 200 {
		t.Fatal(prepared.Code, prepared.Body.String())
	}
	var result struct {
		ID string `json:"operationId"`
	}
	json.Unmarshal(prepared.Body.Bytes(), &result)
	retry := prepare(input)
	if retry.Code != 200 {
		t.Fatal(retry.Code, retry.Body.String())
	}
	var again struct {
		ID string `json:"operationId"`
	}
	json.Unmarshal(retry.Body.Bytes(), &again)
	if again.ID != result.ID {
		t.Fatal("duplicate proposal")
	}
	if sends.Load() != 0 || len(s.feedProposals()) != 1 || len(s.chatOperations(conversation)) != 1 || len(s.chatOperations("unrelated")) != 0 {
		t.Fatal("preparation sent or lost identity")
	}
	// A later saved outreach draft cannot replace the frozen email under approval.
	newer := recruitingPost(t, s, s.handleRecruitingOutreachDraft, "/", candidate.ID, `{"kind":"direct","subject":"Changed subject","body":"UNREVIEWED_REPLACEMENT"}`)
	if newer.Code != 200 {
		t.Fatal(newer.Code, newer.Body.String())
	}
	changed := input
	changed.Body = "UNREVIEWED_REPLACEMENT"
	if w := prepare(changed); w.Code == 200 {
		t.Fatal("changed payload reused approval identity")
	}
	approve := func() *httptest.ResponseRecorder {
		r := httptest.NewRequest("POST", "/", strings.NewReader("{}"))
		r.SetPathValue("id", manifestmcp.ProposalID(result.ID))
		w := httptest.NewRecorder()
		s.handleSpiritsApprovalConfirm(w, r)
		return w
	}
	decisions := make(chan *httptest.ResponseRecorder, 2)
	for i := 0; i < 2; i++ {
		go func() { decisions <- approve() }()
	}
	accepted := 0
	for i := 0; i < 2; i++ {
		w := <-decisions
		if w.Code == 200 {
			accepted++
		} else if w.Code >= 500 {
			t.Fatal(w.Code, w.Body.String())
		}
	}
	if accepted == 0 {
		t.Fatal("neither owner approval succeeded")
	}
	delivered, _ := deliveredBody.Load().(string)
	if sends.Load() != 1 || !strings.Contains(delivered, "From: ben@aion.bio\r\n") || !strings.Contains(delivered, "To: candidate@example.test\r\n") || !strings.Contains(delivered, draft.Body) || strings.Contains(delivered, "UNREVIEWED_REPLACEMENT") {
		t.Fatal("wrong frozen delivery", sends.Load(), delivered)
	}
	receipt := s.chatOperations(conversation)[0]["record"].(*manifestmcp.OperationRecord)
	if receipt.Status != "succeeded" || receipt.Result["messageId"] != "journey-message" || receipt.Result["threadId"] != "journey-thread" || len(s.feedProposals()) != 0 {
		t.Fatal(receipt)
	}
	// Reload the operation and approval stores, then retry the accepted identity.
	restarted, err := manifestmcp.New(vault, data, "system")
	if err != nil {
		t.Fatal(err)
	}
	s.UseApprovals(approvals.NewStore(approvalRoot))
	s.UseManifestOperations(restarted)
	if _, err = restarted.Execute(context.Background(), result.ID); err != nil {
		t.Fatal(err)
	}
	if sends.Load() != 1 {
		t.Fatal("restart replayed email")
	}
	got := s.chatOperations(conversation)[0]["record"].(*manifestmcp.OperationRecord)
	if got.ID != receipt.ID || got.Result["threadId"] != receipt.Result["threadId"] {
		t.Fatal("receipt changed across restart")
	}
	// The board bridge prepares and recovers the same canonical approval directly.
	bridgeDraft := recruitingPost(t, s, s.handleRecruitingOutreachDraft, "/", candidate.ID, `{"kind":"direct","subject":"Bridge review","body":"BRIDGE_REVIEWED"}`)
	if bridgeDraft.Code != 200 {
		t.Fatal(bridgeDraft.Code, bridgeDraft.Body.String())
	}
	var bridge struct {
		Entry recruiting.OutreachEntry `json:"entry"`
	}
	json.Unmarshal(bridgeDraft.Body.Bytes(), &bridge)
	propose := func(revision string) *httptest.ResponseRecorder {
		b, _ := json.Marshal(map[string]string{"revision": revision})
		return recruitingPost(t, s, s.handleRecruitingOutreachPropose, "/", candidate.ID, string(b))
	}
	if w := propose(strings.Repeat("0", 64)); w.Code != 409 {
		t.Fatal("stale bridge review accepted", w.Code)
	}
	revision := outreachDraftRevision(bridge.Entry)
	w := propose(revision)
	if w.Code != 200 {
		t.Fatal(w.Code, w.Body.String())
	}
	var bridged struct {
		ID string `json:"operationId"`
	}
	json.Unmarshal(w.Body.Bytes(), &bridged)
	if len(s.feedProposals()) != 1 || sends.Load() != 1 {
		t.Fatal("bridge sent without approval")
	}
	pendingReceipt := httptest.NewRecorder()
	pendingRequest := httptest.NewRequest("GET", "/", nil)
	pendingRequest.SetPathValue("id", bridged.ID)
	s.handleEmailReceipt(pendingReceipt, pendingRequest)
	if pendingReceipt.Code != 409 || sends.Load() != 1 {
		t.Fatal("pending receipt read executed or claimed delivery", pendingReceipt.Code)
	}
	pendingBody, _ := json.Marshal(map[string]string{"operationId": bridged.ID})
	pendingRecord := recruitingPost(t, s, s.handleRecruitingOutreachReconcile, "/", candidate.ID, string(pendingBody))
	if pendingRecord.Code == 200 || sends.Load() != 1 {
		t.Fatal("pending approval was recorded as sent", pendingRecord.Code)
	}
	projected := s.recruitingOutreachOperations(candidate.ID)
	if len(projected) != 1 || projected[0]["record"].(*manifestmcp.OperationRecord).ID != bridged.ID || len(s.recruitingOutreachOperations("cand/other")) != 0 {
		t.Fatal("bridge identity projection", projected)
	}
	later := recruitingPost(t, s, s.handleRecruitingOutreachDraft, "/", candidate.ID, `{"kind":"direct","subject":"Later","body":"LATER_NOT_APPROVED"}`)
	if later.Code != 200 {
		t.Fatal(later.Code)
	}
	w = propose(revision)
	var recovered struct {
		ID string `json:"operationId"`
	}
	json.Unmarshal(w.Body.Bytes(), &recovered)
	if w.Code != 200 || recovered.ID != bridged.ID {
		t.Fatal("bridge retry lost frozen identity", w.Code, w.Body.String())
	}
	r := httptest.NewRequest("POST", "/", strings.NewReader("{}"))
	r.SetPathValue("id", manifestmcp.ProposalID(bridged.ID))
	decision := httptest.NewRecorder()
	s.handleSpiritsApprovalConfirm(decision, r)
	if decision.Code != 200 || sends.Load() != 2 {
		t.Fatal(decision.Code, decision.Body.String(), sends.Load())
	}
	if body, _ := deliveredBody.Load().(string); !strings.Contains(body, "BRIDGE_REVIEWED") || strings.Contains(body, "LATER_NOT_APPROVED") {
		t.Fatal("bridge payload changed", body)
	}
	if lostAck {
		projected = s.recruitingOutreachOperations(candidate.ID)
		if len(projected) != 1 || projected[0]["record"].(*manifestmcp.OperationRecord).Status != "partial" {
			t.Fatal("lost ack claimed success", projected)
		}
		body, _ := json.Marshal(map[string]string{"operationId": bridged.ID})
		if refused := recruitingPost(t, s, s.handleRecruitingOutreachReconcile, "/", candidate.ID, string(body)); refused.Code == 200 {
			t.Fatal("uncertain outcome recorded")
		}
		// Reopening and ordinary execution/observation must not cross the send boundary.
		recoveredAdapter, err := manifestmcp.New(vault, data, "system")
		if err != nil {
			t.Fatal(err)
		}
		s.UseManifestOperations(recoveredAdapter)
		if _, err := recoveredAdapter.Execute(context.Background(), bridged.ID); err != nil {
			t.Fatal(err)
		}
		unavailable := httptest.NewRecorder()
		unavailableRequest := httptest.NewRequest("POST", "/", strings.NewReader(`{}`))
		unavailableRequest.SetPathValue("id", bridged.ID)
		s.handleEmailReconcile(unavailable, unavailableRequest)
		if unavailable.Code != 409 || sends.Load() != 2 {
			t.Fatal("unconnected evidence check changed delivery", unavailable.Code, sends.Load())
		}
		reads := 0
		lookup := func(ctx context.Context, sender, id string) (gmailsend.SentProof, error) {
			reads++
			raw, _ := deliveredBody.Load().(string)
			if sender != "ben@aion.bio" || !strings.Contains(raw, "Message-ID: "+id+"\r\n") {
				t.Fatal("recovery looked up another attempt", sender, id)
			}
			if _, ok := ctx.Deadline(); !ok {
				t.Fatal("unbounded evidence read")
			}
			return gmailsend.SentProof{Mailbox: sender, Ref: gmailsend.Ref{ID: "bridge-message", ThreadID: "bridge-thread"}, Raw: []byte(raw)}, nil
		}
		for range 2 {
			if _, err := recoveredAdapter.ReconcileEmail(context.Background(), bridged.ID, lookup); err != nil {
				t.Fatal(err)
			}
		}
		if reads != 1 || sends.Load() != 2 {
			t.Fatal("recovery replayed provider work", reads, sends.Load())
		}
		outcome, err := recoveredAdapter.ConfirmedEmail(bridged.ID)
		if err != nil || outcome.Source == nil || outcome.Source.ID != candidate.ID || outcome.Source.Revision != revision || outcome.Message.Body != "BRIDGE_REVIEWED" {
			t.Fatal("recovery lost source or frozen content", outcome, err)
		}
	}
	projected = s.recruitingOutreachOperations(candidate.ID)
	if len(projected) != 1 || projected[0]["record"].(*manifestmcp.OperationRecord).Status != "succeeded" {
		t.Fatal("board lost canonical outcome", projected)
	}
	if w = propose(revision); w.Code != 200 || sends.Load() != 2 {
		t.Fatal("bridge replay after completion", w.Code, sends.Load())
	}
	reconcile := func(id, op string) *httptest.ResponseRecorder {
		b, _ := json.Marshal(map[string]string{"operationId": op})
		return recruitingPost(t, s, s.handleRecruitingOutreachReconcile, "/", id, string(b))
	}
	if w := reconcile("cand/wrong", bridged.ID); w.Code != 409 {
		t.Fatal("wrong candidate accepted", w.Code)
	}
	for i := 0; i < 2; i++ {
		if w := reconcile(candidate.ID, bridged.ID); w.Code != 200 {
			t.Fatal(w.Code, w.Body.String())
		}
	}
	if sends.Load() != 2 {
		t.Fatal("record reconciliation sent mail")
	}
	updated := s.recruiting.LoadCandidate(recruiting.CandidateSlug(candidate.ID))
	if updated.Get("stage") != recruiting.StageOutreach || len(updated.Outreach()[0].Operations) != 1 {
		t.Fatal("outcome not reflected in candidate", updated.Outreach())
	}
	entries, err := s.recruiting.Outreach(candidate.ID)
	if err != nil || len(entries) < 2 || entries[0].Body != draft.Body || entries[0].Subject != draft.Subject {
		t.Fatal("original reviewed draft history changed", entries, err)
	}
	// The sent receipt's owner control changes monitoring without another send.
	for _, body := range []string{`{"enabled":true,"stopAfterReply":true}`, `{"enabled":false}`, `{"enabled":true}`} {
		r := httptest.NewRequest("POST", "/", strings.NewReader(body))
		r.SetPathValue("id", bridged.ID)
		w := httptest.NewRecorder()
		s.handleEmailWatch(w, r)
		var result struct {
			Record struct {
				Watch *manifestmcp.EmailWatch `json:"emailWatch"`
			} `json:"record"`
		}
		if err := json.Unmarshal(w.Body.Bytes(), &result); err != nil || w.Code != 200 || result.Record.Watch == nil || !result.Record.Watch.StopAfterReply || sends.Load() != 2 {
			t.Fatal("watch policy update or legacy toggle lost rule", w.Code, w.Body.String(), err)
		}
	}

	observed := time.Now().UTC()
	if err := s.manifestOperations.PollEmailReplies(context.Background(), observed, func(context.Context, string, string) ([]gmailsync.Msg, error) {
		return []gmailsync.Msg{{ID: "bridge-message", From: "ben@aion.bio", Internal: observed.Add(-time.Hour)}, {ID: "reply-notice", From: "candidate@example.test", Internal: observed.Add(-time.Minute), Body: "Reply for the owner"}}, nil
	}); err != nil {
		t.Fatal(err)
	}
	notices := s.emailReplyCards(observed)
	if len(notices) != 1 || notices[0].OperationID != bridged.ID || s.portalInboxCount() != 1 {
		t.Fatal("reply missing from notices/badge", notices)
	}
	request := httptest.NewRequest("GET", "/", nil)
	request.SetPathValue("id", bridged.ID)
	response := httptest.NewRecorder()
	s.handleEmailReceipt(response, request)
	if response.Code != 200 || !strings.Contains(response.Body.String(), "Reply for the owner") || sends.Load() != 2 {
		t.Fatal("read-only receipt", response.Code, sends.Load())
	}
	payload, _ := json.Marshal(map[string]string{"id": notices[0].ID})
	response = httptest.NewRecorder()
	s.handlePortalDismiss(response, httptest.NewRequest("POST", "/", strings.NewReader(string(payload))))
	if response.Code != 200 || s.portalInboxCount() != 0 || sends.Load() != 2 {
		t.Fatal("notice dismissal", response.Code, s.portalInboxCount(), sends.Load())
	}

}
