package approvals

import "testing"

func TestParseOlgaRequests(t *testing.T) {
	text := "# Requests for Benjamin\n\nintro\n\n" +
		"- 2026-10-09 22:28 — Olga asked: \"a \\\"direct\\\" chat\"\n  Thread: app:c-15221b349911\n  What it would take: a server change.\n  More.\n" +
		"- 2026-10-10 08:00 — Olga asked: \"purple rail\" (touches shared files)\n" +
		"stray line\n  not part of anything\n"
	rs := ParseOlgaRequests(text)
	if len(rs) != 2 {
		t.Fatalf("%+v", rs)
	}
	if r := rs[0]; r.Asked != `a "direct" chat` || r.Thread != "app:c-15221b349911" || r.Need != "What it would take: a server change.\nMore." || len(r.ID) != 40 {
		t.Fatalf("%+v", r)
	}
	if r := rs[1]; r.Why != "touches shared files" || r.Need != "" || r.Thread != "" {
		t.Fatalf("%+v", r)
	}
	if r, ok := OlgaRequestByID(text, rs[1].ID); !ok || r.Asked != "purple rail" {
		t.Fatal("by id")
	}
	if p := OlgaRequestProposal(rs[0]); p.Type != TypeOlgaRequest || p.ID != rs[0].ID || p.Action != `Olga asked: a "direct" chat` {
		t.Fatalf("%+v", p)
	}
}
