package extractorcompare

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// Synthetic adversarial bytes exercise refusal only. These are not model
// outputs, parity fixtures, native run receipts or live evidence.
func stage(t *testing.T, legacy, successor string) Request {
	t.Helper()
	nonce := make([]byte, 32)
	if _, err := rand.Read(nonce); err != nil {
		t.Fatal(err)
	}
	dir := "/var/tmp/extractor-comparison-" + hex.EncodeToString(nonce)
	if err := os.Mkdir(dir, 0700); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.RemoveAll(dir) })
	q := Request{Directory: dir, Vault: t.TempDir(), Ritual: "aion"}
	for i, s := range []string{legacy, successor} {
		h := fmt.Sprintf("%x", sha256.Sum256([]byte(s)))
		side := "legacy"
		if i == 1 {
			side = "successor"
			q.Successor = []string{h}
		} else {
			q.Legacy = []string{h}
		}
		if err := os.WriteFile(filepath.Join(dir, side+"-"+h+".artifact"), []byte(s), 0600); err != nil {
			t.Fatal(err)
		}
	}
	return q
}
func TestAdversarialEvidenceNeverCertifies(t *testing.T) {
	base := `{"source":"source-1","type":"aion-backlog","applyPath":"system/aion/backlog.md","categories":["aion"],"node":"n1","replay":false}`
	cases := map[string]string{
		"identical structural claims": base,
		"changed source":              strings.ReplaceAll(base, "source-1", "source-2"),
		"wrong domain":                strings.ReplaceAll(base, "aion", "realestate"),
		"invalid references":          strings.ReplaceAll(base, "n1", "missing"),
		"replay true":                 strings.ReplaceAll(base, "false", "true"),
		"target mismatch":             strings.ReplaceAll(base, "backlog.md", "people.md"),
		"category mismatch":           strings.ReplaceAll(base, `["aion"]`, `["person"]`),
		"unsupported type":            strings.ReplaceAll(base, "aion-backlog", "run-errand"),
		"ambiguous artifact":          base + base,
		"unbound source":              `{"proposals":[]}`,
		"different write safety":      `{"writes":["/etc/passwd"]}`,
		"redaction":                   `{"body":"PRIVATE prose jane@example.org 555-123-4567 sk-live-secret","path":"Jane Doe address"}`,
	}
	for name, b := range cases {
		t.Run(name, func(t *testing.T) {
			q := stage(t, base, b)
			before, _ := os.ReadFile(filepath.Join(q.Directory, "successor-"+q.Successor[0]+".artifact"))
			r, err := Check(q)
			if err != ErrRefused || r.Readiness != Unrun || !r.InputHashesVerified || r.Migrated || r.Replay || r.StructuralComparisonPerformed || r.SemanticValidationPerformed {
				t.Fatalf("unexpected result: %+v %v", r, err)
			}
			raw, e := os.ReadFile(filepath.Join(q.Directory, "report.json"))
			if e != nil {
				t.Fatal(e)
			}
			for _, s := range []string{"PRIVATE", "jane@", "555-123", "sk-live", "Jane Doe", "source-1", "source-2"} {
				if strings.Contains(string(raw), s) {
					t.Fatal("private data escaped")
				}
			}
			var saved Report
			if json.Unmarshal(raw, &saved) != nil || saved.Successor[0].SHA256 != q.Successor[0] || saved.Legacy[0].Path != filepath.Join(q.Directory, "legacy-"+q.Legacy[0]+".artifact") {
				t.Fatal("lost exact evidence")
			}
			after, _ := os.ReadFile(filepath.Join(q.Directory, "successor-"+q.Successor[0]+".artifact"))
			if string(before) != string(after) {
				t.Fatal("input changed")
			}
			st, _ := os.Stat(filepath.Join(q.Directory, "report.json"))
			if st.Mode().Perm() != 0600 {
				t.Fatal("report permissions")
			}
			if _, err = Check(q); err == ErrRefused {
				t.Fatal("overwrote report")
			}
			again, _ := os.ReadFile(filepath.Join(q.Directory, "report.json"))
			if string(again) != string(raw) {
				t.Fatal("report changed")
			}
		})
	}
}
func TestUnsafeInputsNoReport(t *testing.T) {
	cases := map[string]func(*testing.T, *Request){
		"missing output": func(t *testing.T, q *Request) {
			os.Remove(filepath.Join(q.Directory, "successor-"+q.Successor[0]+".artifact"))
		},
		"missing set":              func(t *testing.T, q *Request) { q.Successor = nil },
		"ambiguous repeated input": func(t *testing.T, q *Request) { q.Legacy = append(q.Legacy, q.Legacy[0]) },
		"hash mismatch": func(t *testing.T, q *Request) {
			os.WriteFile(filepath.Join(q.Directory, "legacy-"+q.Legacy[0]+".artifact"), []byte("changed"), 0600)
		},
		"malformed hash":     func(t *testing.T, q *Request) { q.Legacy[0] = "PRIVATE-secret" },
		"unsupported ritual": func(t *testing.T, q *Request) { q.Ritual = "private@example.org" },
		"vault overlap":      func(t *testing.T, q *Request) { q.Vault = q.Directory },
		"vault ancestor":     func(t *testing.T, q *Request) { q.Vault = "/var/tmp" },
		"public directory":   func(t *testing.T, q *Request) { os.Chmod(q.Directory, 0755) },
		"symlink artifact": func(t *testing.T, q *Request) {
			p := filepath.Join(q.Directory, "legacy-"+q.Legacy[0]+".artifact")
			os.Remove(p)
			os.Symlink(filepath.Join(q.Directory, "successor-"+q.Successor[0]+".artifact"), p)
		},
		"directory artifact": func(t *testing.T, q *Request) {
			p := filepath.Join(q.Directory, "legacy-"+q.Legacy[0]+".artifact")
			os.Remove(p)
			os.Mkdir(p, 0700)
		},
	}
	for name, mutate := range cases {
		t.Run(name, func(t *testing.T) {
			q := stage(t, "same", "same")
			mutate(t, &q)
			_, err := Check(q)
			if err == nil || err == ErrRefused || strings.Contains(err.Error(), "PRIVATE") || strings.Contains(err.Error(), "private@") {
				t.Fatalf("unsafe error: %v", err)
			}
			if _, err := os.Stat(filepath.Join(q.Directory, "report.json")); !os.IsNotExist(err) {
				t.Fatal("unexpected report")
			}
		})
	}
}
func TestAllDutiesAndSets(t *testing.T) {
	for _, ritual := range []string{"aion", "real-estate", "ooda-email"} {
		t.Run(ritual, func(t *testing.T) {
			q := stage(t, "old", "new")
			q.Ritual = ritual
			r, err := Check(q)
			if err != ErrRefused || r.Duty != "extractor/"+ritual {
				t.Fatal(r, err)
			}
		})
	}
}

