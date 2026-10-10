package approvals

import (
	"crypto/sha1"
	"encoding/hex"
	"regexp"
	"strconv"
	"strings"
)

// TypeOlgaRequest is something Olga asked Liber (her assistant) for that needs
// a change outside her own part of the app. Liber writes it up in the vault
// (system/olga/requests.md); the owner wanted it in Approvals "like other
// things for me to quickly address" rather than in a file he has to know to
// open. Like TypePortalProposal it carries no ApplyPath: Done and Won't do
// only record his decision.
const TypeOlgaRequest = "olga-request"

// OlgaRequest is one entry of requests.md.
type OlgaRequest struct {
	ID    string // stable: sha1 of the entry's first line, so a decided card never returns
	At    string // "2006-01-02 15:04" as Liber wrote it
	Asked string // her words
	Why   string // a short reason in parentheses, when Liber gave one
	Need  string // "What it would take", when the builder wrote one up
}

var olgaRequestHead = regexp.MustCompile(`^- (\d{4}-\d{2}-\d{2} \d{2}:\d{2}) — Olga asked: ("(?:[^"\\]|\\.)*")(?: \((.*)\))?\s*$`)

// ParseOlgaRequests reads requests.md. An entry starts with
// `- <time> — Olga asked: "<words>"` and may continue on indented lines,
// the first of them `What it would take: …`. Anything else is ignored.
func ParseOlgaRequests(text string) []OlgaRequest {
	var out []OlgaRequest
	var cur *OlgaRequest
	for _, line := range strings.Split(text, "\n") {
		if m := olgaRequestHead.FindStringSubmatch(line); m != nil {
			sum := sha1.Sum([]byte(strings.TrimSpace(line)))
			asked := m[2]
			if u, err := strconv.Unquote(asked); err == nil { // Liber writes her words with %q
				asked = u
			}
			out = append(out, OlgaRequest{ID: hex.EncodeToString(sum[:]), At: m[1], Asked: asked, Why: m[3]})
			cur = &out[len(out)-1]
			continue
		}
		if cur == nil {
			continue
		}
		if t := strings.TrimSpace(line); t != "" && (strings.HasPrefix(line, " ") || strings.HasPrefix(line, "\t")) {
			t = strings.TrimPrefix(t, "What it would take:")
			cur.Need = strings.TrimSpace(strings.TrimSpace(cur.Need + "\n" + strings.TrimSpace(t)))
		} else if t != "" {
			cur = nil
		}
	}
	return out
}

// OlgaRequestProposal is the Approvals card for one request.
func OlgaRequestProposal(r OlgaRequest) Proposal {
	title := strings.Join(strings.Fields(r.Asked), " ")
	if len([]rune(title)) > 120 {
		title = string([]rune(title)[:119]) + "…"
	}
	body := "Olga asked Liber on " + r.At + ":\n\n" + r.Asked + "\n"
	if r.Need != "" {
		body += "\nWhat it would take:\n" + r.Need + "\n"
	} else if r.Why != "" {
		body += "\nWhy Liber couldn't do it: " + r.Why + "\n"
	}
	return Proposal{ID: r.ID, Type: TypeOlgaRequest, Action: "Olga asked: " + title, Agent: "olga", Body: body}
}
