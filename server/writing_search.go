package server

import (
	"errors"
	"net/http"
	"slices"
	"sort"
	"strconv"
	"strings"
	"unicode/utf8"
)

// Library search over the writing index, in iA Writer's query language:
// bare terms are case- and diacritic-insensitive word-start prefixes, all
// required; "phrase"; -x or NOT x; OR/AND and parentheses; #tag and -# (no
// tags); [ ] / [x] tasks; name:/path: substrings; NEAR(a b [n]) within n words.

type wq struct {
	op    string // and or not term phrase tag anytag open done name path near
	kids  []*wq
	text  string
	terms []string
	n     int
}

type qtok struct {
	k byte // w word, p phrase, ( ), - negate, O OR, A AND, N NOT, n NEAR, o [ ], d [x]
	s string
}

func tokenizeWritingQuery(q string) ([]qtok, error) {
	var out []qtok
	for i := 0; i < len(q); {
		c := q[i]
		switch {
		case c == ' ' || c == '\t' || c == '\n' || c == '\r':
			i++
		case c == '(' || c == ')':
			out = append(out, qtok{k: c})
			i++
		case c == '-':
			if i+1 < len(q) && q[i+1] != ' ' && q[i+1] != ')' {
				out = append(out, qtok{k: '-'})
			}
			i++
		case c == '"':
			j := strings.IndexByte(q[i+1:], '"')
			if j < 0 {
				return nil, errors.New("A quoted phrase is missing its closing quote.")
			}
			out = append(out, qtok{k: 'p', s: q[i+1 : i+1+j]})
			i += j + 2
		case c == '[' && i+2 < len(q) && q[i+2] == ']' && strings.IndexByte(" xX", q[i+1]) >= 0:
			k := byte('o')
			if q[i+1] != ' ' {
				k = 'd'
			}
			out = append(out, qtok{k: k})
			i += 3
		case strings.HasPrefix(q[i:], "NEAR("):
			j := strings.IndexByte(q[i:], ')')
			if j < 0 {
				return nil, errors.New("NEAR( is missing its closing parenthesis.")
			}
			out = append(out, qtok{k: 'n', s: q[i+5 : i+j]})
			i += j + 1
		default:
			j := i
			for j < len(q) && strings.IndexByte(" \t\r\n()\"", q[j]) < 0 {
				j++
			}
			w := q[i:j]
			// name:"two words" keeps its quoted value with the field.
			if strings.HasSuffix(w, ":") && j < len(q) && q[j] == '"' {
				k := strings.IndexByte(q[j+1:], '"')
				if k < 0 {
					return nil, errors.New("A quoted phrase is missing its closing quote.")
				}
				w += q[j+1 : j+1+k]
				j += k + 2
			}
			switch w {
			case "OR":
				out = append(out, qtok{k: 'O'})
			case "AND":
				out = append(out, qtok{k: 'A'})
			case "NOT":
				out = append(out, qtok{k: 'N'})
			default:
				out = append(out, qtok{k: 'w', s: w})
			}
			i = j
		}
	}
	return out, nil
}

type qparser struct {
	t []qtok
	i int
}

func (p *qparser) peek() byte {
	if p.i < len(p.t) {
		return p.t[p.i].k
	}
	return 0
}

func parseWritingQuery(q string) (*wq, error) {
	toks, err := tokenizeWritingQuery(q)
	if err != nil {
		return nil, err
	}
	if len(toks) == 0 {
		return nil, errors.New("Type something to search for.")
	}
	p := &qparser{t: toks}
	n, err := p.or()
	if err != nil {
		return nil, err
	}
	if p.i < len(p.t) {
		return nil, errors.New("The search has an unmatched parenthesis.")
	}
	return n, nil
}

