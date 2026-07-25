package server

import (
	"encoding/csv"
	"net/http"
	"strconv"
	"strings"

	"manifest/realestate"
)

// STATEMENT WORKBENCH (admin-portal plan §G) — the one place bank statements
// enter the system: upload → map columns (remembered) → rows land in a
// persistent parking lot → categorize + assign to properties (or split) →
// apply writes each allocation into the target property's ledger csv. Replaces
// the per-property csv import entirely.

func (s *Server) statementsOK(w http.ResponseWriter) bool {
	if s.realestate == nil || s.vault == nil || s.reImport == nil || s.statements == nil {
		http.Error(w, "statements not available", http.StatusServiceUnavailable)
		return false
	}
	return true
}

// handleStatementsUpload parses an uploaded csv (pure parse — nothing stored)
// and returns headers/rows plus the remembered-or-suggested column mapping.
func (s *Server) handleStatementsUpload(w http.ResponseWriter, r *http.Request) {
	if !s.statementsOK(w) {
		return
	}
	if err := r.ParseMultipartForm(8 << 20); err != nil {
		httpError(w, err)
		return
	}
	fhs := r.MultipartForm.File["file"]
	if len(fhs) == 0 {
		httpError(w, errBadRequest("no csv in upload"))
		return
	}
	f, err := fhs[0].Open()
	if err != nil {
		httpError(w, err)
		return
	}
	defer f.Close()
	rd := csv.NewReader(f)
	rd.FieldsPerRecord = -1
	records, err := rd.ReadAll()
	if err != nil {
		httpError(w, err)
		return
	}
	if len(records) < 2 {
		httpError(w, errBadRequest("csv has no data rows"))
		return
	}
	headers, rows := records[0], records[1:]
	if len(rows) > 2000 {
		rows = rows[:2000]
	}
	sig := realestate.Signature(headers)
	remembered, _, _ := s.reImport.Lookup(sig)
	mapping := remembered
	if mapping == nil {
		mapping = realestate.SuggestMapping(headers)
	}
	writeJSON(w, map[string]any{
		"label": fhs[0].Filename, "headers": headers, "rows": rows,
		"signature": sig, "mapping": mapping, "remembered": remembered != nil,
	})
}

// handleStatementsIngest adds client-mapped rows to the parking lot: dedupes
// against every property ledger + the lot itself; vendor memory pre-assigns.
func (s *Server) handleStatementsIngest(w http.ResponseWriter, r *http.Request) {
	if !s.statementsOK(w) {
		return
	}
	var b struct {
		Label     string            `json:"label"`
		Signature string            `json:"signature"`
		Mapping   map[string]string `json:"mapping"`
		Rows      []struct {
			Date, Vendor, Note string
			Amount             float64
		} `json:"rows"`
	}
	if err := decode(r, &b); err != nil || len(b.Rows) == 0 {
		httpError(w, errBadRequest("rows are required"))
		return
	}
	// every ledger line across the portfolio — no double entry, ever
	ledgerKeys := map[string]bool{}
	props, _ := s.realestate.Properties()
	for _, p := range props {
		for _, lr := range p.Ledger {
			ledgerKeys[realestate.DedupeKey(lr.Date, lr.Amount, lr.Vendor)] = true
		}
	}
	_, vendorCat, vendorProp := s.reImport.Lookup("")
	rows := make([]realestate.StatementRow, 0, len(b.Rows))
	for _, row := range b.Rows {
		if strings.TrimSpace(row.Date) == "" || row.Amount <= 0 {
			continue
		}
		rows = append(rows, realestate.StatementRow{
			Date: strings.TrimSpace(row.Date), Vendor: strings.TrimSpace(row.Vendor),
			Note: strings.TrimSpace(row.Note), Amount: row.Amount,
		})
	}
	added, dups := s.statements.Ingest(b.Label, rows, ledgerKeys, vendorCat, vendorProp)
	s.reImport.Remember(b.Signature, b.Mapping, nil, nil) // column mapping memory
	list, last := s.statements.List()
	writeJSON(w, map[string]any{"added": added, "duplicates": dups, "rows": list, "lastImport": last})
}

func (s *Server) handleStatementsList(w http.ResponseWriter, r *http.Request) {
	if !s.statementsOK(w) {
		return
	}
	list, last := s.statements.List()
	writeJSON(w, map[string]any{"rows": list, "lastImport": last})
}

// handleStatementsRow patches one row (category / assignments / state).
func (s *Server) handleStatementsRow(w http.ResponseWriter, r *http.Request) {
	if !s.statementsOK(w) {
		return
	}
	var b struct {
		ID          string              `json:"id"`
		Category    *string             `json:"category"`
		Assignments *[]realestate.Alloc `json:"assignments"`
		State       *string             `json:"state"`
	}
	if err := decode(r, &b); err != nil || b.ID == "" {
		httpError(w, errBadRequest("id is required"))
		return
	}
	row, err := s.statements.Update(b.ID, b.Category, b.Assignments, b.State)
	if err != nil {
		httpError(w, err)
		return
	}
	writeJSON(w, row)
}

// handleStatementsApply writes the validated rows into their target property
// ledgers (splits annotated `split k/n of $total · VENDOR`), re-dedupes, flips
// rows to applied, and feeds the vendor memory.
func (s *Server) handleStatementsApply(w http.ResponseWriter, r *http.Request) {
	if !s.statementsOK(w) {
		return
	}
	var b struct {
		IDs []string `json:"ids"`
	}
	if err := decode(r, &b); err != nil || len(b.IDs) == 0 {
		httpError(w, errBadRequest("ids are required"))
		return
	}
	rows, err := s.statements.Applicable(b.IDs)
	if err != nil {
		httpError(w, err)
		return
	}
	// resolve slugs → ledger paths up front (fail before any write)
	rels := map[string]string{}
	for _, row := range rows {
		for _, a := range row.Assignments {
			if _, ok := rels[a.Slug]; ok {
				continue
			}
			rel, ok := s.propertyRel(a.Slug)
			if !ok {
				httpError(w, errBadRequest("unknown property "+a.Slug))
				return
			}
			rels[a.Slug] = rel
		}
	}
	written := 0
	propsTouched := map[string]bool{}
	vendorCats, vendorProps := map[string]string{}, map[string]string{}
	for _, row := range rows {
		n := len(row.Assignments)
		for i, a := range row.Assignments {
			note := row.Note
			if n > 1 {
				note = strings.TrimSpace("split " + strconv.Itoa(i+1) + "/" + strconv.Itoa(n) +
					" of $" + strconv.FormatFloat(row.Amount, 'f', 2, 64) + " · " + row.Vendor)
			}
			rec := []string{
				row.Date, "expense", row.Category, row.Vendor, "",
				strconv.FormatFloat(a.Amount, 'f', -1, 64), "paid", note, "",
			}
			if err := s.vault.AppendLedgerRow(realestate.LedgerRel(rels[a.Slug]), realestate.LedgerHeader, rec); err != nil {
				httpError(w, err)
				return
			}
			written++
			propsTouched[a.Slug] = true
		}
		if row.Vendor != "" {
			vendorCats[row.Vendor] = row.Category
			if n == 1 {
				vendorProps[row.Vendor] = row.Assignments[0].Slug
			}
		}
	}
	s.statements.MarkApplied(b.IDs)
	s.reImport.Remember("", nil, vendorCats, vendorProps)
	list, last := s.statements.List()
	writeJSON(w, map[string]any{
		"applied": len(rows), "lines": written, "properties": len(propsTouched),
		"rows": list, "lastImport": last,
	})
}
