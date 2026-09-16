package transcriptsync

import (
	"regexp"
	"strings"
)

// titleFiller are meeting-type / connector words dropped from a title before
// resolving the remaining tokens to vault people (attendee seeding, plan §4:
// "add likely contacts from my vault"). Connectors also split multi-person
// titles ("bridget and joey intro" → bridget, joey).
var titleFiller = map[string]bool{
	"intro": true, "sync": true, "call": true, "meeting": true, "interview": true,
	"brainstorm": true, "chat": true, "catch": true, "up": true, "followup": true,
	"follow": true, "review": true, "standup": true, "1on1": true, "check": true,
	"in": true, "ii": true, "iii": true, "iv": true, "and": true, "with": true,
	"the": true, "a": true, "an": true, "vs": true, "x": true, "team": true,
}

var titleTokenRe = regexp.MustCompile(`[A-Za-z0-9]+`)

// titleAttendees resolves the people named in a meeting title to canonical vault
// contacts. It splits the title into segments on connector words, then tries the
// cleaned phrase and each token against the vault's person entities, keeping only
// confident (exact or unique-prefix) matches — so "robert lufkin intro" →
// [[Robert Lufkin]] and "austin" → [[Austin Tunnell]], but a topic word links
// nothing.
func titleAttendees(idx *Index, title string) []string {
	if idx == nil {
		return nil
	}
	// tokenize, dropping filler; connectors (also filler) implicitly segment by
	// resetting the running phrase.
	var segments [][]string
	var cur []string
	for _, raw := range strings.Fields(strings.ToLower(title)) {
		tok := strings.Join(titleTokenRe.FindAllString(raw, -1), "")
		if tok == "" {
			continue
		}
		if titleFiller[tok] {
			if len(cur) > 0 {
				segments = append(segments, cur)
				cur = nil
			}
			continue
		}
		cur = append(cur, tok)
	}
	if len(cur) > 0 {
		segments = append(segments, cur)
	}

	seen := map[string]bool{}
	var out []string
	tryResolve := func(cand string) {
		if cand == "" {
			return
		}
		if d, ok := idx.ResolvePerson(cand); ok {
			if k := strings.ToLower(d); !seen[k] {
				seen[k] = true
				out = append(out, d)
			}
		}
	}
	for _, seg := range segments {
		tryResolve(strings.Join(seg, " ")) // the whole cleaned phrase first
		if len(seg) > 1 {
			for _, tok := range seg { // then individual tokens (first-name matches)
				tryResolve(tok)
			}
		}
	}
	return out
}
