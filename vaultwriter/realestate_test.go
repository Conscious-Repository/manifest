package vaultwriter

import (
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

// SaveDoc: the app's first binary write path — pin down the guard rails.
func TestSaveDoc(t *testing.T) {
	vault := t.TempDir()
	w := New(vault).WithZoneRoots("system", "extrinsic")
	dir := "system/realestate/docs/4848-page"

	rel, err := w.SaveDoc(dir, "Joe Bid.pdf", []byte("%PDF-1.4 fake"))
	if err != nil {
		t.Fatal(err)
	}
	if rel != dir+"/Joe Bid.pdf" {
		t.Fatalf("rel = %q", rel)
	}
	// collision → numbered suffix, never clobber
	rel2, err := w.SaveDoc(dir, "Joe Bid.pdf", []byte("second"))
	if err != nil || rel2 != dir+"/Joe Bid-2.pdf" {
		t.Fatalf("collision suffix: %q %v", rel2, err)
	}
	// disallowed extension
	if _, err := w.SaveDoc(dir, "evil.sh", []byte("x")); err == nil {
		t.Fatal("extension allowlist must refuse .sh")
	}
	// traversal in filename is neutralized by sanitize + Base
	if rel3, err := w.SaveDoc(dir, "../../escape.pdf", []byte("x")); err != nil {
		t.Fatal(err)
	} else if strings.Contains(rel3, "..") {
		t.Fatalf("traversal survived: %q", rel3)
	}
	// outside the system zone → refused by the guard
	if _, err := w.SaveDoc("notes/docs/x", "a.pdf", []byte("x")); err == nil {
		t.Fatal("docs outside the database zones must be refused")
	}
	// oversize
	if _, err := w.SaveDoc(dir, "big.pdf", make([]byte, MaxDocBytes+1)); err == nil {
		t.Fatal("size cap must refuse oversize files")
	}
}

// WriteExport: overwrite-allowed but pinned to exports/.
func TestWriteExport(t *testing.T) {
	vault := t.TempDir()
	w := New(vault).WithZoneRoots("system", "extrinsic")
	rel := "system/realestate/exports/deal-underwrite.csv"
	if err := w.WriteExport(rel, []byte("a,b\n")); err != nil {
		t.Fatal(err)
	}
	if err := w.WriteExport(rel, []byte("a,b,c\n")); err != nil {
		t.Fatal("exports must be overwritable:", err)
	}
	b, _ := os.ReadFile(filepath.Join(vault, filepath.FromSlash(rel)))
	if string(b) != "a,b,c\n" {
		t.Fatalf("overwrite didn't take: %q", b)
	}
	if err := w.WriteExport("system/realestate/records/x.csv", []byte("x")); err == nil {
		t.Fatal("WriteExport must refuse paths outside exports/")
	}
}

// §A3: a ledger APPEND must land in write-audit.log like every other vault
// write (updates/deletes already did; the O_APPEND fast path gets its own line).
func TestAppendLedgerRowAudits(t *testing.T) {
	vault, data := t.TempDir(), t.TempDir()
	w := New(vault).WithZoneRoots("system", "extrinsic").WithAudit(data)
	rel := "system/realestate/4848-page.ledger.csv"

	if err := w.AppendLedgerRow(rel, []string{"date", "amount"}, []string{"2026-07-29", "125"}); err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(filepath.Join(vault, filepath.FromSlash(rel)))
	if err != nil {
		t.Fatal(err)
	}
	if want := "date,amount\n2026-07-29,125\n"; string(raw) != want {
		t.Fatalf("append bytes: %q want %q", raw, want)
	}
	audit, err := os.ReadFile(filepath.Join(data, "write-audit.log"))
	if err != nil {
		t.Fatal("append did not write an audit line:", err)
	}
	line := strings.TrimSpace(string(audit))
	if !strings.Contains(line, rel+"\tre-ledger-append\tuser-action\t+"+strconv.Itoa(len(raw))) {
		t.Fatalf("audit line: %q", line)
	}
}
