package server

import (
	"context"
	"encoding/json"
	"manifest/artifacts"
	"os"
	"strings"
	"testing"
	"time"
)

func TestSharedTerminalFilesKeepThreadScopeAndExactReceipt(t *testing.T) {
	s, se, thread, sends := sharedInputFixture(t, false)
	var err error
	s.artifacts, err = artifacts.New(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	upload := func(domain, target, name, content string) artifacts.Ref {
		f, e := s.artifacts.Save(strings.NewReader(content), name, "text/plain")
		if e != nil {
			t.Fatal(e)
		}
		if e = s.artifacts.Add(domain, artifacts.Entry{Ref: f, Thread: target, ByEmail: "member@aion.bio", At: time.Now()}); e != nil {
			t.Fatal(e)
		}
		return f
	}
	selected := upload("aion", thread, "plan.md", "EXACT_SELECTED_PLAN")
	sibling := upload("aion", "another-thread", "sibling.md", "SIBLING_SECRET")
	wrong := upload("ooda", thread, "ooda.md", "WRONG_DOMAIN")
	scope := &sharedTerminalInputScope{s.kairosAgent(), thread, se.ID, "member@aion.bio", "Member"}
	ctx, _, _, err := s.sharedInputContext(context.Background(), scope, nil, selected.Hash)
	if err != nil || !strings.Contains(ctx, selected.Hash) || !strings.Contains(ctx, s.artifacts.BlobPath(selected.Hash)) {
		t.Fatal(ctx, err)
	}
	for _, hash := range []string{sibling.Hash, wrong.Hash} {
		if _, _, _, err = s.sharedInputContext(context.Background(), scope, nil, hash); err == nil {
			t.Fatal("cross-thread/domain file accepted")
		}
	}
	payload, _ := json.Marshal(map[string]any{"text": "Discuss this plan", "files": []string{selected.Hash}, "requestId": "receipt-input-001"})
	response := postSharedInput(s, se, thread, scope.Email, string(payload))
	if response.Code != 200 {
		t.Fatal(response.Code, response.Body.String())
	}
	receipt, err := s.terminal.readInputReceipt(se.ID, "receipt-input-001")
	if err != nil || len(receipt.Files) != 1 || receipt.Files[0].Hash != selected.Hash || receipt.Files[0].Name != "plan.md" {
		t.Fatal(receipt, err)
	}
	before := sends.Load()
	// A receipt retry must not reread/reinterpret a newer file or send twice.
	if err = os.WriteFile(s.artifacts.BlobPath(selected.Hash), []byte("CORRUPTED"), 0600); err != nil {
		t.Fatal(err)
	}
	response = postSharedInput(s, se, thread, scope.Email, string(payload))
	if response.Code != 200 || sends.Load() != before {
		t.Fatal("receipt retry replayed input", response.Code)
	}
	if _, _, _, err = s.sharedInputContext(context.Background(), scope, nil, selected.Hash); err == nil {
		t.Fatal("corrupt selected version accepted for a new send")
	}
	payload, _ = json.Marshal(map[string]any{"text": "Discuss this plan", "files": []string{sibling.Hash}, "requestId": "receipt-input-001"})
	if response = postSharedInput(s, se, thread, scope.Email, string(payload)); response.Code != 409 || sends.Load() != before {
		t.Fatal("changed receipt files accepted", response.Code)
	}
}