func TestUnsafeStagingAndDestination(t *testing.T) {
	t.Run("private filename", func(t *testing.T) {
		q := stage(t, "a", "b")
		q.Directory += "-jane@example.org"
		_, err := Check(q)
		if err != errInput {
			t.Fatal(err)
		}
	})
	t.Run("symlink directory", func(t *testing.T) {
		q := stage(t, "a", "b")
		original := q.Directory
		nonce := make([]byte, 32)
		if _, err := rand.Read(nonce); err != nil {
			t.Fatal(err)
		}
		alias := "/var/tmp/extractor-comparison-" + hex.EncodeToString(nonce)
		if err := os.Symlink(original, alias); err != nil {
			t.Fatal(err)
		}
		defer os.Remove(alias)
		q.Directory = alias
		if _, err := Check(q); err != errInput {
			t.Fatal(err)
		}
	})
	t.Run("symlink report", func(t *testing.T) {
		q := stage(t, "a", "b")
		target := filepath.Join(q.Vault, "sentinel")
		os.WriteFile(target, []byte("unchanged"), 0600)
		os.Symlink(target, filepath.Join(q.Directory, "report.json"))
		if _, err := Check(q); err == ErrRefused || err == nil {
			t.Fatal(err)
		}
		raw, _ := os.ReadFile(target)
		if string(raw) != "unchanged" {
			t.Fatal("vault modified")
		}
	})
}
