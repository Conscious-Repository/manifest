package agentchat

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func TestRelatedCreationKeepsSourceAndRetryIdentity(t *testing.T) {
	st := New(t.TempDir())
	source, err := st.Create("alfred", "", "Original", "")
	if err != nil {
		t.Fatal(err)
	}
	st.AppendTurn("alfred", source, "user", "Original user text", 0)
	st.SetStatus("alfred", source, StatusThinking)
	path := filepath.Join(st.Root(), "alfred", source+".md")
	before, _ := os.ReadFile(path)
	origin := Origin{Agent: "alfred", ID: source, Prompt: "Reviewed handoff\n\nuser: original request\nagent: quoted reply", Task: "inbox/work", Artifacts: []ArtifactReference{{ID: "0123456789abcdef", Revision: "revision"}}}
	id, err := st.CreateRelatedOnce("alfred", "", "Related", "", "related-request-001", origin)
	if err != nil {
		t.Fatal(err)
	}
	fresh := New(st.Root())
	again, err := fresh.CreateRelatedOnce("alfred", "", "Related", "", "related-request-001", origin)
	if err != nil || again != id {
		t.Fatal(again, err)
	}
	sess, body, queue, ok := fresh.Get("alfred", id)
	if !ok || sess.Origin == nil || sess.Origin.Prompt != origin.Prompt || sess.Task != origin.Task || body != "" || len(queue) != 0 || sess.Turns != 0 {
		t.Fatal(sess, body, queue)
	}
	after, _ := os.ReadFile(path)
	if !bytes.Equal(before, after) {
		t.Fatal("source rewritten")
	}
	recovered, found, err := fresh.RecoverRelatedCreation("alfred", "Related", "", "related-request-001", origin)
	if err != nil || !found || recovered.ID != id {
		t.Fatal("could not recover accepted creation after reopening store", recovered, found, err)
	}
	if _, found, err = fresh.RecoverRelatedCreation("alfred", "Related", "", "not-yet-accepted", origin); err != nil || found {
		t.Fatal("unaccepted request reported as recovered", found, err)
	}
	origin.Prompt = "Changed handoff"
	if _, found, err = fresh.RecoverRelatedCreation("alfred", "Related", "", "related-request-001", origin); !found || !errors.Is(err, ErrRequestConflict) {
		t.Fatal("changed retry was not a conflict", found, err)
	}
	if _, err = fresh.CreateRelatedOnce("alfred", "", "Related", "", "related-request-001", origin); !errors.Is(err, ErrRequestConflict) {
		t.Fatal(err)
	}
}
