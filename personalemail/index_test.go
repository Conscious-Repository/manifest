package personalemail

import (
	"database/sql"
	_ "modernc.org/sqlite"
	"testing"
)

func TestCanonicalContactScope(t *testing.T) {
	db, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	db.SetMaxOpenConns(1)
	for _, q := range []string{
		`CREATE TABLE note_emails(path TEXT,email_lower TEXT)`,
		`CREATE TABLE entities(note_path TEXT,display TEXT)`,
		`CREATE TABLE notes(path TEXT,zone TEXT,ai_authored INTEGER,gmail_thread_id TEXT)`,
		`INSERT INTO notes VALUES ('person','knowledge',0,''),('ai','knowledge',1,''),('log','log',0,'thread')`,
		`INSERT INTO entities VALUES ('person','Person'),('ai','AI'),('log','Log')`,
		`INSERT INTO note_emails VALUES ('person','known@example.com'),('ai','ai@example.com'),('log','log@example.com')`,
	} {
		if _, err = db.Exec(q); err != nil {
			t.Fatal(err)
		}
	}
	i := &Index{DB: db}
	if err = i.Refresh(); err != nil {
		t.Fatal(err)
	}
	if n, ok := i.PersonByEmail("KNOWN@example.com"); !ok || n != "Person" {
		t.Fatal(n, ok)
	}
	for _, a := range []string{"ai@example.com", "log@example.com", "missing@example.com"} {
		if _, ok := i.PersonByEmail(a); ok {
			t.Fatal("noncanonical contact admitted", a)
		}
	}
	paths, err := i.Paths("thread")
	if err != nil || len(paths) != 1 || paths[0] != "log" {
		t.Fatal(paths, err)
	}
	db.Close()
	if i.Refresh() == nil {
		t.Fatal("index failure hidden")
	}
}
