package server

import (
	"context"
	"encoding/json"
	"github.com/modelcontextprotocol/go-sdk/mcp"
	"manifest/agentchat"
	"manifest/approvals"
	"manifest/manifestmcp"
	"manifest/recruiting"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestManifestOperationSurfacesAndTakeover(t *testing.T) {
	for _, mode := range []string{"confirm", "reject", "manual", "edit"} {
		t.Run(mode, func(t *testing.T) {
			root := t.TempDir()
			vault := filepath.Join(root, "vault")
			data := filepath.Join(root, "data")
			write := func(path string, b []byte) error {
				if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
					return err
				}
				return os.WriteFile(path, b, 0600)
			}
			records := recruiting.NewStore(vault, "system/aion/recruiting", write)
			if err := records.Ensure(); err != nil {
				t.Fatal(err)
			}
			runs, err := recruiting.NewRunStore(filepath.Join(data, "recruiting/runs"), records)
			if err != nil {
				t.Fatal(err)
			}
			runs.RegisterDefaults()
			run, err := runs.Execute(context.Background(), recruiting.RunRequest{Source: "manual", Query: "Ada Example", Max: 1}, time.Now())
			if err != nil {
				t.Fatal(err)
			}
			adapter, err := manifestmcp.New(vault, data, "system")
			if err != nil {
				t.Fatal(err)
			}
			store := approvals.NewStore(filepath.Join(root, "artifacts"))
			s := New(nil, nil, nil)
			s.UseApprovals(store)
			s.UseManifestOperations(adapter)
			chats := agentchat.New(filepath.Join(root, "chats"))
			s.UseAgentChat(chats)
			conversation, err := chats.Create("alfred", "", "Cohesion", "")
			if err != nil {
				t.Fatal(err)
			}
			// A separate MCP process shares only disk, as in the deployed stdio wiring.
			producer, err := manifestmcp.New(vault, data, "system")
			if err != nil {
				t.Fatal(err)
			}
			producer.Conversation, producer.Turn = conversation, "1"
			producer.Approvals = approvals.NewStore(filepath.Join(root, "artifacts"))
			ctx := context.Background()
			st, ct := mcp.NewInMemoryTransports()
			ss, err := producer.Server().Connect(ctx, st, nil)
			if err != nil {
				t.Fatal(err)
			}
			defer ss.Close()
			cs, err := mcp.NewClient(&mcp.Implementation{Name: "surface-test", Version: "1"}, nil).Connect(ctx, ct, nil)
			if err != nil {
				t.Fatal(err)
			}
			defer cs.Close()
			call := func(name string, args any) map[string]any {
				t.Helper()
				res, err := cs.CallTool(ctx, &mcp.CallToolParams{Name: name, Arguments: args})
				if err != nil || res.IsError {
					t.Fatalf("tool: %v %+v", err, res)
				}
				b, _ := json.Marshal(res.StructuredContent)
				var out map[string]any
				if err := json.Unmarshal(b, &out); err != nil {
					t.Fatal(err)
				}
				return out
			}
			out := call("candidate_accept.prepare", manifestmcp.DraftInput{RunID: run.ID, DraftID: "d1", Conversation: "model-supplied-wrong-thread", Turn: "99"})
			id := out["operationId"].(string)
			proposalID := manifestmcp.ProposalID(id)
			if len(store.List("pending")) != 1 {
				t.Fatal("prepare did not file FEED proposal")
			}
			rows := s.feedProposals()
			if len(rows) != 1 || !rows[0].Allowed || rows[0].Operation == nil || !strings.Contains(rows[0].Body, "Ada Example") {
				t.Fatalf("missing grounded FEED card: %+v", rows)
			}
			if len(s.chatOperations("other")) != 0 || len(s.chatOperations(conversation)) != 1 {
				t.Fatal("conversation association")
			}
			// Inspection works with no Hermes runner configured.
			sessionReq := httptest.NewRequest("GET", "/", nil)
			sessionReq.SetPathValue("agent", "alfred")
			sessionReq.SetPathValue("id", conversation)
			sessionW := httptest.NewRecorder()
			s.handleAgentChatSession(sessionW, sessionReq)
			if sessionW.Code != 200 || !strings.Contains(sessionW.Body.String(), id) {
				t.Fatalf("offline chat inspection: %s", sessionW.Body.String())
			}
			// A lost projection is recreated from the recoverable operation.
			if err := os.Remove(filepath.Join(root, "artifacts/approvals/pending", proposalID+".md")); err != nil {
				t.Fatal(err)
			}
			if len(s.feedProposals()) != 1 {
				t.Fatal("proposal recovery failed")
			}
			switch mode {
			case "confirm", "reject":
				req := httptest.NewRequest("POST", "/", strings.NewReader("{}"))
				req.SetPathValue("id", proposalID)
				w := httptest.NewRecorder()
				if mode == "confirm" {
					s.handleSpiritsApprovalConfirm(w, req)
				} else {
					s.handleSpiritsApprovalReject(w, req)
				}
				if w.Code != 200 {
					t.Fatalf("decision: %d %s", w.Code, w.Body.String())
				}
			case "manual":
				if _, _, err := runs.Accept(run.ID, "d1", time.Now()); err != nil {
					t.Fatal(err)
				}
			case "edit":
				if err := write(filepath.Join(vault, records.Root(), "target-edited.md"), []byte("owner edit")); err != nil {
					t.Fatal(err)
				}
			}
			chat := s.chatOperations(conversation)
			o := chat[0]["record"].(*manifestmcp.OperationRecord)
			want := map[string]string{"confirm": "succeeded", "reject": "rejected", "manual": "stale", "edit": "stale"}[mode]
			if o.Status != want {
				t.Fatalf("status %s, want %s: %s", o.Status, want, o.Error)
			}
			if len(s.feedProposals()) != 0 {
				t.Fatal("resolved/stale proposal remains in FEED")
			}
			gotRun, err := runs.Get(run.ID)
			if err != nil {
				t.Fatal(err)
			}
			accepted := gotRun.Drafts[0].Status == recruiting.DraftAccepted
			if accepted != (mode == "confirm" || mode == "manual") {
				t.Fatal("unexpected domain effect")
			}
			if mode == "confirm" {
				if o.Result["objectRefs"] == nil {
					t.Fatal("missing result refs")
				}
				again, err := adapter.Execute(ctx, id)
				if err != nil || again["status"] != "succeeded" {
					t.Fatal("retry failed")
				}
			}
			if mode == "reject" {
				standing := call("source_run.prepare", manifestmcp.SourceInput{Conversation: conversation, Turn: "3", Request: recruiting.RunRequest{Source: "manual", Query: "Grace Example", Max: 1}})
				if standing["policy"] != "standing_authorization" || len(s.feedProposals()) != 0 {
					t.Fatal("source run acquired an approval gate")
				}
				done := call("operation.execute", manifestmcp.OperationInput{OperationID: standing["operationId"].(string)})
				if done["status"] != "succeeded" || done["result"].(map[string]any)["runId"] == nil {
					t.Fatalf("standing result: %+v", done)
				}
			}
			if mode == "manual" || mode == "edit" {
				result, err := adapter.Execute(ctx, id)
				if err != nil || result["status"] != "stale" {
					t.Fatal("dead payload applied")
				}
				fresh, err := adapter.Regenerate(id)
				if mode == "manual" && err == nil {
					t.Fatal("manual completion regenerated a duplicate accept")
				}
				if mode == "edit" && (err != nil || fresh["status"] != "pending_approval" || fresh["operationId"] == id) {
					t.Fatalf("fresh preview: %v %v", fresh, err)
				}
			}
		})
	}
}
