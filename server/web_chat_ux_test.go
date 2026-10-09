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
	for _, f := range []string{"web/js/48-chat.js", "web/js/73-terminal.js", "web/js/74-files.js", "web/js/05-components.js"} {
		for n, line := range strings.Split(read(f), "\n") {
			code := strings.TrimSpace(line)
			if i := strings.Index(code, "//"); i >= 0 {
				code = code[:i] // a comment may name the banned call
			}
			for _, native := range []string{"confirm(", "alert(", "prompt("} {
				// window.prompt/alert/confirm — the idioms are armedDelete, a
				// clickable showToast, and askText
				if strings.Contains(code, " "+native) || strings.Contains(code, "!"+native) || strings.Contains(code, "("+native) {
					t.Errorf("%s:%d uses a native %s — armedDelete / a clickable toast is the idiom", f, n+1, native)
				}
			}
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

// The phone Now section and stream switcher are a read-only projection over
// the inbox the rail already loaded: no request, no storage, no timer of their
// own (the chat refresh lifecycle repaints them), and none of the writers the
// rail's menus use. Pinned stays the owner's existing pin preference, and
// opening the switcher can never mark a conversation read.
func TestChatNowIsReadOnly(t *testing.T) {
	b, err := fs.ReadFile(webFiles, "web/js/49-chat-now.js")
	if err != nil {
		t.Fatalf("49-chat-now.js: %v", err)
	}
	var code strings.Builder
	for _, line := range strings.Split(string(b), "\n") {
		if i := strings.Index(line, "//"); i >= 0 && !strings.Contains(line[:i], "\"") && !strings.Contains(line[:i], "'") {
			line = line[:i]
		}
		code.WriteString(line + "\n")
	}
	src := code.String()
	for _, banned := range []string{"fetch(", "XMLHttpRequest", "EventSource", "sendBeacon", "localStorage", "sessionStorage", "indexedDB",
		"setInterval(", "postJSON", "chatSetPinned(", "chatMarkViewed(", "chatSetLifecycle(", "chatSetPriority(", "chatSaveWorkstream("} {
		if strings.Contains(src, banned) {
			t.Errorf("49-chat-now.js uses %s — the Now section and the switcher only read what the inbox already holds", banned)
		}
	}
	idx, err := fs.ReadFile(webFiles, "web/index.html")
	if err != nil {
		t.Fatal(err)
	}
	page := string(idx)
	chat, now, tiles := strings.Index(page, `src="js/48-chat.js`), strings.Index(page, `src="js/49-chat-now.js`), strings.Index(page, `src="js/50-chat-tiles.js`)
	if now < 0 || !(chat < now && now < tiles) {
		t.Errorf("index.html must load js/49-chat-now.js after 48-chat.js and before 50-chat-tiles.js (positions %d, %d, %d)", chat, now, tiles)
	}
}
