package main

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"mime"
	"os"
	"path"
	"path/filepath"
	"strings"

	"manifest/record"
	"manifest/vaultwriter"
)

// vaultStore is the tool's only door into the vault: three capability-bound
// folders (notes, attachments, archive), each write idempotent.
type vaultStore struct {
	vw          *vaultwriter.Writer
	vault       string
	folder      string // samizdat/
	attachments string // attachments/
	archive     string // archive/
	dryRun      bool

	index     map[string]string // original image URL → attachments/<sha>.<ext>
	indexPath string
	byURL     map[string]string // canonical post URL → existing note name (stem) in folder
}

type imageStatus int

const (
	imgDownloaded imageStatus = iota
	imgReused
)

// noteName is the FILENAME a post's note gets: the title lowercased, words
// separated by single spaces, apostrophes kept, no dashes — the shape of the
// owner's own notes ("being a lizard.md"), not the URL slug. A post with no
// usable title falls back to its URL slug run through the same rule.
func noteName(title, slug string) string {
	if n := record.SlugSpaces(title, 0); n != "" {
		return n
	}
	return record.SlugSpaces(slug, 0)
}

// noteFor resolves which file in the folder holds this post: the title-named
// file when it exists (or nothing does), else the note already carrying the
// post's URL in its frontmatter — so a re-titled post, or a mirror made under
// an older naming rule, is updated in place rather than duplicated.
func (s *vaultStore) noteFor(name string, urls ...string) (string, bool) {
	if _, err := os.Stat(filepath.Join(s.vault, filepath.FromSlash(s.folder), name+".md")); err == nil {
		return name, false
	}
	s.loadURLs()
	for _, u := range urls {
		if u == "" {
			continue
		}
		if have, ok := s.byURL[canonURL(u)]; ok && have != name {
			return have, true
		}
	}
	return name, false
}

// loadURLs indexes the folder's notes by their `url:` frontmatter, once.
func (s *vaultStore) loadURLs() {
	if s.byURL != nil {
		return
	}
	s.byURL = map[string]string{}
	entries, err := os.ReadDir(filepath.Join(s.vault, filepath.FromSlash(s.folder)))
	if err != nil {
		return
	}
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".md") {
			continue
		}
		raw, err := os.ReadFile(filepath.Join(s.vault, filepath.FromSlash(s.folder), e.Name()))
		if err != nil {
			continue
		}
		if u := noteURL(string(raw)); u != "" {
			s.byURL[canonURL(u)] = strings.TrimSuffix(e.Name(), ".md")
		}
	}
}

// noteURL reads the `url:` line of a note's frontmatter ("" when absent).
func noteURL(content string) string {
	block, _, ok := record.SplitFrontmatter(content)
	if !ok {
		return ""
	}
	for _, ln := range block {
		if k, v, ok := strings.Cut(ln, ":"); ok && strings.TrimSpace(k) == "url" {
			return record.Unquote(strings.TrimSpace(v))
		}
	}
	return ""
}

// canonURL makes two spellings of one post URL compare equal: scheme, host
// case, a trailing slash and a query string do not make a different post.
func canonURL(u string) string {
	u = strings.TrimSpace(u)
	if i := strings.Index(u, "://"); i >= 0 {
		u = u[i+3:]
	}
	u = strings.TrimPrefix(u, "www.")
	if i := strings.IndexAny(u, "?#"); i >= 0 {
		u = u[:i]
	}
	u = strings.TrimSuffix(u, "/")
	if i := strings.Index(u, "/"); i >= 0 {
		return strings.ToLower(u[:i]) + u[i:]
	}
	return strings.ToLower(u)
}

// writeNote writes folder/<name>.md, replacing from source. Returns whether
// the bytes changed (an identical note is left alone: no write, no audit line).
func (s *vaultStore) writeNote(name, content string) (bool, error) {
	rel := path.Join(s.folder, name+".md")
	if old, err := os.ReadFile(filepath.Join(s.vault, filepath.FromSlash(rel))); err == nil && string(old) == content {
		return false, nil
	}
	if s.dryRun {
		return true, nil
	}
	if err := s.vw.WriteCap(capNotes, rel, []byte(content)); err != nil {
		return true, err
	}
	if s.byURL != nil {
		if u := noteURL(content); u != "" {
			s.byURL[canonURL(u)] = name
		}
	}
	return true, nil
}

