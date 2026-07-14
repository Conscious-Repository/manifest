package studio

import (
	"database/sql"
	"os"
	"path/filepath"

	_ "modernc.org/sqlite"
)

// Corpus is a READ-ONLY handle on the engine's X corpus (vessel/corpus/x.db) —
// the same file the `excalibur x` CLI fills. The dashboard reads it for the
// Inspiration tab; it never writes.
type Corpus struct{ db *sql.DB }

// CorpusPath is <excalibur>/vessel/corpus/x.db.
func CorpusPath(excaliburRoot string) string {
	return filepath.Join(excaliburRoot, "vessel", "corpus", "x.db")
}

// OpenCorpus opens the corpus read-only. Missing file → (nil, nil): the
// Inspiration tab simply shows empty until the first backfill.
func OpenCorpus(dbPath string) (*Corpus, error) {
	if _, err := os.Stat(dbPath); err != nil {
		return nil, nil
	}
	db, err := sql.Open("sqlite", "file:"+dbPath+"?mode=ro&_pragma=busy_timeout(3000)")
	if err != nil {
		return nil, err
	}
	if err := db.Ping(); err != nil {
		db.Close()
		return nil, err
	}
	return &Corpus{db: db}, nil
}

func (c *Corpus) Close() error {
	if c == nil {
		return nil
	}
	return c.db.Close()
}

// Post is a corpus tweet with its latest engagement snapshot.
type Post struct {
	ID        string `json:"id"`
	Text      string `json:"text"`
	URL       string `json:"url"`
	CreatedAt string `json:"createdAt"`
	Views     int64  `json:"views"`
	Likes     int64  `json:"likes"`
}

// Account is a watchlist account with its top posts by views.
type Account struct {
	Handle      string `json:"handle"`
	DisplayName string `json:"displayName"`
	Bio         string `json:"bio"`
	Followers   int64  `json:"followers"`
	Commentary  string `json:"commentary"`
	TopPosts    []Post `json:"topPosts"`
}

// Watchlist returns the watchlist accounts, each with its topN highest-view
// non-reply posts.
func (c *Corpus) Watchlist(topN int) ([]Account, error) {
	if c == nil {
		return nil, nil
	}
	if topN <= 0 {
		topN = 5
	}
	rows, err := c.db.Query(`SELECT handle, display_name, bio, followers, commentary
		FROM x_accounts WHERE is_watchlist=1 ORDER BY followers DESC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var accts []Account
	for rows.Next() {
		var a Account
		if err := rows.Scan(&a.Handle, &a.DisplayName, &a.Bio, &a.Followers, &a.Commentary); err != nil {
			return nil, err
		}
		accts = append(accts, a)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	for i := range accts {
		posts, err := c.topPosts(accts[i].Handle, topN)
		if err != nil {
			return nil, err
		}
		accts[i].TopPosts = posts
	}
	return accts, nil
}

func (c *Corpus) topPosts(handle string, limit int) ([]Post, error) {
	rows, err := c.db.Query(`
		SELECT p.id, p.text, p.url, p.created_at,
			COALESCE((SELECT views FROM x_metric_snapshots s WHERE s.post_id=p.id ORDER BY captured_at DESC LIMIT 1),0) AS v,
			COALESCE((SELECT likes FROM x_metric_snapshots s WHERE s.post_id=p.id ORDER BY captured_at DESC LIMIT 1),0)
		FROM x_posts p WHERE p.handle=? AND p.is_reply=0 AND p.is_retweet=0
		ORDER BY v DESC LIMIT ?`, handle, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Post
	for rows.Next() {
		var p Post
		if err := rows.Scan(&p.ID, &p.Text, &p.URL, &p.CreatedAt, &p.Views, &p.Likes); err != nil {
			return nil, err
		}
		out = append(out, p)
	}
	return out, rows.Err()
}
