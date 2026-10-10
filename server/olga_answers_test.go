package server

import (
	"encoding/json"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"manifest/approvals"
	"manifest/jev"
	"manifest/olgachat"
	"manifest/record"
	"manifest/vaultwriter"
)

// The round trip the owner asked for (2026-10-10): Olga asks Liber to ask
// Benjamin something → it arrives in his Approvals → his Won't do (with a
// note) comes back into the very chat she asked in, as his words.
func TestOlgaRequestRoundTrip(t *testing.T) {
	prev := olgaAnswerPoll
	olgaAnswerPoll = 20 * time.Millisecond
	t.Cleanup(func() { olgaAnswerPoll = prev })
	voice := &fakeVoice{answer: func(p string) string {
		if strings.Contains(p, "Benjamin: ") {
			return "Sunday it is, then.\n```json\n{\"route\":\"talk\"}\n```"
		}
		return "I've sent that to Benjamin; his answer will show up here.\n```json\n{\"route\":\"talk\",\"for_benjamin\":\"Olga asks whether Saturday works for the plywood run.\"}\n```"
	}}
	do, vault := liberRig(t, voice, fakeRouter{route: jev.RouteTalk})
	w := do("POST", "/api/liber/send", `{"text":"Can you ask Benjamin if Saturday works for the plywood run?"}`, "")
	var th olgachat.Thread
	if json.Unmarshal(w.Body.Bytes(), &th); w.Code != 200 || th.ID == "" {
		t.Fatalf("send: %d %s", w.Code, w.Body)
	}
	got := waitThread(t, do, "id="+th.ID, func(th *olgachat.Thread) bool {
		return len(th.Turns) == 2 && th.Turns[1].Status == "" && th.Turns[1].Text != ""
	})
	if cs := got.Turns[1].Cards; len(cs) != 1 || cs[0].Kind != olgachat.CardNote || !strings.Contains(cs[0].Summary, "Sent to Benjamin") {
		t.Fatalf("no Sent to Benjamin card: %+v", got.Turns[1].Cards)
	}

	// his side: the same vault
	srv := &Server{}
	srv.UseVault(vaultwriter.New(vault).Grant(vaultwriter.Capability{Name: "olga-answers", Zone: record.ZoneSystem, Pattern: "system/olga/answers/**", Actor: vaultwriter.ActorUserAction}))
	srv.UseApprovals(approvals.NewStore(t.TempDir()))
	var card *approvalRow
	for _, r := range srv.feedProposals() {
		if r.Type == approvals.TypeOlgaRequest {
			r := r
			card = &r
		}
	}
	if card == nil || !strings.Contains(card.Body, "Message for Benjamin: Olga asks whether Saturday works") {
		t.Fatalf("no card in Approvals: %+v", card)
	}
	if strings.Contains(card.Body, "Thread:") {
		t.Fatalf("the chat reference leaked into the card body:\n%s", card.Body)
	}
	req := httptest.NewRequest("POST", "/api/spirits/approvals/"+card.ID+"/reject", strings.NewReader(`{"reason":"Saturday's out — Sunday morning?"}`))
	req.SetPathValue("id", card.ID)
	rec := httptest.NewRecorder()
	srv.handleSpiritsApprovalReject(rec, req)
	if rec.Code != 200 {
		t.Fatalf("Won't do: %d %s", rec.Code, rec.Body)
	}
	var a approvals.OlgaAnswer
	b, _ := os.ReadFile(filepath.Join(vault, "system", "olga", "answers", card.ID+".json"))
	if json.Unmarshal(b, &a) != nil || a.Thread != "app:"+th.ID || a.Decision != "wont" || a.Note != "Saturday's out — Sunday morning?" {
		t.Fatalf("answer file: %s", b)
	}

	// her side: his words arrive in the chat she asked in, once
	got = waitThread(t, do, "id="+th.ID, func(th *olgachat.Thread) bool {
		for _, tu := range th.Turns {
			if tu.Who == "benjamin" {
				return true
			}
		}
		return false
	})
	last := got.Turns[len(got.Turns)-1]
	if last.Who != "benjamin" || !strings.Contains(last.Text, "Saturday's out — Sunday morning?") || !strings.Contains(last.Text, "Not doing this one") {
		t.Fatalf("his answer: %+v", last)
	}
	time.Sleep(100 * time.Millisecond) // several more polls
	again := waitThread(t, do, "id="+th.ID, func(*olgachat.Thread) bool { return true })
	if len(again.Turns) != len(got.Turns) {
		t.Fatalf("his answer was delivered twice: %d → %d turns", len(got.Turns), len(again.Turns))
	}
	// Liber reads it as his, in her next turn
	do("POST", "/api/liber/send", `{"id":"`+th.ID+`","text":"ok what now"}`, "")
	waitThread(t, do, "id="+th.ID, func(th *olgachat.Thread) bool {
		return th.Turns[len(th.Turns)-1].Text == "Sunday it is, then."
	})
}
