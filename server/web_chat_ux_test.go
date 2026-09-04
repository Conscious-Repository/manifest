package server

import (
	"io/fs"
	"strings"
	"testing"
)

// The chat surface's conventions pass (plan 2026-09-04 §1, Stage U) moved two
// shared helpers into the library and retired the modal rename + the
// terminal's native confirm. Classic scripts share window scope, so a second
// copy of a helper in a tab file would silently shadow the library's — these
// greps are the only guard.
func TestChatUXConventions(t *testing.T) {
	read := func(p string) string {
		b, err := fs.ReadFile(webFiles, p)
		if err != nil {
			t.Fatalf("%s: %v", p, err)
		}
		return string(b)
	}
	lib := read("web/js/05-components.js")
	for _, want := range []string{"function inlineRename(nameEl, value, onCommit)", "function armedDelete(label, armedLabel, onConfirm)"} {
		if !strings.Contains(lib, want) {
			t.Errorf("05-components.js lacks %q", want)
		}
	}
	chat := read("web/js/48-chat.js")
	if strings.Contains(chat, `askText("Rename`) {
		t.Error("48-chat.js still renames through askText — the idiom is inlineRename")
	}
	if !strings.Contains(chat, "inlineRename(") {
		t.Error("48-chat.js does not use inlineRename")
	}
	for _, f := range []string{"web/js/48-chat.js", "web/js/73-terminal.js", "web/js/40-agents.js", "web/js/74-files.js"} {
		src := read(f)
		for _, banned := range []string{"function armedDelete(", "function inlineRename(", "function termRenameInline("} {
			if strings.Contains(src, banned) {
				t.Errorf("%s defines %s — it lives in 05-components.js", f, banned)
			}
		}
	}
	for _, f := range []string{"web/js/48-chat.js", "web/js/73-terminal.js"} {
		if strings.Contains(read(f), "confirm(") {
			t.Errorf("%s uses a native confirm() — armedDelete is the idiom", f)
		}
	}
	// the page anatomy: CHAT carries the shared head like every top-level view
	idx := read("web/index.html")
	if !strings.Contains(idx, `<span class="agent-title">CHAT</span>`) || !strings.Contains(idx, `id="chatHeadActions"`) {
		t.Error("index.html: #chatView lacks the .agent-head / .agent-actions anatomy")
	}
	css := read("web/css/48-chat.css")
	if !strings.Contains(css, "padding: 0 var(--page-gutter)") || !strings.Contains(css, "var(--rail-w)") {
		t.Error("48-chat.css: the shell must use --page-gutter and the rail --rail-w")
	}
	if strings.Contains(css, "text-transform: uppercase") {
		t.Error("48-chat.css re-types the .micro-label recipe")
	}
}
