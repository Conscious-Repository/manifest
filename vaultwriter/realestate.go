package vaultwriter

import (
	"bytes"
	"encoding/csv"
	"os"
	"path/filepath"
	"strings"
)

// Real-estate structured writes (real-estate-management plan, Pass 1): the
// property ledger csv sidecar and the `## log` site diary. Both are DATABASE-class
// records under the system zone, guarded exactly like the book records — user
// actions taken through the dashboard, never AI authoring.

// AppendLedgerRow appends one row to a property's csv ledger sidecar, creating the
// file with `header` when absent. Values are CSV-escaped (quotes/commas safe).
func (w *Writer) AppendLedgerRow(rel string, header, row []string) error {
	full, err := w.resolveRecord(rel) // guards WriteDatabase + traversal
	if err != nil {
		return err
	}
	var buf bytes.Buffer
	cw := csv.NewWriter(&buf)
	if _, statErr := os.Stat(full); os.IsNotExist(statErr) {
		if err := cw.Write(header); err != nil {
			return err
		}
	}
	if err := cw.Write(row); err != nil {
		return err
	}
	cw.Flush()
	if err := cw.Error(); err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
		return err
	}
	f, err := os.OpenFile(full, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		return err
	}
	defer f.Close()
	_, err = f.Write(buf.Bytes())
	return err
}

// PrependLogLine inserts a `- <line>` bullet at the TOP of the record's `## log`
// section (newest-first), creating the section when absent. Bytes outside the
// insertion are preserved.
func (w *Writer) PrependLogLine(rel, line string) error {
	full, err := w.resolveRecord(rel)
	if err != nil {
		return err
	}
	raw, err := os.ReadFile(full)
	if err != nil {
		return err
	}
	return os.WriteFile(full, []byte(insertLogBullet(string(raw), "- "+strings.TrimSpace(line))), 0o644)
}

// insertLogBullet places bullet right under the `## log` heading, or appends a new
// `## log` section at the end when there is none.
func insertLogBullet(content, bullet string) string {
	lines := strings.Split(content, "\n")
	for i, ln := range lines {
		if strings.EqualFold(strings.TrimSpace(ln), "## log") {
			out := make([]string, 0, len(lines)+1)
			out = append(out, lines[:i+1]...)
			out = append(out, bullet)
			return strings.Join(append(out, lines[i+1:]...), "\n")
		}
	}
	return strings.TrimRight(content, "\n") + "\n\n## log\n" + bullet + "\n"
}
