package personalemail

import (
	"database/sql"
	"strings"
)

// Index uses the canonical Manifest contact predicate used by legacy vaultquery.
// Preloading propagates SQL errors instead of treating an unavailable index as
// an empty contact set and advancing past mail.
type Index struct {
	DB       *sql.DB
	contacts map[string]string
}

func (i *Index) Refresh() error {
	rows, err := i.DB.Query(`SELECT em.email_lower,e.display FROM note_emails em JOIN entities e ON e.note_path=em.path JOIN notes n ON n.path=em.path WHERE n.zone='knowledge' AND n.ai_authored=0 AND e.display!='' ORDER BY em.email_lower,e.display`)
	if err != nil {
		return err
	}
	defer rows.Close()
	contacts := map[string]string{}
	for rows.Next() {
		var email, name string
		if err := rows.Scan(&email, &name); err != nil {
			return err
		}
		if _, ok := contacts[email]; !ok {
			contacts[email] = name
		}
	}
	if err := rows.Err(); err != nil {
		return err
	}
	i.contacts = contacts
	return nil
}
func (i *Index) PersonByEmail(email string) (string, bool) {
	n, ok := i.contacts[strings.ToLower(strings.TrimSpace(email))]
	return n, ok
}
func (i *Index) Paths(id string) ([]string, error) {
	rows, err := i.DB.Query(`SELECT path FROM notes WHERE gmail_thread_id=? ORDER BY path`, id)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var paths []string
	for rows.Next() {
		var p string
		if err := rows.Scan(&p); err != nil {
			return nil, err
		}
		paths = append(paths, p)
	}
	return paths, rows.Err()
}