// saveImage mirrors one image into attachments/<sha256>.<ext> and returns the
// vault-relative path to link. Content-addressed: the same bytes under two
// URLs land once, and a hash file already on disk is never rewritten. A
// URL→path index (outside the vault) lets a re-run skip the download too.
func (s *vaultStore) saveImage(ctx context.Context, f *fetcher, im image) (string, imageStatus, error) {
	s.loadIndex()
	if rel, ok := s.index[im.Original]; ok {
		if _, err := os.Stat(filepath.Join(s.vault, filepath.FromSlash(rel))); err == nil {
			return rel, imgReused, nil
		}
	}
	data, ctype, err := f.get(ctx, im.Original, "image/*")
	if err != nil && im.Original != im.Src {
		// The original behind the CDN URL is gone or refused: mirror what
		// the page actually renders instead.
		data, ctype, err = f.get(ctx, im.Src, "image/*")
	}
	if err != nil {
		return "", 0, err
	}
	if len(data) == 0 {
		return "", 0, errors.New("empty image body")
	}
	ext := imageExt(ctype, im.Original)
	if ext == "" {
		return "", 0, fmt.Errorf("not an image (content-type %q)", ctype)
	}
	sum := sha256.Sum256(data)
	rel := path.Join(s.attachments, hex.EncodeToString(sum[:])+ext)
	full := filepath.Join(s.vault, filepath.FromSlash(rel))
	status := imgDownloaded
	if _, err := os.Stat(full); err == nil {
		status = imgReused
	} else if !s.dryRun {
		if err := s.vw.WriteCap(capAttachments, rel, data); err != nil {
			return "", 0, err
		}
	}
	if !s.dryRun {
		s.index[im.Original] = rel
		s.saveIndex()
	}
	return rel, status, nil
}

// archiveNote moves a top-level note into archive/, never overwriting: a
// name already taken there gets a numeric suffix. Returns the destination.
func (s *vaultStore) archiveNote(name string) (string, error) {
	base := strings.TrimSuffix(name, ".md")
	dest := path.Join(s.archive, name)
	for i := 2; ; i++ {
		if _, err := os.Stat(filepath.Join(s.vault, filepath.FromSlash(dest))); err != nil {
			break
		}
		dest = path.Join(s.archive, fmt.Sprintf("%s-%d.md", base, i))
	}
	if s.dryRun {
		return dest, nil
	}
	if err := os.MkdirAll(filepath.Join(s.vault, filepath.FromSlash(s.archive)), 0o755); err != nil {
		return "", err
	}
	return dest, s.vw.RenameCap(capArchive, name, dest)
}

// imageExt picks the file extension from the content-type, falling back to
// the URL's own. "" means the payload is not an image.
func imageExt(ctype, src string) string {
	mt, _, _ := mime.ParseMediaType(ctype)
	switch mt {
	case "image/jpeg", "image/jpg":
		return ".jpg"
	case "image/png":
		return ".png"
	case "image/gif":
		return ".gif"
	case "image/webp":
		return ".webp"
	case "image/svg+xml":
		return ".svg"
	case "image/avif":
		return ".avif"
	case "image/heic":
		return ".heic"
	}
	if strings.HasPrefix(mt, "text/") || strings.Contains(mt, "html") || strings.Contains(mt, "json") {
		return ""
	}
	ext := strings.ToLower(path.Ext(strings.SplitN(src, "?", 2)[0]))
	switch ext {
	case ".jpeg":
		return ".jpg"
	case ".jpg", ".png", ".gif", ".webp", ".svg", ".avif", ".heic", ".bmp", ".tif", ".tiff":
		return ext
	}
	if strings.HasPrefix(mt, "image/") {
		return ".img"
	}
	return ""
}

func (s *vaultStore) loadIndex() {
	if s.index != nil {
		return
	}
	s.index = map[string]string{}
	home, _ := os.UserHomeDir()
	s.indexPath = filepath.Join(home, ".config", "manifest", "samizdat-images.json")
	b, err := os.ReadFile(s.indexPath)
	if err != nil {
		return
	}
	if err := json.Unmarshal(b, &s.index); err != nil {
		log.Printf("image index %s unreadable (%v); starting fresh", s.indexPath, err)
		s.index = map[string]string{}
	}
}

func (s *vaultStore) saveIndex() {
	b, err := json.MarshalIndent(s.index, "", "  ")
	if err != nil {
		return
	}
	_ = os.MkdirAll(filepath.Dir(s.indexPath), 0o755)
	if old, err := os.ReadFile(s.indexPath); err == nil && bytes.Equal(old, b) {
		return
	}
	_ = os.WriteFile(s.indexPath, b, 0o644)
}
