package agentchat

import "testing"

func TestToolScopeIsDurableDispatchMetadata(t *testing.T) {
	s := New(t.TempDir())
	id, _ := s.Create("alfred", "", "", "")
	const request = "scope-request"
	original, err := s.Accept("alfred", id, request, "instruction")
	if err != nil {
		t.Fatal(err)
	}
	scope := ToolScope{Toolsets: "web,files", Source: "request"}
	if s.RecordToolScope("alfred", id, request, scope) == nil {
		t.Fatal("queued delivery recorded dispatch")
	}
	if _, claimed, err := s.Claim("alfred", id); err != nil || !claimed {
		t.Fatal(err)
	}
	if err := s.RecordToolScope("alfred", id, request, scope); err != nil {
		t.Fatal(err)
	}
	reopened := New(s.Root())
	got, ok := reopened.Receipt("alfred", id, request)
	if !ok || got.ToolScope == nil || *got.ToolScope != scope || got.Fingerprint != original.Delivery.Fingerprint {
		t.Fatal(got)
	}
	if err := reopened.RecordToolScope("alfred", id, request, scope); err != nil {
		t.Fatal(err)
	}
	if reopened.RecordToolScope("alfred", id, request, ToolScope{Toolsets: "other", Source: "runner"}) == nil {
		t.Fatal("overwrote frozen scope")
	}
	if _, err := reopened.Accept("alfred", id, request, "instruction"); err != nil {
		t.Fatal("metadata changed retry identity", err)
	}
}
