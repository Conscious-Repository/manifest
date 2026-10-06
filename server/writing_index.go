package server

import (
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"sync"
	"unicode"
	"unicode/utf8"
)

// The Writing library's text index: every vault note the browser lists, held
// in memory so search, tags and excerpts answer without a disk pass. A request
// refreshes it with a stat walk and re-reads only files whose (mtime, size)
// moved, so outside editors are seen on the next request. ~31 MB of notes cost
// roughly twice that resident (raw + folded); the vault is the owner's own.

type writingDoc struct {
	Path     string
	Name     string
	Modified int64
	ReadOnly bool
	mtime    int64 // ns; the change test with size
	size     int64
	raw      string
	fold     string // foldText(raw): lower-case, diacritic-free, whitespace collapsed
	nameFold string
	pathFold string
	headFold string // folded heading lines, NUL-separated
	excerpt  string
	tags     []string // distinct casings as written in this note
	tagKeys  []string // distinct folded tag keys
	openTask bool
	doneTask bool
}

type writingView struct {
	files    []*writingDoc // sorted by lower-case path, like the file list
	folders  []string
	tagName  map[string]string // key → most common casing
	tagCount map[string]int    // key → notes carrying it
}

type writingIndex struct {
	refreshMu sync.Mutex // one walk at a time; readers use the last view
	mu        sync.RWMutex
	docs      map[string]*writingDoc
	view      *writingView
	root      string
}

func (s *Server) writingTextIndex() *writingIndex {
	s.writingTextOnce.Do(func() { s.writingText = &writingIndex{} })
	return s.writingText
}

type writingStat struct {
	rel, name string
	mtime     int64
	modified  int64
	size      int64
	readOnly  bool
}

// refresh walks with the same filters as the file browser always has: no
// dotfiles, no symlinks, nothing under the writing record root, and no folder
// the owner cannot write.
func (x *writingIndex) refresh(s *Server) (*writingView, error) {
	x.refreshMu.Lock()
	defer x.refreshMu.Unlock()
	root := s.vault.VaultRoot()
	var stats []writingStat
	folders := []string{""}
	err := filepath.WalkDir(root, func(full string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if full == root {
			return nil
		}
		if strings.HasPrefix(d.Name(), ".") || d.Type()&os.ModeSymlink != 0 {
			if d.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}
		rel, _ := filepath.Rel(root, full)
		rel = filepath.ToSlash(rel)
		if s.writing != nil && (rel == s.writing.Root || strings.HasPrefix(rel, s.writing.Root+"/")) {
			if d.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}
		if d.IsDir() {
			if !s.vault.CanUserWrite(rel) {
				return filepath.SkipDir
			}
			folders = append(folders, rel)
			return nil
		}
		if !strings.HasSuffix(strings.ToLower(rel), ".md") {
			return nil
		}
		fi, err := d.Info()
		if err != nil {
			return err
		}
		stats = append(stats, writingStat{rel, strings.TrimSuffix(d.Name(), filepath.Ext(d.Name())), fi.ModTime().UnixNano(), fi.ModTime().Unix(), fi.Size(), !s.vault.CanUserWrite(rel)})
		return nil
	})
	if err != nil {
		return nil, err
	}
	x.mu.RLock()
	old, prevRoot, prevView := x.docs, x.root, x.view
	x.mu.RUnlock()
	if prevRoot != root {
		old, prevView = nil, nil
	}
	next := make(map[string]*writingDoc, len(stats))
	var stale []writingStat
	changed := prevView == nil || len(old) != len(stats)
	for _, st := range stats {
		d := old[st.rel]
		if d == nil || d.mtime != st.mtime || d.size != st.size {
			stale = append(stale, st)
			changed = true
			continue
		}
		if d.ReadOnly != st.readOnly || d.Name != st.name {
			c := *d
			c.ReadOnly, c.Name, c.nameFold = st.readOnly, st.name, foldText(st.name)
			d = &c
		}
		next[st.rel] = d
	}
	// The first build reads the whole vault; spread it over a few readers.
	if len(stale) > 0 {
		out := make([]*writingDoc, len(stale))
		workers := 8
		if len(stale) < workers {
			workers = len(stale)
		}
		var wg sync.WaitGroup
		jobs := make(chan int)
		for range workers {
			wg.Add(1)
			go func() {
				defer wg.Done()
				for i := range jobs {
					st := stale[i]
					raw, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(st.rel)))
					if err != nil {
						continue // vanished between stat and read
					}
					out[i] = buildWritingDoc(st, raw)
				}
			}()
		}
		for i := range stale {
			jobs <- i
		}
		close(jobs)
		wg.Wait()
		for _, d := range out {
			if d != nil {
				next[d.Path] = d
			}
		}
	}
	if !changed && prevView != nil && len(prevView.folders) == len(folders) {
		same := true
		for i := range folders {
			if prevView.folders[i] != folders[i] {
				same = false
				break
			}
		}
		if same && sameDocs(prevView.files, next) {
			return prevView, nil
		}
	}
	v := &writingView{folders: folders, tagName: map[string]string{}, tagCount: map[string]int{}}
	v.files = make([]*writingDoc, 0, len(next))
	casings := map[string]map[string]int{}
	for _, d := range next {
		v.files = append(v.files, d)
		for _, t := range d.tags {
			k := foldText(t)
			if casings[k] == nil {
				casings[k] = map[string]int{}
			}
			casings[k][t]++
		}
		for _, k := range d.tagKeys {
			v.tagCount[k]++
		}
	}
	for k, cs := range casings {
		best, n := "", -1
		for c, m := range cs {
			if m > n || (m == n && c < best) {
				best, n = c, m
			}
		}
		v.tagName[k] = best
	}
	sort.Slice(v.files, func(i, j int) bool { return strings.ToLower(v.files[i].Path) < strings.ToLower(v.files[j].Path) })
	sort.Strings(v.folders)
	x.mu.Lock()
	x.docs, x.view, x.root = next, v, root
	x.mu.Unlock()
	return v, nil
}

