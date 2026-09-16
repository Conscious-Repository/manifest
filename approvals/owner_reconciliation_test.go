package approvals

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func ownerFixture(t *testing.T, source string) string {
	t.Helper()
	root := t.TempDir()
	for _, status := range statuses {
		if err := os.MkdirAll(filepath.Join(root, "approvals", status), 0700); err != nil {
			t.Fatal(err)
		}
	}
	r, _ := ownerCase(source)
	cards := map[string]string{}
	status := "approved"
	scope := source
	switch source {
	case "granola":
		cards["58719e5e1d11"] = "2026-06-25 austin.md"
	case "pocket":
		cards["00fcb06ba967"] = "2026-08-25 raise process and communications.md"
	case "email":
		scope = "gmail-thread"
		status = "rejected"
		cards["2a71f54cd7b3"] = "2026-08-07 our new project wi-fly.md"
		cards["d75af2b821e6"] = "2026-08-07 our new project wi-fly 4d15a0.md"
	}
	for id, path := range cards {
		raw := "---\nid: " + id + "\ntype: create-vault-note\napply-path: " + path + "\n" + scope + "-id: " + r.SourceID + "\n---\n```proposed\n---\n" + scope + "-id: " + r.SourceID + "\n---\nPRIVATE TRANSCRIPT BODY\n```\n"
		if err := os.WriteFile(filepath.Join(root, "approvals", status, id+".md"), []byte(raw), 0600); err != nil {
			t.Fatal(err)
		}
	}
	return root
}

func TestOwnerReconciliation(t *testing.T) {
	for _, source := range []string{"granola", "pocket", "email"} {
		t.Run(source, func(t *testing.T) {
			root, data := ownerFixture(t, source), t.TempDir()
			before := InspectConnectorInventory(root)
			r, inv, err := PreviewOwnerReconciliation(root, source)
			if err != nil {
				t.Fatal(err)
			}
			b := OwnerReconciliationBytes(r)
			hash := EvidenceHash(string(b))
			if r.Replay || strings.Contains(string(b), "PRIVATE") {
				t.Fatal("unsafe record")
			}
			entries, _ := os.ReadDir(data)
			if len(entries) != 0 {
				t.Fatal("preview wrote data")
			}
			if _, err = WriteOwnerReconciliation(data, root, source, "wrong"); err == nil {
				t.Fatal("drift accepted")
			}
			if _, err = WriteOwnerReconciliation(data, root, source, hash); err != nil {
				t.Fatal(err)
			}
			if _, err = WriteOwnerReconciliation(data, root, source, hash); err != nil {
				t.Fatal("idempotent apply failed", err)
			}
			if _, owner, err := ReadReconciledConnectorInventory(root, source, data); err != nil || owner == nil {
				t.Fatal(err)
			}
			if InspectConnectorInventory(root).Hash != before.Hash {
				t.Fatal("approval history changed")
			}
			if source == "email" {
				if len(inv.Items) != 2 {
					t.Fatal("lost duplicate")
				}
				if _, err := ReadConnectorInventory(root); err == nil {
					t.Fatal("global strict inventory weakened")
				}
				if _, err := ReadConnectorInventoryForSource(root, source); err == nil {
					t.Fatal("scoped strict inventory weakened")
				}
			}
			for _, change := range []func(*OwnerReconciliation){func(x *OwnerReconciliation) { x.SourceID += "x" }, func(x *OwnerReconciliation) { x.Replay = true }, func(x *OwnerReconciliation) { x.Disposition = "existing-note" }, func(x *OwnerReconciliation) { x.Artifacts[0].Hash = "changed" }} {
				fresh, _, _ := PreviewOwnerReconciliation(root, source)
				change(&fresh)
				if ValidateOwnerReconciliation(fresh, inv) == nil {
					t.Fatal("invalid record accepted")
				}
				os.WriteFile(ownerRecordPath(data, source), OwnerReconciliationBytes(fresh), 0600)
				if _, _, err := ReadReconciledConnectorInventory(root, source, data); err == nil {
					t.Fatal("tampering accepted")
				}
			}

			for _, bad := range []string{strings.Replace(string(b), `"replay": false,`, "", 1), strings.Replace(string(b), `"replay": false,`, `"replay": true, "replay": false,`, 1), string(b) + "{}", strings.Replace(string(b), `"version": 1,`, `"version": 1, "secret": "unexpected",`, 1)} {
				os.WriteFile(ownerRecordPath(data, source), []byte(bad), 0600)
				if _, _, err := ReadReconciledConnectorInventory(root, source, data); err == nil {
					t.Fatal("malformed record accepted")
				}
			}
			os.WriteFile(ownerRecordPath(data, source), b, 0600)
			artifact := filepath.Join(root, "approvals", r.Artifacts[0].Status, r.Artifacts[0].ID+".md")
			raw, _ := os.ReadFile(artifact)
			os.WriteFile(artifact, append(raw, []byte("drift")...), 0600)
			if _, _, err := ReadReconciledConnectorInventory(root, source, data); err == nil {
				t.Fatal("content drift accepted")
			}
			if _, err := WriteOwnerReconciliation(data, root, source, hash); err == nil {
				t.Fatal("apply accepted old evidence")
			}
		})
	}
}

func TestOwnerRefusesUnlistedIdentity(t *testing.T) {
	for _, source := range []string{"granola", "pocket", "email"} {
		t.Run(source, func(t *testing.T) {
			root := ownerFixture(t, source)
			r, _, _ := PreviewOwnerReconciliation(root, source)
			p := filepath.Join(root, "approvals", r.Artifacts[0].Status, r.Artifacts[0].ID+".md")
			b, _ := os.ReadFile(p)
			b = []byte(strings.ReplaceAll(string(b), r.SourceID, r.SourceID+"-other"))
			os.WriteFile(p, b, 0600)
			if _, _, err := PreviewOwnerReconciliation(root, source); err == nil {
				t.Fatal("unlisted identity authorized")
			}
		})
	}
}
