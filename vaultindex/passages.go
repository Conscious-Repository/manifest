package vaultindex

import (
	"os"

	"manifest/vaultwriter"

	"strings"
	"unicode"
	"unicode/utf8"
)

type Passage struct {
	Path     string `json:"path"`
	Name     string `json:"name"`
	Date     string `json:"date,omitempty"`
	Revision string `json:"sourceRevision"`
	Quote    string `json:"quote"`
	Start    int    `json:"start"`
	End      int    `json:"end"`
}

func SignificantTerms(text string) []string {
	words := strings.FieldsFunc(strings.ToLower(text), func(r rune) bool { return !unicode.IsLetter(r) && !unicode.IsNumber(r) })
	stop := map[string]bool{"this": true, "that": true, "with": true, "from": true, "have": true, "what": true, "when": true, "where": true, "which": true, "there": true, "their": true, "about": true, "would": true, "could": true, "should": true, "into": true, "your": true, "they": true, "them": true, "than": true, "then": true, "were": true, "been": true, "also": true}
	seen := map[string]bool{}
	out := []string{}
	for _, word := range words {
		if utf8.RuneCountInString(word) < 4 || stop[word] || seen[word] {
			continue
		}
		seen[word] = true
		out = append(out, word)
		if len(out) == 8 {
			break
		}
	}
	return out
}
func pathInFolder(p, folder string) bool {
	return folder == "" || p == folder || strings.HasPrefix(p, folder+"/")
}

// Passages uses FTS only to rank candidates, then anchors a literal passage in
// current raw bytes. Configured allowlists and daily exclusions apply twice.
func (ix *Index) Passages(text, current string, allowed, excluded []string) ([]Passage, error) {
	out := []Passage{}
	terms := SignificantTerms(text)
	if len(allowed) == 0 || len(terms) == 0 || len(text) > 12000 {
		return out, nil
	}
	clauses := []string{}
	for _, term := range terms {
		clauses = append(clauses, `"`+strings.ReplaceAll(term, `"`, `""`)+`"`)
	}
	rows, err := ix.db.Query(`SELECT n.path,n.name,n.date FROM notes_fts JOIN notes n ON n.id=notes_fts.rowid WHERE notes_fts MATCH ? AND `+knowledgeSrcSQL+` ORDER BY rank,n.path LIMIT 100`, strings.Join(clauses, " OR "))
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var p Passage
		if err = rows.Scan(&p.Path, &p.Name, &p.Date); err != nil {
			return nil, err
		}
		if p.Path == current {
			continue
		}
		permitted := false
		for _, folder := range allowed {
			if pathInFolder(p.Path, folder) {
				permitted = true
			}
		}
		for _, folder := range excluded {
			if folder != "" && pathInFolder(p.Path, folder) {
				permitted = false
			}
		}
		if !permitted {
			continue
		}
		full, err := vaultwriter.SafePath(ix.cfg.VaultRoot, p.Path)
		if err != nil {
			continue
		}
		fi, err := os.Stat(full)
		if err != nil || fi.Size() > 4<<20 {
			continue
		}
		raw, err := os.ReadFile(full)
		if err != nil || !utf8.Valid(raw) {
			continue
		}
		// Apply current path/zone policy and derive dates from current bytes.
		note := ParseNote(p.Path, raw, 0, ix.cfg.aiRegions())
		if ix.cfg.zoneOf(p.Path) != "knowledge" || note.AIAuthored {
			continue
		}
		p.Date = note.Date
		// Lowercase Unicode can change byte width: locate words in original text.
		pos := -1
		offset := 0
		for _, line := range strings.SplitAfter(string(raw), "\n") {
			for _, term := range terms {
				if strings.Contains(strings.ToLower(line), term) {
					pos = offset
					break
				}
			}
			if pos >= 0 {
				break
			}
			offset += len(line)
		}
		if pos < 0 {
			continue
		}
		p.Start = pos
		p.End = pos + 1200
		if p.End > len(raw) {
			p.End = len(raw)
		}
		for p.End < len(raw) && !utf8.RuneStart(raw[p.End]) {
			p.End--
		}
		p.Quote = string(raw[p.Start:p.End])
		p.Revision = vaultwriter.Revision(raw)
		out = append(out, p)
		if len(out) == 6 {
			break
		}
	}
	return out, rows.Err()
}