func (p *qparser) or() (*wq, error) {
	first, err := p.and()
	if err != nil {
		return nil, err
	}
	kids := []*wq{first}
	for p.peek() == 'O' {
		p.i++
		n, err := p.and()
		if err != nil {
			return nil, err
		}
		kids = append(kids, n)
	}
	if len(kids) == 1 {
		return first, nil
	}
	return &wq{op: "or", kids: kids}, nil
}

func (p *qparser) and() (*wq, error) {
	var kids []*wq
	for {
		switch p.peek() {
		case 0, ')', 'O':
			if len(kids) == 0 {
				return nil, errors.New("OR, AND and parentheses need a search term on each side.")
			}
			if len(kids) == 1 {
				return kids[0], nil
			}
			return &wq{op: "and", kids: kids}, nil
		case 'A':
			p.i++
			continue
		}
		n, err := p.unary()
		if err != nil {
			return nil, err
		}
		kids = append(kids, n)
	}
}

func (p *qparser) unary() (*wq, error) {
	if k := p.peek(); k == 'N' || k == '-' {
		p.i++
		n, err := p.unary()
		if err != nil {
			return nil, err
		}
		return &wq{op: "not", kids: []*wq{n}}, nil
	}
	if p.i >= len(p.t) {
		return nil, errors.New("NOT and - need a search term after them.")
	}
	t := p.t[p.i]
	p.i++
	switch t.k {
	case '(':
		n, err := p.or()
		if err != nil {
			return nil, err
		}
		if p.peek() != ')' {
			return nil, errors.New("The search has an unmatched parenthesis.")
		}
		p.i++
		return n, nil
	case 'p':
		f := strings.TrimSpace(foldText(t.s))
		if f == "" {
			return nil, errors.New("A quoted phrase is empty.")
		}
		return &wq{op: "phrase", text: f}, nil
	case 'o':
		return &wq{op: "open"}, nil
	case 'd':
		return &wq{op: "done"}, nil
	case 'n':
		fields := strings.Fields(strings.ReplaceAll(t.s, `"`, " "))
		n := &wq{op: "near", n: 10}
		if len(fields) > 2 {
			if v, err := strconv.Atoi(fields[len(fields)-1]); err == nil && v > 0 {
				n.n, fields = v, fields[:len(fields)-1]
			}
		}
		for _, f := range fields {
			n.terms = append(n.terms, foldText(f))
		}
		if len(n.terms) < 2 {
			return nil, errors.New("NEAR( needs at least two words.")
		}
		return n, nil
	case 'w':
		return writingAtom(t.s)
	}
	return nil, errors.New("The search has an unmatched parenthesis.")
}

func writingAtom(w string) (*wq, error) {
	lw := strings.ToLower(w)
	switch {
	case w == "#":
		return &wq{op: "anytag"}, nil
	case strings.HasPrefix(w, "#"):
		return &wq{op: "tag", text: foldText(w[1:])}, nil
	case strings.HasPrefix(lw, "name:") || strings.HasPrefix(lw, "path:"):
		v := foldText(w[5:])
		if strings.TrimSpace(v) == "" {
			return nil, errors.New(lw[:5] + " needs text after it.")
		}
		return &wq{op: lw[:4], text: v}, nil
	}
	return &wq{op: "term", text: foldText(w)}, nil
}

// wordAt is the first occurrence of t in hay at a word start; a term opening
// with punctuation matches anywhere.
func wordAt(hay, t string, from int) int {
	first, _ := utf8.DecodeRuneInString(t)
	anchored := isWordRune(first)
	for from <= len(hay) {
		j := strings.Index(hay[from:], t)
		if j < 0 {
			return -1
		}
		at := from + j
		if !anchored || at == 0 {
			return at
		}
		if prev, _ := utf8.DecodeLastRuneInString(hay[:at]); !isWordRune(prev) {
			return at
		}
		from = at + 1
	}
	return -1
}

func countWord(hay, t string, cap int) int {
	n := 0
	for at := wordAt(hay, t, 0); at >= 0 && n < cap; at = wordAt(hay, t, at+len(t)) {
		n++
	}
	return n
}

