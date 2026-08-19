package vaultwriter

import (
	"bytes"
	"encoding/csv"
	"encoding/json"
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
	return w.AppendLedgerRowAs(rel, header, row, ActorUserAction)
}

// AppendLedgerRowAs is the actor-carrying append (bank-accounts plan §7):
// manual workbench applies stay user-action; the feed's auto-apply lane
// audits as bank-feed. Same write path, same guard, different forensic line.
func (w *Writer) AppendLedgerRowAs(rel string, header, row []string, actor Actor) error {
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
	if _, err := f.Write(buf.Bytes()); err != nil {
		return err
	}
	// §A3: appends audit like every other vault write (updates/deletes already
	// did via commit; the O_APPEND fast path needs its own line). Delta is the
	// appended byte count by construction.
	w.audit(filepath.ToSlash(rel), "re-ledger-append", string(actor), int64(buf.Len()))
	return nil
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
	return w.commit(full, "re-log", []byte(insertLogBullet(string(raw), "- "+strings.TrimSpace(line))))
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
		if err := w.commit(full, "re-doc", data); err != nil {
			return "", err
		}
		return relDir + "/" + fn, nil
	}
	return "", errors.New("too many name collisions")
}

// WriteSourceJSON writes a record's engine-shaped source sidecar — the LIVE
// canonical for the owner's public-site data (admin-portal plan). Pinned to
// `*.source.json` under the database zones; overwrite allowed (it's an edit
// surface, not a write-once record). Callers must round-trip the FULL object —
// unknown fields are never dropped (the fidelity contract).
func (w *Writer) WriteSourceJSON(rel string, data []byte) error {
	if !w.Enabled() {
		return errors.New("no vault configured")
	}
	rel = filepath.ToSlash(rel)
	if !strings.HasSuffix(rel, ".source.json") {
		return errors.New("source writes are pinned to *.source.json")
	}
	if err := w.Guard(rel, WriteDatabase); err != nil {
		return err
	}
	if !json.Valid(data) {
		return errors.New("refusing to write invalid JSON")
	}
	full := filepath.Join(w.vault, filepath.FromSlash(rel))
	if !isUnder(full, w.vault) {
		return errors.New("invalid source path")
	}
	if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
		return err
	}
	return w.commit(full, "re-source", data)
}

// WriteUnderwriteJSON writes a property's estimate-vintage lock snapshot
// (overhaul §3.6) — pinned to `*.underwrite.json`. Overwrite is allowed at
// THIS layer (the server gates re-lock behind an explicit confirm; the
// deliberateness lives in the gesture, not the writer).
func (w *Writer) WriteUnderwriteJSON(rel string, data []byte) error {
	if !w.Enabled() {
		return errors.New("no vault configured")
	}
	rel = filepath.ToSlash(rel)
	if !strings.HasSuffix(rel, ".underwrite.json") {
		return errors.New("underwrite writes are pinned to *.underwrite.json")
	}
	if err := w.Guard(rel, WriteDatabase); err != nil {
		return err
	}
	if !json.Valid(data) {
		return errors.New("refusing to write invalid JSON")
	}
	full := filepath.Join(w.vault, filepath.FromSlash(rel))
	if !isUnder(full, w.vault) {
		return errors.New("invalid underwrite path")
	}
	if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
		return err
	}
	return w.commit(full, "re-underwrite", data)
}

// UpdateLedgerRow replaces one exact row in a ledger csv; DeleteLedgerRow
// removes it. Both are FAIL-CLOSED on a stale match (the file changed
// underneath — an Obsidian/Excel edit is never silently clobbered), matching
// the ReplaceBullet discipline. Rows are compared field-by-field after csv
// parsing, so quoting differences don't defeat the match. First match wins.
func (w *Writer) UpdateLedgerRow(rel string, original, replacement []string) error {
	return w.editLedger(rel, original, &replacement)
}

func (w *Writer) DeleteLedgerRow(rel string, original []string) error {
	return w.editLedger(rel, original, nil)
}