func sameDocs(files []*writingDoc, next map[string]*writingDoc) bool {
	if len(files) != len(next) {
		return false
	}
	for _, d := range files {
		if next[d.Path] != d {
			return false
		}
	}
	return true
}

func buildWritingDoc(st writingStat, b []byte) *writingDoc {
	raw := string(b)
	if !utf8.ValidString(raw) {
		raw = strings.ToValidUTF8(raw, "\uFFFD")
	}
	d := &writingDoc{Path: st.rel, Name: st.name, Modified: st.modified, ReadOnly: st.readOnly, mtime: st.mtime, size: st.size, raw: raw}
	d.fold = foldText(raw)
	d.nameFold = foldText(st.name)
	d.pathFold = foldText(st.rel)
	body := stripFrontmatter(raw)
	d.excerpt = writingExcerpt(body, 160)
	d.tags = extractHashtags(body)
	seen := map[string]bool{}
	for _, t := range d.tags {
		if k := foldText(t); !seen[k] {
			seen[k] = true
			d.tagKeys = append(d.tagKeys, k)
		}
	}
	var heads []string
	fence := ""
	for line := range strings.SplitSeq(body, "\n") {
		if f := fenceMarker(line); f != "" {
			if fence == "" {
				fence = f
			} else if strings.HasPrefix(f, fence[:1]) && len(f) >= len(fence) {
				fence = ""
			}
			continue
		}
		if fence != "" {
			continue
		}
		if headingRe.MatchString(line) {
			heads = append(heads, foldText(line))
		}
	}
	d.headFold = strings.Join(heads, "\x00")
	d.openTask = openTaskRe.MatchString(body)
	d.doneTask = doneTaskRe.MatchString(body)
	return d
}

var (
	headingRe  = regexp.MustCompile(`^ {0,3}#{1,6}(\s|$)`)
	openTaskRe = regexp.MustCompile(`(?m)^[ \t]*(?:[-*+]|\d+[.)])[ \t]+\[ \]`)
	doneTaskRe = regexp.MustCompile(`(?m)^[ \t]*(?:[-*+]|\d+[.)])[ \t]+\[[xX]\]`)
)

