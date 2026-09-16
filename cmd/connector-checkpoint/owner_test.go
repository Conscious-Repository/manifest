package main

import (
	"bytes"
	"database/sql"
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"manifest/approvals"
)

func treeBytes(t *testing.T, root string) map[string]string {
	t.Helper()
	out := map[string]string{}
	err := filepath.Walk(root, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if !info.IsDir() {
			b, e := os.ReadFile(path)
			if e != nil {
				return e
			}
			out[path] = string(b)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	return out
}

func TestOwnerApplyAndCheckpointPreserveHistory(t *testing.T) {
	for _, source := range []string{"granola", "pocket", "email"} {
		t.Run(source, func(t *testing.T) {
			o := fixture(t)
			o.Source = source
			sid, id, path := "not_vJw8dIUwVUiWDT", "58719e5e1d11", "2026-06-25 austin.md"
			status, scope := "approved", source
			if source == "pocket" {
				sid, id, path = "72886f85-9810-488e-a70a-b32ef2fd9dd6", "00fcb06ba967", "2026-08-25 raise process and communications.md"
			}
			if source == "email" {
				sid, id, path = "19fdd282744d15a0", "2a71f54cd7b3", "2026-08-07 our new project wi-fly.md"
				status, scope = "rejected", "gmail-thread"
			}
			writeCard := func(cardID, applyPath, sourceID string) {
				t.Helper()
				raw := "---\nid: " + cardID + "\ntype: create-vault-note\napply-path: " + applyPath + "\n" + scope + "-id: " + sourceID + "\n---\n"
				if err := os.WriteFile(filepath.Join(o.Root, "artifacts", "approvals", status, cardID+".md"), []byte(raw), 0600); err != nil {
					t.Fatal(err)
				}
			}
			writeCard(id, path, sid)
			if source == "email" {
				writeCard("d75af2b821e6", "2026-08-07 our new project wi-fly 4d15a0.md", sid)
				os.WriteFile(o.EmailState, []byte(`{"watermark":"2026-09-10T00:00:00Z","threads":{"19fdd282744d15a0":{"status":"proposed","proposal_id":"d75af2b821e6"}}}`), 0600)
			} else {
				o.Index = filepath.Join(o.Root, "vault-index.sqlite")
				db, err := sql.Open("sqlite", o.Index)
				if err != nil {
					t.Fatal(err)
				}
				if _, err = db.Exec("CREATE TABLE notes(path TEXT, granola_id TEXT, pocket_id TEXT)"); err != nil {
					t.Fatal(err)
				}
				db.Close()
				wm := filepath.Join(o.Root, "vessel", "state", source, "watermark")
				os.MkdirAll(filepath.Dir(wm), 0700)
				os.WriteFile(wm, []byte("2026-09-10T00:00:00Z\n"), 0600)
			}
			// The vault, approvals, cursor and frozen index all belong to the read-only tree.
			os.WriteFile(filepath.Join(o.Root, "vault-sentinel.md"), []byte("never write"), 0600)
			before := treeBytes(t, o.Root)
			var out bytes.Buffer
			if err := run(o, &out); err == nil {
				t.Fatal("accepted unresolved history")
			}
			o.OwnerReconciliation = true
			out.Reset()
			if err := run(o, &out); err != nil {
				t.Fatal(err)
			}
			var preview struct {
				Hash string `json:"reconciliationHash"`
			}
			if err := json.Unmarshal(out.Bytes(), &preview); err != nil {
				t.Fatal(err)
			}
			entries, _ := os.ReadDir(o.DataDir)
			if len(entries) != 0 {
				t.Fatal("dry run wrote files")
			}
			o.Apply = true
			o.ExpectReconciliation = preview.Hash
			if err := run(o, &out); err != nil {
				t.Fatal(err)
			}
			o.OwnerReconciliation = false
			o.Apply = false
			o.Stage = true
			out.Reset()
			if err := run(o, &out); err != nil {
				t.Fatal(err)
			}
			files, err := filepath.Glob(filepath.Join(o.DataDir, "connector-handoff", "checkpoints", source, "*.json"))
			if err != nil || len(files) != 1 {
				t.Fatal(files, err)
			}
			b, _ := os.ReadFile(files[0])
			want := approvals.ReconciledUncertain
			if source == "email" {
				want = approvals.ReconciledRejected
				for _, value := range []string{"2a71f54cd7b3", "d75af2b821e6", `"status": "muted"`} {
					if !strings.Contains(string(b), value) {
						t.Fatal("lost rejected lineage")
					}
				}
			}
			if !strings.Contains(string(b), want) || !strings.Contains(string(b), `"replay": false`) || strings.Contains(string(b), "existing-note") {
				t.Fatal("historical truth lost", string(b))
			}
			if !reflect.DeepEqual(before, treeBytes(t, o.Root)) {
				t.Fatal("vault, approval or legacy state write")
			}
			o.Stage = false

			if source != "email" {
				db, err := sql.Open("sqlite", o.Index)
				if err != nil {
					t.Fatal(err)
				}
				if _, err = db.Exec("INSERT INTO notes(path,"+source+"_id) VALUES (?,?)", "found.md", sid); err != nil {
					t.Fatal(err)
				}
				db.Close()
				if err := run(o, &out); err == nil {
					t.Fatal("reconciled uncertain silently promoted to existing note")
				}
				db, _ = sql.Open("sqlite", o.Index)
				db.Exec("DELETE FROM notes")
				db.Close()
			} else {
				raw, _ := os.ReadFile(o.EmailState)
				drift := strings.Replace(string(raw), "19fdd282744d15a0", "unlisted-source", 1)
				os.WriteFile(o.EmailState, []byte(drift), 0600)
				if err := run(o, &out); err == nil {
					t.Fatal("unlisted email identity mismatch accepted")
				}
				os.WriteFile(o.EmailState, raw, 0600)
			}
			writeCard("unlisted", "other.md", "unlisted-source")
			if source == "email" {
				writeCard("unlisted-duplicate", "other.md", "unlisted-source")
			}
			if err := run(o, &out); err == nil {
				t.Fatal("unlisted mismatch accepted")
			}
		})
	}
}