// nearAt finds every term within n words; returns the window's fold offset.
func nearAt(fold string, terms []string, n int) int {
	for _, t := range terms {
		if wordAt(fold, t, 0) < 0 {
			return -1
		}
	}
	lastIdx := make([]int, len(terms))
	lastOff := make([]int, len(terms))
	for i := range lastIdx {
		lastIdx[i] = -1
	}
	word := 0
	for i := 0; i < len(fold); {
		r, w := utf8.DecodeRuneInString(fold[i:])
		if !isWordRune(r) {
			i += w
			continue
		}
		j := i
		for j < len(fold) {
			r2, w2 := utf8.DecodeRuneInString(fold[j:])
			if !isWordRune(r2) {
				break
			}
			j += w2
		}
		hit := false
		for k, t := range terms {
			if strings.HasPrefix(fold[i:j], t) {
				lastIdx[k], lastOff[k], hit = word, i, true
			}
		}
		if hit {
			lo, off := word, i
			for k := range terms {
				if lastIdx[k] < 0 {
					lo = -1
					break
				}
				if lastIdx[k] < lo {
					lo, off = lastIdx[k], lastOff[k]
				}
			}
			if lo >= 0 && word-lo <= n {
				return off
			}
		}
		word++
		i = j
	}
	return -1
}

func (q *wq) match(d *writingDoc) bool {
	switch q.op {
	case "and":
		for _, k := range q.kids {
			if !k.match(d) {
				return false
			}
		}
		return true
	case "or":
		return slices.ContainsFunc(q.kids, func(k *wq) bool { return k.match(d) })
	case "not":
		return !q.kids[0].match(d)
	case "term":
		return wordAt(d.nameFold, q.text, 0) >= 0 || wordAt(d.fold, q.text, 0) >= 0
	case "phrase":
		return strings.Contains(d.nameFold, q.text) || strings.Contains(d.fold, q.text)
	case "tag":
		return slices.Contains(d.tagKeys, q.text)
	case "anytag":
		return len(d.tagKeys) > 0
	case "open":
		return d.openTask
	case "done":
		return d.doneTask
	case "name":
		return strings.Contains(d.nameFold, q.text)
	case "path":
		return strings.Contains(d.pathFold, q.text)
	case "near":
		return nearAt(d.fold, q.terms, q.n) >= 0
	}
	return false
}

type writingHit struct {
	score int
	first int // earliest positive body match in fold, -1 for none
}

func (h *writingHit) at(off int) {
	if off >= 0 && (h.first < 0 || off < h.first) {
		h.first = off
	}
}

// score walks only the branches that made d match: a filename hit outweighs
// any number of heading hits, which outweigh body occurrences.
func (q *wq) score(d *writingDoc, h *writingHit) {
	switch q.op {
	case "and":
		for _, k := range q.kids {
			k.score(d, h)
		}
	case "or":
		for _, k := range q.kids {
			if k.match(d) {
				k.score(d, h)
			}
		}
	case "term":
		if wordAt(d.nameFold, q.text, 0) >= 0 {
			h.score += 1000
		}
		h.score += 50*countWord(d.headFold, q.text, 10) + countWord(d.fold, q.text, 100)
		h.at(wordAt(d.fold, q.text, 0))
	case "phrase":
		if strings.Contains(d.nameFold, q.text) {
			h.score += 1000
		}
		h.score += 50*min(strings.Count(d.headFold, q.text), 10) + min(strings.Count(d.fold, q.text), 100)
		h.at(strings.Index(d.fold, q.text))
	case "near":
		h.score += 20
		h.at(nearAt(d.fold, q.terms, q.n))
	case "name":
		h.score += 1000
	case "path":
		h.score += 10
	case "tag":
		h.score += 5
	case "open", "done", "anytag":
		h.score++
	}
}