// stripFrontmatter drops a leading YAML block. An unclosed block is body text.
func stripFrontmatter(raw string) string {
	raw = strings.TrimPrefix(raw, "\uFEFF")
	if !strings.HasPrefix(raw, "---\n") && !strings.HasPrefix(raw, "---\r\n") {
		return raw
	}
	i := strings.IndexByte(raw, '\n') + 1
	for i < len(raw) {
		j := strings.IndexByte(raw[i:], '\n')
		line, next := raw[i:], len(raw)
		if j >= 0 {
			line, next = raw[i:i+j], i+j+1
		}
		if l := strings.TrimRight(line, " \t\r"); l == "---" || l == "..." {
			return raw[next:]
		}
		i = next
	}
	return raw
}

// fenceMarker returns the ``` or ~~~ run opening a fenced code line, or "".
func fenceMarker(line string) string {
	t := strings.TrimLeft(line, " ")
	if len(line)-len(t) > 3 || len(t) < 3 || (t[0] != '`' && t[0] != '~') {
		return ""
	}
	n := 0
	for n < len(t) && t[n] == t[0] {
		n++
	}
	if n < 3 {
		return ""
	}
	return t[:n]
}

var (
	excerptLead  = regexp.MustCompile(`^\s*(?:>\s*)*(?:#{1,6}\s+|[-*+]\s+|\d+[.)]\s+)?(?:\[[ xX]\]\s+)?`)
	excerptWiki  = regexp.MustCompile(`\[\[(?:[^\]|]*\|)?([^\]]*)\]\]`)
	excerptLink  = regexp.MustCompile(`!?\[([^\]]*)\]\([^)]*\)`)
	excerptHTML  = regexp.MustCompile(`<[^>]+>`)
	excerptPunct = strings.NewReplacer("*", "", "_", "", "~", "", "`", "", "==", "", "|", " ")
)

// writingExcerpt is the first readable body text: Markdown marks removed,
// whitespace collapsed, cut at a word near max runes.
func writingExcerpt(body string, max int) string {
	var b strings.Builder
	fence := false
	for line := range strings.SplitSeq(body, "\n") {
		if b.Len() > max*4 {
			break
		}
		if fenceMarker(line) != "" {
			fence = !fence
			continue
		}
		t := strings.TrimSpace(line)
		if t == "" || strings.Trim(t, "-*_= ") == "" {
			continue
		}
		t = excerptLead.ReplaceAllString(t, "")
		t = excerptWiki.ReplaceAllString(t, "$1")
		t = excerptLink.ReplaceAllString(t, "$1")
		t = excerptHTML.ReplaceAllString(t, "")
		t = excerptPunct.Replace(t)
		if t = strings.TrimSpace(t); t != "" {
			b.WriteString(t)
			b.WriteByte(' ')
		}
	}
	return clipRunes(strings.Join(strings.Fields(b.String()), " "), max)
}

func clipRunes(s string, max int) string {
	if utf8.RuneCountInString(s) <= max {
		return s
	}
	r := []rune(s)[:max]
	cut := string(r)
	if i := strings.LastIndexByte(cut, ' '); i > len(cut)/2 {
		cut = cut[:i]
	}
	return strings.TrimRight(cut, " ,;:.-") + "…"
}

// ---- hashtags --------------------------------------------------------------

func isWordRune(r rune) bool {
	return unicode.IsLetter(r) || unicode.IsDigit(r) || r == '_' || unicode.Is(unicode.Mn, r)
}

