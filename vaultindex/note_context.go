package vaultindex

import "strings"

// ContextNotes searches authored knowledge notes by path, name or alias. Paths
// are the identity: equal titles in different folders must remain separate.
func (ix *Index) ContextNotes(query string, limit int) ([]NoteRef, error) {
	if limit < 1 || limit > 50 {
		limit = 50
	}
	q := strings.ToLower(strings.TrimSpace(query))
	rows, err := ix.db.Query(`SELECT n.path,n.name,n.date,n.mtime FROM notes n
	 WHERE n.zone='knowledge' AND n.ai_authored=0 AND
	 (instr(lower(n.path),?)>0 OR instr(lower(n.name),?)>0 OR EXISTS
	 (SELECT 1 FROM note_aliases a WHERE a.path=n.path AND instr(a.alias_lower,?)>0))
	 ORDER BY n.name_lower,n.path LIMIT ?`, q, q, q, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []NoteRef{}
	for rows.Next() {
		var n NoteRef
		if err := rows.Scan(&n.Path, &n.Name, &n.Date, &n.MTime); err != nil {
			return nil, err
		}
		out = append(out, n)
	}
	return out, rows.Err()
}

// ContextNote checks the exact indexed identity, never a name/alias resolution.
func (ix *Index) ContextNote(path string) bool {
	var count int
	err := ix.db.QueryRow(`SELECT count(*) FROM notes WHERE path=? AND zone='knowledge' AND ai_authored=0`, path).Scan(&count)
	return err == nil && count == 1
}
