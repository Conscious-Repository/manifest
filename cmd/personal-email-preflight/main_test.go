package main

import (
	"bytes"
	"context"
	"manifest/personalemail"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

type fixtureReader struct{ calls int }

func (r *fixtureReader) ThreadAnchor(_ context.Context, id, msg string, ms int64) error {
	r.calls++
	return nil
}

func TestReadOnlyPreflightAndDuplicateProposal(t *testing.T) {
	root := t.TempDir()
	art := filepath.Join(root, "artifacts")
	for _, s := range []string{"pending", "approved", "rejected"} {
		if err := os.MkdirAll(filepath.Join(art, "approvals", s), 0700); err != nil {
			t.Fatal(err)
		}
	}
	state := filepath.Join(root, "state.json")
	raw := `{"watermark":"2026-09-01T00:00:00Z","threads":{"t":{"status":"proposed","proposal_id":"p","last_msg_id":"m","last_internal_ms":123}}}`
	proposal := "---\nid: p\ntype: create-vault-note\nritual: email-sync\ngmail-thread-id: t\n---\nPRIVATE BODY"
	for path, b := range map[string]string{state: raw, filepath.Join(art, "approvals", "pending", "p.md"): proposal, filepath.Join(root, "vault.md"): "VAULT SENTINEL", filepath.Join(root, "cursor"): "CURSOR SENTINEL"} {
		if err := os.WriteFile(path, []byte(b), 0600); err != nil {
			t.Fatal(err)
		}
	}
	snapshot := func() map[string]string {
		m := map[string]string{}
		err := filepath.WalkDir(root, func(p string, d os.DirEntry, e error) error {
			if e != nil {
				return e
			}
			if !d.IsDir() {
				b, e := os.ReadFile(p)
				if e != nil {
					return e
				}
				m[p] = string(b)
			}
			return nil
		})
		if err != nil {
			t.Fatal(err)
		}
		return m
	}
	before := snapshot()
	var out bytes.Buffer
	if err := run(context.Background(), options{State: state, Artifacts: art, Account: "a"}, &out, nil); err != nil {
		t.Fatal(err)
	}
	r := &fixtureReader{}
	live := options{State: state, Artifacts: art, Account: "a", Live: true}
	var liveOut bytes.Buffer
	if err := run(context.Background(), live, &liveOut, func(context.Context, string) (personalemail.AnchorReader, error) { return r, nil }); err != nil || r.calls != 1 {
		t.Fatal("live fixture", err, r.calls)
	}
	after := snapshot()
	if len(before) != len(after) {
		t.Fatal("files created")
	}
	for p, b := range before {
		if after[p] != b {
			t.Fatal("file changed", p)
		}
	}
	if strings.Contains(out.String(), "PRIVATE") || !strings.Contains(out.String(), `"enabled":false`) {
		t.Fatal(out.String())
	}
	if err := os.WriteFile(filepath.Join(art, "approvals", "rejected", "p.md"), []byte(proposal), 0600); err != nil {
		t.Fatal(err)
	}
	out.Reset()
	if err := run(context.Background(), options{State: state, Artifacts: art, Account: "a"}, &out, nil); err == nil || out.Len() != 0 {
		t.Fatal("duplicate proposal accepted", err)
	}
}
