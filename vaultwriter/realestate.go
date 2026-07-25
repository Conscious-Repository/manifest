package vaultwriter

import (
	"bytes"
	"encoding/csv"
	"errors"
	"os"
	"path/filepath"
	"strconv"
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

// docExtAllow is the property-docs upload allowlist (bid PDFs, photos, sheets).
var docExtAllow = map[string]bool{
	".pdf": true, ".png": true, ".jpg": true, ".jpeg": true, ".heic": true,
	".webp": true, ".csv": true, ".xlsx": true, ".docx": true, ".txt": true, ".md": true,
}

// MaxDocBytes caps one uploaded property document.
const MaxDocBytes = 25 << 20

// SaveDoc writes an uploaded property document under relDir (the server pins
// relDir to <reRoot>/docs/<slug>/). Database-class guarded, filename sanitized,
// extension allow-listed, size capped, write-once (numbered suffix on collision).
// Returns the vault-relative path written.
func (w *Writer) SaveDoc(relDir, filename string, data []byte) (string, error) {
	if !w.Enabled() {
		return "", errors.New("no vault configured")
	}
	if len(data) == 0 {
		return "", errors.New("empty file")
	}
	if len(data) > MaxDocBytes {
		return "", errors.New("file exceeds the 25MB doc cap")
	}
	ext := strings.ToLower(filepath.Ext(filename))
	if !docExtAllow[ext] {
		return "", errors.New("file type " + ext + " is not allowed in property docs")
	}
	name := sanitizeName(strings.TrimSuffix(filepath.Base(filename), filepath.Ext(filename)))
	if name == "" {
		return "", errors.New("filename has no usable characters")
	}
	relDir = strings.Trim(filepath.ToSlash(relDir), "/")
	if err := w.Guard(relDir+"/x"+ext, WriteDatabase); err != nil {
		return "", err
	}
	dir := filepath.Join(w.vault, filepath.FromSlash(relDir))
	if !isUnder(dir, w.vault) {
		return "", errors.New("invalid docs path")
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", err
	}
	// write-once: never clobber — suffix -2, -3, … on collision
	for i := 0; i < 100; i++ {
		fn := name + ext
		if i > 0 {
			fn = name + "-" + strconv.Itoa(i+1) + ext
		}
		full := filepath.Join(dir, fn)
		if _, err := os.Stat(full); err == nil {
			continue
		}
		if err := os.WriteFile(full, data, 0o644); err != nil {
			return "", err
		}
		return relDir + "/" + fn, nil
	}
	return "", errors.New("too many name collisions")
}

// WriteExport writes a generated export file (underwrite/tax handshake to the
// spreadsheets). Database-class guarded; the caller pins rel under
// <reRoot>/exports/. OVERWRITE allowed — exports regenerate, unlike records.
func (w *Writer) WriteExport(rel string, data []byte) error {
	if !w.Enabled() {
		return errors.New("no vault configured")
	}
	rel = filepath.ToSlash(rel)
	if !strings.Contains(rel, "/exports/") {
		return errors.New("exports must land under the exports/ folder")
	}
	if err := w.Guard(rel, WriteDatabase); err != nil {
		return err
	}
	full := filepath.Join(w.vault, filepath.FromSlash(rel))
	if !isUnder(full, w.vault) {
		return errors.New("invalid export path")
	}
	if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
		return err
	}
	return os.WriteFile(full, data, 0o644)
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
