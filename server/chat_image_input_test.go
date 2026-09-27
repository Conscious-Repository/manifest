package server

import (
	"strings"
	"testing"
	"time"

	"manifest/agentchat"
	"manifest/threads"
)

// Owner decision D2 (2026-09-27): Manifest keeps no per-model vision table it
// cannot verify. No adapter declares image input, so every adapter reports
// imageInput "unknown", and every path that hands an image to a model labels
// it "vision support unknown" instead of promising a vision tool. The image
// is still delivered, never silently: the owner sees the same label on the
// attachment (testdata/chat-attachment-turn.cjs).
func TestImageAttachmentVisionSupportUnknown(t *testing.T) {
	for _, caps := range chatAdapterCapabilities() {
		if caps.ImageInput != imageInputUnknown {
			t.Fatalf("%s declares image input %q; no model's vision support is known", caps.Adapter, caps.ImageInput)
		}
	}
	png := "\x89PNG\r\n\x1a\n" + strings.Repeat("\x00", 32)
	check := func(path, prompt, image, other string) {
		t.Helper()
		line := ""
		for _, l := range strings.Split(prompt, "\n") {
			if strings.Contains(l, image) {
				line = l
			}
		}
		if !strings.Contains(line, "vision support unknown") || !strings.Contains(line, "say so rather than describing it") {
			t.Fatalf("%s: image line does not say vision support is unknown: %q\n%s", path, line, prompt)
		}
		if strings.Contains(prompt, "vision tool") {
			t.Fatalf("%s: still promises a vision tool:\n%s", path, prompt)
		}
		for _, l := range strings.Split(prompt, "\n") {
			if strings.Contains(l, other) && strings.Contains(l, "vision") {
				t.Fatalf("%s: a non-image file is labelled as an image: %q", path, l)
			}
		}
	}

	s := &Server{}
	s.UseChatState(t.TempDir())
	private, err := threads.New(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	s.UseThreads(private, nil, nil, nil, "owner@example.test")
	shot, err := private.SaveBlob(strings.NewReader(png), "screen.png", "image/png")
	if err != nil {
		t.Fatal(err)
	}
	notes, err := private.SaveBlob(strings.NewReader("\x00binary"), "notes.bin", "application/octet-stream")
	if err != nil {
		t.Fatal(err)
	}

	// Native Hermes chat: [file::] tokens on the user turn.
	turn := agentchat.Turn{Who: "user", Text: "what is wrong here?\n[file:: " + shot.Hash + " screen.png]\n[file:: " + notes.Hash + " notes.bin]"}
	check("native chat", s.agentChatAttachments(turn), "screen.png", "notes.bin")

	// Owned chat files, both the native reference line and the coding
	// (terminal) attachment context block.
	img := uploadOwned(t, s, "agent:alfred/one", "photo.png", []byte(png))
	doc := uploadOwned(t, s, "agent:alfred/one", "meta.json", []byte(`{"a":1}`))
	check("native owned file", s.agentChatAttachments(agentchat.Turn{Who: "user", Text: "[context-file:: " + img.ID + "]\n[context-file:: " + doc.ID + "]"}), img.ID, doc.ID)
	coding, err := s.ownedChatContext("agent:alfred/one", "[context-file:: "+img.ID+"]\n[context-file:: "+doc.ID+"]")
	if err != nil {
		t.Fatal(err)
	}
	check("coding attachment context", coding, "photo.png", "meta.json")

	// Task-thread delegation: the owner's thread attachments.
	if _, err := private.Add(threads.Identity{ID: "owner", Name: "Owner"}, "inbox/fix-layout", "comment", "see the screenshot", nil, []threads.FileRef{shot, notes}, nil, time.Now()); err != nil {
		t.Fatal(err)
	}
	check("task delegation", s.hermesAttachments("inbox/fix-layout"), "screen.png", "notes.bin")
}
