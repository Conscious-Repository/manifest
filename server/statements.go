package server

import (
	"encoding/csv"
	"net/http"
	"strconv"
	"strings"

	"manifest/realestate"
	"manifest/vaultwriter"
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
		// pass-5: uploads bind to a paying entity, remembered per source label
		"entity": s.reImport.LabelEntityFor(fhs[0].Filename),
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
		Entity    string            `json:"entity"` // paying entity — required (pass-5)
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
	if strings.TrimSpace(b.Entity) == "" {
		httpError(w, errBadRequest("paying entity is required"))
		return
	}
	s.reImport.BindLabel(b.Label, b.Entity)
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
		if strings.TrimSpace(row.Date) == "" || row.Amount == 0 {
			continue
		}
		// deposits ride in NEGATIVE after the sign convention → inflow rows
		// (rent / capital / transfer) that must also be reconciled
		amt, inflow := row.Amount, false
		if amt < 0 {
			amt, inflow = -amt, true
		}
		rows = append(rows, realestate.StatementRow{
			Date: strings.TrimSpace(row.Date), Vendor: strings.TrimSpace(row.Vendor),
			Note: strings.TrimSpace(row.Note), Amount: amt, Inflow: inflow,
			Entity: strings.TrimSpace(b.Entity),
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
		Note        *string             `json:"note"` // owner-editable — becomes the ledger note (bank plan §5)
		Assignments *[]realestate.Alloc `json:"assignments"`
		State       *string             `json:"state"`
		Reason      *string             `json:"reason"`
		// File marks a FILING gesture (property pick, category set, the
		// explicit file button) — intermediate edits (node/contract hops,
		// notes) patch without it so a half-tethered row never applies early.
		File bool `json:"file"`
	}
	if err := decode(r, &b); err != nil || b.ID == "" {
		httpError(w, errBadRequest("id is required"))
		return
	}
	row, err := s.statements.Update(b.ID, b.Category, b.Note, b.Assignments, b.State, b.Reason)
	if err != nil {
		httpError(w, err)
		return
	}
	// filing IS the write (owner call 2026-08-19): on a filing gesture, a
	// fully-filed row — assigned with a category (inflows need none) and sums
	// matching — applies to the ledger and leaves the workbench for the
	// entity's history fold. No separate bulk-apply step. Partially-filed
	// rows (say, a property but no category yet) stay in the lot untouched.
	if b.File && (row.State == "assigned" || row.State == "split") {
		if rows, err := s.statements.Applicable([]string{row.ID}); err == nil {
			if _, _, err := s.applyStatementRows(rows, vaultwriter.ActorUserAction); err != nil {
				httpError(w, err)
				return
			}
			s.statements.MarkApplied([]string{row.ID})
			row.State = "applied"
		}
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
	written, propsTouched, err := s.applyStatementRows(rows, vaultwriter.ActorUserAction)
	if err != nil {
		httpError(w, err)
		return
	}
	s.statements.MarkApplied(b.IDs)
	list, last := s.statements.List()
	writeJSON(w, map[string]any{
		"applied": len(rows), "lines": written, "properties": len(propsTouched),
		"rows": list, "lastImport": last,
	})
}

// applyStatementRows is THE row-write (bank plan §6 prerequisite): token
// assembly ([work::] [contract::] [cat::] [paid-by::] [stmt::]), split
// annotation, income-vs-expense shaping, the audited append, and vendor
// memory. Manual apply and the feed's auto-apply call the same code — only
// the audit actor differs. Callers MarkApplied on success.
func (s *Server) applyStatementRows(rows []realestate.StatementRow, actor vaultwriter.Actor) (written int, propsTouched map[string]bool, err error) {
	// resolve targets → ledger csv paths up front (fail before any write).
	// A slug of "admin:<entity>" routes to the entity's own admin ledger.
	ledgers := map[string]string{}
	for _, row := range rows {
		for _, a := range row.Assignments {
			if _, ok := ledgers[a.Slug]; ok {
				continue
			}
			if ent, isAdmin := strings.CutPrefix(a.Slug, "admin:"); isAdmin {
				if strings.TrimSpace(ent) == "" {
					return 0, nil, errBadRequest("admin assignment needs an entity")
				}
				ledgers[a.Slug] = s.realestateRootOr() + "/entities/" + slugify(ent) + ".ledger.csv"
				continue
			}
			rel, ok := s.propertyRel(a.Slug)
			if !ok {
				return 0, nil, errBadRequest("unknown property " + a.Slug)
			}
			ledgers[a.Slug] = realestate.LedgerRel(rel)
		}
	}
	propsTouched = map[string]bool{}
	vendorCats, vendorProps, vendorWork := map[string]string{}, map[string]string{}, map[string]string{}
	for _, row := range rows {
		n := len(row.Assignments)
		for i, a := range row.Assignments {
			note := row.Note
			if n > 1 {
				note = strings.TrimSpace("split " + strconv.Itoa(i+1) + "/" + strconv.Itoa(n) +
					" of $" + strconv.FormatFloat(row.Amount, 'f', 2, 64) + " · " + row.Vendor)
			}
			// tokens: work tether per alloc + budget category + the paying entity
			// + statement provenance (the accountant CSV's bank reference)
			if a.WorkID != "" {
				note = strings.TrimSpace(note + " [work:: " + a.WorkID + "]")
				if p, ok := s.realestate.Get(a.Slug); ok {
					s.tetherWorkID(p, a.WorkID) // freeze the id in the record
				}
			}
			if a.Contract != "" {
				note = strings.TrimSpace(note + " [contract:: " + a.Contract + "]") // draw-down tether (§7)
			}
			if c := strings.ToLower(strings.TrimSpace(a.Cat)); !row.Inflow && (c == realestate.CatSoft ||
				c == realestate.CatAcquisition) {
				note = strings.TrimSpace(note + " [cat:: " + c + "]")
			}
			if row.Entity != "" {
				note = strings.TrimSpace(note + " [paid-by:: " + row.Entity + "]")
			}
			if row.Statement != "" {
				note = strings.TrimSpace(note + " [stmt:: " + row.Statement + "]")
			}
			// inflows book as income rows (rent / capital) — rollups ignore them
			// (expense-only math); they exist for the books + accountant CSV
			rowType, status, cat := "expense", "paid", row.Category
			if row.Inflow {
				rowType, status = "income", "received"
				if c := strings.ToLower(strings.TrimSpace(a.Cat)); c != "" {
					cat = c
				}
			}
			rec := []string{
				row.Date, rowType, cat, row.Vendor, "",
				strconv.FormatFloat(a.Amount, 'f', -1, 64), status, note, "",
			}
			if err := s.vault.AppendLedgerRowAs(ledgers[a.Slug], realestate.LedgerHeader, rec, actor); err != nil {
				return written, propsTouched, err
			}
			written++
			propsTouched[a.Slug] = true
		}
		if row.Vendor != "" {
			vendorCats[row.Vendor] = row.Category
			if n == 1 && !strings.HasPrefix(row.Assignments[0].Slug, "admin:") {
				vendorProps[row.Vendor] = row.Assignments[0].Slug
				if row.Assignments[0].WorkID != "" {
					vendorWork[row.Vendor] = row.Assignments[0].WorkID
				}
			}
		}
	}
	s.reImport.Remember("", nil, vendorCats, vendorProps)
	s.reImport.RememberVendorWork(vendorWork)
	return written, propsTouched, nil
}
