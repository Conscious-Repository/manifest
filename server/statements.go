package server

import (
	"encoding/csv"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
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
	// Which sign means "money left the account" is a property of the export,
	// not of manifest — guessing it wrong inverts every row in the file. Recall
	// the owner's answer for this format, or suggest one from the data and let
	// the mapping strip put it in front of him.
	amounts := columnFloats(headers, rows, mapping["amount"])
	sign := s.reImport.SignFor(sig)
	signRemembered := realestate.SignConventionOK(sign)
	if !signRemembered {
		sign = realestate.SuggestSign(amounts)
	}
	neg, pos := 0, 0
	for _, a := range amounts {
		if a < 0 {
			neg++
		} else if a > 0 {
			pos++
		}
	}
	writeJSON(w, map[string]any{
		"label": fhs[0].Filename, "headers": headers, "rows": rows,
		"signature": sig, "mapping": mapping, "remembered": remembered != nil,
		"sign": sign, "signRemembered": signRemembered,
		"signCounts": map[string]int{"negative": neg, "positive": pos},
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
		Sign      string            `json:"sign"` // which sign is a charge (realestate.Sign*)
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
	if !realestate.SignConventionOK(b.Sign) {
		httpError(w, errBadRequest("sign convention is required — which sign is a charge"))
		return
	}
	s.reImport.BindLabel(b.Label, b.Entity)
	// every ledger line across the portfolio — no double entry, ever
	ledgerKeys := map[string]int{}
	props, _ := s.realestate.Properties()
	for _, p := range props {
		for _, lr := range p.Ledger {
			ledgerKeys[realestate.DedupeKey(lr.Date, lr.Amount, lr.Vendor)]++
		}
	}
	// the whole date column decides DD/MM vs MM/DD — never a row at a time
	raw := make([]string, 0, len(b.Rows))
	for _, row := range b.Rows {
		raw = append(raw, row.Date)
	}
	dayFirst := realestate.DateOrder(raw)

	_, vendorCat, vendorProp := s.reImport.Lookup("")
	rows := make([]realestate.StatementRow, 0, len(b.Rows))
	unparsed := 0
	for _, row := range b.Rows {
		if strings.TrimSpace(row.Date) == "" || row.Amount == 0 {
			continue
		}
		// ISO on the way in, so a CSV row and its bank-feed twin share a key
		date, ok := realestate.NormalizeDate(row.Date, dayFirst)
		if !ok {
			unparsed++
			continue
		}
		// the export says which sign is a charge; the lot stores unsigned
		// amounts plus Inflow (deposits — rent / capital / transfer — still
		// have to be reconciled)
		amt, inflow := row.Amount, false
		if b.Sign == realestate.SignExpenseNegative {
			inflow = amt > 0
		} else {
			inflow = amt < 0
		}
		if amt < 0 {
			amt = -amt
		}
		// the tidied payee is what the workbench shows and vendor memory keys;
		// the bank's raw description survives as the note when the format has
		// no separate memo column
		vendor := realestate.TidyVendor(row.Vendor)
		note := strings.TrimSpace(row.Note)
		if note == "" && vendor != strings.TrimSpace(row.Vendor) {
			note = strings.Join(strings.Fields(row.Vendor), " ")
		}
		rows = append(rows, realestate.StatementRow{
			Date: date, Vendor: vendor, Note: note, Amount: amt, Inflow: inflow,
			Entity: strings.TrimSpace(b.Entity),
		})
	}
	// A same-day same-amount hit that the exact key missed is almost always the
	// same transaction wearing a different payee string (a CSV copy of a row
	// the bank feed already delivered). Never dropped — the vendor text is the
	// only thing distinguishing two real charges — but counted, so an import
	// that overlaps a synced window says so instead of quietly doubling.
	loose := map[string]int{}
	existing, _ := s.statements.List()
	for _, r := range existing {
		loose[looseKey(r.Date, r.Amount)]++
	}
	for _, p := range props {
		for _, lr := range p.Ledger {
			loose[looseKey(lr.Date, lr.Amount)]++
		}
	}
	suspects := 0
	for _, r := range rows {
		if loose[looseKey(r.Date, r.Amount)] > 0 {
			suspects++
		}
	}
	added, dups := s.statements.Ingest(b.Label, rows, ledgerKeys, vendorCat, vendorProp)
	s.reImport.Remember(b.Signature, b.Mapping, nil, nil) // column mapping memory
	s.reImport.RememberSign(b.Signature, b.Sign)
	if suspects -= dups; suspects < 0 { // the ones already reported as exact dups
		suspects = 0
	}
	list, last := s.statements.List()
	writeJSON(w, map[string]any{"added": added, "duplicates": dups, "suspects": suspects,
		"unparsedDates": unparsed, "rows": list, "lastImport": last})
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
	// entity's history fold. No separate bulk-apply step. A row that can't
	// file yet stays in the lot and the reason RIDES BACK on the response
	// (fileError) — never a silent no-op (the 736 lesson: five rows sat
	// "assigned" for weeks behind a success-shaped 200).
	fileError := ""
	if b.File && (row.State == "assigned" || row.State == "split") {
		if rows, err := s.statements.Applicable([]string{row.ID}); err != nil {
			fileError = err.Error()
		} else {
			if _, _, err := s.applyStatementRows(rows, vaultwriter.ActorUserAction); err != nil {
				httpError(w, err)
				return
			}
			s.statements.MarkApplied([]string{row.ID})
			row.State = "applied"
		}
	}
	writeJSON(w, map[string]any{"row": row, "state": row.State, "fileError": fileError})
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
	// chart-of-accounts class lookup, memoized per apply batch: an
	// operating-class category writes [cat:: operating] so the row joins the
	// property's operating lane instead of its rehab budget
	classMemo := map[string]string{}
	classOf := func(category string) string {
		key := strings.ToLower(strings.TrimSpace(category))
		if key == "" {
			return ""
		}
		if c, ok := classMemo[key]; ok {
			return c
		}
		c := s.realestate.CategoryClass(key)
		classMemo[key] = c
		return c
	}
	for _, row := range rows {
		n := len(row.Assignments)
		for i, a := range row.Assignments {
			rec := s.statementRec(row, a, i, n, classOf)
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

// statementRec assembles ONE ledger csv record for slice i of n — note text
// (+ split annotation), the token set ([work::] [contract::] [cat::]
// [paid-by::] [stmt::]), income-vs-expense shaping. Apply and the filed-edit
// rewrite call the same code so a refile is byte-canonical.
func (s *Server) statementRec(row realestate.StatementRow, a realestate.Alloc, i, n int, classOf func(string) string) []string {
	note := row.Note
	if n > 1 {
		// keep the owner's note — the split annotation APPENDS (it used
		// to replace, discarding hand-written context on split rows)
		note = strings.TrimSpace(note + " · split " + strconv.Itoa(i+1) + "/" + strconv.Itoa(n) +
			" of $" + strconv.FormatFloat(row.Amount, 'f', 2, 64) + " · " + row.Vendor)
		note = strings.TrimPrefix(note, "· ")
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
	} else if !row.Inflow && classOf(row.Category) == "operating" {
		// operating-class category (chart of accounts) → operating lane
		note = strings.TrimSpace(note + " [cat:: " + realestate.CatOperating + "]")
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
	return []string{
		row.Date, rowType, cat, row.Vendor, "",
		strconv.FormatFloat(a.Amount, 'f', -1, 64), status, note, "",
	}
}

// statementLedgerRel resolves one assignment slug to its ledger csv path.
func (s *Server) statementLedgerRel(slug string) (string, error) {
	if ent, isAdmin := strings.CutPrefix(slug, "admin:"); isAdmin {
		if strings.TrimSpace(ent) == "" {
			return "", errBadRequest("admin assignment needs an entity")
		}
		return s.realestateRootOr() + "/entities/" + slugify(ent) + ".ledger.csv", nil
	}
	rel, ok := s.propertyRel(slug)
	if !ok {
		return "", errBadRequest("unknown property " + slug)
	}
	return realestate.LedgerRel(rel), nil
}

// locateFiledRec finds the ONE on-disk ledger record a filed slice wrote —
// matched by statement provenance + date + vendor + slice amount. Ambiguity
// or absence fails closed (the file changed underneath — reload and retry).
func (s *Server) locateFiledRec(rel string, row realestate.StatementRow, a realestate.Alloc) ([]string, error) {
	raw, err := os.ReadFile(filepath.Join(s.vault.VaultRoot(), filepath.FromSlash(rel)))
	if err != nil {
		return nil, errBadRequest("ledger not found for " + a.Slug)
	}
	var hit *realestate.LedgerRow
	for _, lr := range realestate.ParseLedgerBytes(raw) {
		if lr.Stmt == row.Statement && lr.Date == row.Date &&
			strings.EqualFold(lr.Vendor, row.Vendor) && lr.Amount == a.Amount {
			if hit != nil {
				return nil, errBadRequest("two identical ledger rows match — edit them on the property page")
			}
			l := lr
			hit = &l
		}
	}
	if hit == nil {
		return nil, errBadRequest("the written ledger row no longer matches (edited on the property page?) — edit it there or reload")
	}
	return ledgerRecord(*hit), nil
}

// handleStatementsRefile — POST /api/realestate/statements/{id}/refile: the
// filed-edit lane (owner call 2026-08-19). Two modes:
//   - {"unfile": true} deletes every written ledger row and returns the row
//     to the to-file lane (assigned) for reassignment.
//   - {category?/note?/assignments?} rewrites the written ledger row(s) in
//     place — category, note, and per-slice bid/node tethers; target slugs
//     and amounts are identity (unfile to move money).
func (s *Server) handleStatementsRefile(w http.ResponseWriter, r *http.Request) {
	if !s.statementsOK(w) {
		return
	}
	var b struct {
		Unfile      bool                `json:"unfile"`
		Category    *string             `json:"category"`
		Note        *string             `json:"note"`
		Assignments *[]realestate.Alloc `json:"assignments"`
	}
	if err := decode(r, &b); err != nil {
		httpError(w, err)
		return
	}
	row, ok := s.statements.Get(r.PathValue("id"))
	if !ok || row.State != "applied" {
		httpError(w, errBadRequest("row is not filed"))
		return
	}
	// locate every written record up front — fail before any write
	type slice struct {
		rel string
		old []string
	}
	slices := make([]slice, 0, len(row.Assignments))
	for _, a := range row.Assignments {
		rel, err := s.statementLedgerRel(a.Slug)
		if err != nil {
			httpError(w, err)
			return
		}
		rec, err := s.locateFiledRec(rel, row, a)
		if err != nil {
			httpError(w, err)
			return
		}
		slices = append(slices, slice{rel: rel, old: rec})
	}
	if b.Unfile {
		for _, sl := range slices {
			if err := s.vault.DeleteLedgerRow(sl.rel, sl.old); err != nil {
				httpError(w, err)
				return
			}
		}
		if _, err := s.statements.Unfile(row.ID); err != nil {
			httpError(w, err)
			return
		}
		list, last := s.statements.List()
		writeJSON(w, map[string]any{"unfiled": true, "rows": list, "lastImport": last})
		return
	}
	// edit in place: patch the store row, then rewrite each written record
	// through the SAME assembly apply used (byte-canonical refile)
	updated, err := s.statements.UpdateApplied(row.ID, b.Category, b.Note, b.Assignments)
	if err != nil {
		httpError(w, err)
		return
	}
	classOf := func(category string) string { return s.realestate.CategoryClass(category) }
	n := len(updated.Assignments)
	for i, a := range updated.Assignments {
		rec := s.statementRec(updated, a, i, n, classOf)
		if err := s.vault.UpdateLedgerRow(slices[i].rel, slices[i].old, rec); err != nil {
			httpError(w, err)
			return
		}
	}
	writeJSON(w, map[string]any{"row": updated, "state": updated.State})
}

// looseKey is the date+amount half of realestate.DedupeKey — the vendor-blind
// probe behind the overlap warning.
func looseKey(date string, amount float64) string {
	return strings.TrimSpace(date) + "|" + fmt.Sprintf("%.2f", amount)
}

// columnFloats pulls one named column out of an uploaded grid as numbers,
// tolerating $ , and (parenthesised negatives) the way the browser mapper does.
func columnFloats(headers []string, rows [][]string, col string) []float64 {
	idx := -1
	for i, h := range headers {
		if h == col {
			idx = i
			break
		}
	}
	if idx < 0 {
		return nil
	}
	out := make([]float64, 0, len(rows))
	for _, r := range rows {
		if idx >= len(r) {
			continue
		}
		txt := strings.TrimSpace(r[idx])
		neg := strings.HasPrefix(txt, "(") && strings.HasSuffix(txt, ")")
		txt = strings.NewReplacer("$", "", ",", "", "(", "", ")", "").Replace(txt)
		v, err := strconv.ParseFloat(strings.TrimSpace(txt), 64)
		if err != nil {
			continue
		}
		if neg {
			v = -v
		}
		out = append(out, v)
	}
	return out
}