func (w *Writer) editLedger(rel string, original []string, replacement *[]string) error {
	full, err := w.resolveRecord(rel)
	if err != nil {
		return err
	}
	raw, err := os.ReadFile(full)
	if err != nil {
		return err
	}
	rd := csv.NewReader(bytes.NewReader(raw))
	rd.FieldsPerRecord = -1
	records, err := rd.ReadAll()
	if err != nil {
		return err
	}
	found := -1
	for i, rec := range records {
		if i == 0 && len(rec) > 0 && strings.EqualFold(strings.TrimSpace(rec[0]), "date") {
			continue // header
		}
		if rowsEqual(rec, original) {
			found = i
			break
		}
	}
	if found < 0 {
		return errors.New("that ledger row no longer matches the file (it changed underneath) — reload and retry")
	}
	if replacement != nil {
		records[found] = *replacement
	} else {
		records = append(records[:found], records[found+1:]...)
	}
	var buf bytes.Buffer
	cw := csv.NewWriter(&buf)
	if err := cw.WriteAll(records); err != nil {
		return err
	}
	cw.Flush()
	if err := cw.Error(); err != nil {
		return err
	}
	return w.commit(full, "re-ledger", buf.Bytes())
}

// rowsEqual compares csv rows field-by-field, padding the shorter (ragged rows
// from hand edits still match their parsed projection).
func rowsEqual(a, b []string) bool {
	n := len(a)
	if len(b) > n {
		n = len(b)
	}
	get := func(s []string, i int) string {
		if i < len(s) {
			return strings.TrimSpace(s[i])
		}
		return ""
	}
	for i := 0; i < n; i++ {
		if get(a, i) != get(b, i) {
			return false
		}
	}
	return true
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
	return w.commit(full, "re-export", data)
}

// ReplaceSection swaps the BODY of one `## section` in a record (creating the
// section at EOF when absent), byte-stable everywhere else — the surgical
// write the `## work` mutations use (parse → mutate → emit → replace).
// Database-class guarded.
func (w *Writer) ReplaceSection(rel, section, body string) error {
	full, err := w.resolveRecord(rel)
	if err != nil {
		return err
	}
	raw, err := os.ReadFile(full)
	if err != nil {
		return err
	}
	return w.commit(full, "re-section", []byte(spliceSection(string(raw), section, body)))
}

// ReplaceSectionCap is the same surgical section swap under a DECLARED write
// capability (redesign stage 4 — new section writes must not ship on the
// legacy guard path). Check, splice, write, capability audit.
func (w *Writer) ReplaceSectionCap(capName, rel, section, body string) error {
	if !w.Enabled() {
		return errors.New("no vault configured")
	}
	_, clean, err := w.checkCap(capName, rel)
	if err != nil {
		return err
	}
	raw, err := os.ReadFile(filepath.Join(w.vault, filepath.FromSlash(clean)))
	if err != nil {
		return err
	}
	return w.WriteCap(capName, clean, []byte(spliceSection(string(raw), section, body)))
}

// spliceSection swaps one `## section` body inside raw (creating the section
// at EOF when absent), preserving every byte outside the section span.
func spliceSection(raw, section, body string) string {
	lines := strings.Split(raw, "\n")
	start := -1
	for i, ln := range lines {
		t := strings.TrimRight(ln, " \t")
		if strings.EqualFold(t, "## "+section) {
			start = i
			break
		}
	}
	body = strings.TrimRight(body, "\n")
	var bodyLines []string
	if body != "" {
		bodyLines = strings.Split(body, "\n")
	}
	var out []string
	if start < 0 {
		// absent → append at EOF
		out = append(out, strings.Split(strings.TrimRight(raw, "\n"), "\n")...)
		out = append(out, "", "## "+section)
		out = append(out, bodyLines...)
	} else {
		end := len(lines)
		for i := start + 1; i < len(lines); i++ {
			t := strings.TrimRight(lines[i], " \t")
			if strings.HasPrefix(t, "## ") && !strings.HasPrefix(t, "### ") {
				end = i
				break
			}
		}
		// trim trailing blanks inside the old section span, keep ONE separator
		out = append(out, lines[:start+1]...)
		out = append(out, bodyLines...)
		if end < len(lines) {
			out = append(out, "") // blank line before the next section
			out = append(out, lines[end:]...)
		}
	}
	return strings.TrimRight(strings.Join(out, "\n"), "\n") + "\n"
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