// extractHashtags follows Twitter's rules as iA Writer documents them: '#' then
// word characters with at least one letter, not preceded by a letter, digit,
// '&', '#' or '_'. Headings ("# x"), fenced and inline code, link targets,
// wikilinks and URLs never yield tags. The caller strips frontmatter.
func extractHashtags(body string) []string {
	var out []string
	seen := map[string]bool{}
	fence := ""
	for line := range strings.SplitSeq(body, "\n") {
		if f := fenceMarker(line); f != "" {
			if fence == "" {
				fence = f
			} else if f[0] == fence[0] && len(f) >= len(fence) {
				fence = ""
			}
			continue
		}
		if fence != "" || !strings.Contains(line, "#") {
			continue
		}
		tokenStart := 0
		prev := rune(-1)
		for i := 0; i < len(line); {
			r, w := utf8.DecodeRuneInString(line[i:])
			switch {
			case r == '`':
				n := 0
				for i+n < len(line) && line[i+n] == '`' {
					n++
				}
				run := line[i : i+n]
				if j := closingBackticks(line[i+n:], n); j >= 0 {
					i += n + j + len(run)
				} else {
					i += n
				}
				prev = '`'
				continue
			case r == ']' && strings.HasPrefix(line[i:], "]("):
				if j := strings.IndexByte(line[i:], ')'); j >= 0 {
					i += j + 1
					prev = ')'
					continue
				}
			case r == '[' && strings.HasPrefix(line[i:], "[["):
				if j := strings.Index(line[i:], "]]"); j >= 0 {
					i += j + 2
					prev = ']'
					continue
				}
			case unicode.IsSpace(r):
				tokenStart = i + w
			case r == '#':
				ok := prev == -1 || !(unicode.IsLetter(prev) || unicode.IsDigit(prev) || prev == '&' || prev == '#' || prev == '_')
				if tok := line[tokenStart:i]; ok && (strings.Contains(tok, "://") || strings.HasPrefix(strings.ToLower(tok), "www.")) {
					ok = false
				}
				j, letter := i+1, false
				for j < len(line) {
					r2, w2 := utf8.DecodeRuneInString(line[j:])
					if !isWordRune(r2) {
						break
					}
					letter = letter || unicode.IsLetter(r2)
					j += w2
				}
				if ok && letter {
					if t := line[i+1 : j]; !seen[t] {
						seen[t] = true
						out = append(out, t)
					}
				}
				if j > i+1 {
					prev, _ = utf8.DecodeLastRuneInString(line[:j])
					i = j
					continue
				}
			}
			prev = r
			i += w
		}
	}
	return out
}

// closingBackticks finds a run of exactly n backticks in s (CommonMark code span).
func closingBackticks(s string, n int) int {
	for i := 0; i < len(s); {
		if s[i] != '`' {
			i++
			continue
		}
		m := 0
		for i+m < len(s) && s[i+m] == '`' {
			m++
		}
		if m == n {
			return i
		}
		i += m
	}
	return -1
}

// ---- folding ---------------------------------------------------------------

var foldMap = func() map[rune]rune {
	m := map[rune]rune{}
	for base, set := range map[rune]string{
		'a': "àáâãäåāăą", 'c': "çćĉċč", 'd': "ďđ", 'e': "èéêëēĕėęě", 'g': "ĝğġģ",
		'h': "ĥħ", 'i': "ìíîïĩīĭįı", 'j': "ĵ", 'k': "ķ", 'l': "ĺļľŀł", 'n': "ñńņňŉ",
		'o': "òóôõöøōŏő", 'r': "ŕŗř", 's': "śŝşšș", 't': "ţťŧț", 'u': "ùúûüũūŭůűų",
		'w': "ŵ", 'y': "ýÿŷ", 'z': "źżž",
	} {
		for _, r := range set {
			m[r] = base
		}
	}
	return m
}()

// foldStep maps one source rune into the folded text: -1 drops it (combining
// marks, repeated whitespace). Search and snippet offsets share it exactly.
func foldStep(r rune, lastSpace bool) rune {
	if unicode.IsSpace(r) {
		if lastSpace {
			return -1
		}
		return ' '
	}
	if unicode.Is(unicode.Mn, r) {
		return -1
	}
	if r < utf8.RuneSelf {
		if 'A' <= r && r <= 'Z' {
			return r + 32
		}
		return r
	}
	r = unicode.ToLower(r)
	if b, ok := foldMap[r]; ok {
		return b
	}
	return r
}

func foldText(s string) string {
	var b strings.Builder
	b.Grow(len(s))
	last := false
	for _, r := range s {
		f := foldStep(r, last)
		if f < 0 {
			continue
		}
		last = f == ' '
		b.WriteRune(f)
	}
	return b.String()
}

// origOffset maps a byte offset in foldText(s) back to one in s.
func origOffset(s string, foldOff int) int {
	n, last := 0, false
	for i, r := range s {
		if n >= foldOff {
			return i
		}
		f := foldStep(r, last)
		if f < 0 {
			continue
		}
		last = f == ' '
		n += utf8.RuneLen(f)
	}
	return len(s)
}
