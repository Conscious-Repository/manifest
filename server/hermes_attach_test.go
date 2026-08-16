package server

import (
	"strings"
	"testing"

	"manifest/threads"
)

// The owner attaches files on a thread → the do-bot's work order must carry
// them: text inlined, images handed a path (vision reads those).
func TestHermesAttachmentsInlineAndImage(t *testing.T) {
	srv, _ := panelFixture(t)
	taskID := "inbox/wants-a-ui"

	css := srv.saveThreadBlob(t, "design.css", "text/css", "body { color: rebeccapurple }")
	img := srv.saveThreadBlob(t, "inspo.png", "image/png", "\x89PNG\r\n\x1a\nfake-bytes")
	if _, err := srv.addThreadEntry(srv.ownerIdentity(), taskID, threads.ActComment,
		"build this — i attached a ui file", nil, []threads.FileRef{css, img}, nil); err != nil {
		t.Fatal(err)
	}

	att := srv.hermesAttachments(taskID)
	if !strings.Contains(att, "design.css (attached file)") || !strings.Contains(att, "rebeccapurple") {
		t.Errorf("text attachment not inlined:\n%s", att)
	}
	if !strings.Contains(att, "inspo.png (image") || !strings.Contains(att, "vision") {
		t.Errorf("image attachment not handed by path:\n%s", att)
	}
	// the image path must be a real, absolute on-disk blob path
	imgPath := srv.threads.private.BlobPath(img.Hash)
	if imgPath == "" || !strings.Contains(att, imgPath) {
		t.Errorf("image path %q not surfaced:\n%s", imgPath, att)
	}
	// raw PNG bytes must NOT be inlined
	if strings.Contains(att, "fake-bytes") {
		t.Errorf("binary image inlined into the prompt:\n%s", att)
	}

	// an agent-authored attachment is NOT surfaced (owner's only)
	af := srv.saveThreadBlob(t, "agent.txt", "text/plain", "agent output")
	_, _ = srv.addThreadEntry(agentIdentity("hermes"), taskID, threads.ActComment, "here", nil, []threads.FileRef{af}, nil)
	if strings.Contains(srv.hermesAttachments(taskID), "agent output") {
		t.Errorf("agent attachment leaked into the owner-attachment block")
	}

	// no attachments → empty block
	if got := srv.hermesAttachments("inbox/nothing-here"); got != "" {
		t.Errorf("empty thread should yield no block, got %q", got)
	}
}

func (s *Server) saveThreadBlob(t *testing.T, name, mime, content string) threads.FileRef {
	t.Helper()
	ref, err := s.threads.private.SaveBlob(strings.NewReader(content), name, mime)
	if err != nil {
		t.Fatal(err)
	}
	return ref
}