// writingSnippet is ~max runes of the raw note around a fold offset.
func writingSnippet(raw string, foldOff, max int) string {
	at := origOffset(raw, foldOff)
	start := at
	for n := 0; start > 0 && n < max/3; n++ {
		_, w := utf8.DecodeLastRuneInString(raw[:start])
		start -= w
	}
	// Prefer starting at a word boundary when one is close.
	if start > 0 {
		if i := strings.IndexAny(raw[start:at], " \n\t"); i >= 0 {
			start += i + 1
		}
	}
	end := at
	for n := 0; end < len(raw) && n < max-utf8.RuneCountInString(raw[start:at]); n++ {
		_, w := utf8.DecodeRuneInString(raw[end:])
		end += w
	}
	s := strings.Join(strings.Fields(raw[start:end]), " ")
	if start > 0 {
		s = "…" + s
	}
	if end < len(raw) {
		s += "…"
	}
	return s
}

func (s *Server) handleWritingSearch(w http.ResponseWriter, r *http.Request) {
	if s.vault == nil || !s.vault.Enabled() {
		http.Error(w, "vault unavailable", 503)
		return
	}
	raw := strings.TrimSpace(r.URL.Query().Get("q"))
	if raw == "" {
		http.Error(w, "Type something to search for.", 400)
		return
	}
	if len(raw) > 2000 {
		http.Error(w, "That search is too long.", 400)
		return
	}
	q, err := parseWritingQuery(raw)
	if err != nil {
		http.Error(w, err.Error(), 400)
		return
	}
	limit := 50
	if v, err := strconv.Atoi(r.URL.Query().Get("limit")); err == nil && v > 0 {
		limit = min(v, 500)
	}
	view, err := s.writingTextIndex().refresh(s)
	if err != nil {
		http.Error(w, "could not read the vault file list", 500)
		return
	}
	type found struct {
		d *writingDoc
		h writingHit
	}
	var hits []found
	for _, d := range view.files {
		if q.match(d) {
			h := writingHit{first: -1}
			q.score(d, &h)
			hits = append(hits, found{d, h})
		}
	}
	sort.Slice(hits, func(i, j int) bool {
		a, b := hits[i], hits[j]
		if a.h.score != b.h.score {
			return a.h.score > b.h.score
		}
		if a.d.Modified != b.d.Modified {
			return a.d.Modified > b.d.Modified
		}
		return a.d.Path < b.d.Path
	})
	type result struct {
		Path     string `json:"path"`
		Name     string `json:"name"`
		Modified int64  `json:"modified"`
		Excerpt  string `json:"excerpt"`
		Snippet  string `json:"snippet"`
		Score    int    `json:"score"`
	}
	out := []result{}
	for _, f := range hits[:min(limit, len(hits))] {
		snip := f.d.excerpt
		if f.h.first >= 0 {
			snip = writingSnippet(f.d.raw, f.h.first, 160)
		}
		out = append(out, result{f.d.Path, f.d.Name, f.d.Modified, f.d.excerpt, snip, f.h.score})
	}
	writeJSON(w, map[string]any{"results": out, "total": len(hits)})
}

func (s *Server) handleWritingTags(w http.ResponseWriter, r *http.Request) {
	if s.vault == nil || !s.vault.Enabled() {
		http.Error(w, "vault unavailable", 503)
		return
	}
	view, err := s.writingTextIndex().refresh(s)
	if err != nil {
		http.Error(w, "could not read the vault file list", 500)
		return
	}
	type tag struct {
		Tag   string `json:"tag"`
		Count int    `json:"count"`
	}
	out := make([]tag, 0, len(view.tagCount))
	for k, n := range view.tagCount {
		out = append(out, tag{view.tagName[k], n})
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Count != out[j].Count {
			return out[i].Count > out[j].Count
		}
		return strings.ToLower(out[i].Tag) < strings.ToLower(out[j].Tag)
	})
	writeJSON(w, map[string]any{"tags": out})
}
