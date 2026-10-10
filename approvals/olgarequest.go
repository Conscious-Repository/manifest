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
	ID     string // stable: sha1 of the entry's first line, so a decided card never returns
	At     string // "2006-01-02 15:04" as Liber wrote it
	Asked  string // her words
	Why    string // a short reason in parentheses, when Liber gave one
	Need   string // the indented write-up ("What it would take: …", "Message for Benjamin: …")
	Thread string // her conversation it came from ("app:c-…", "task:<id>"), when recorded
}

var olgaRequestHead = regexp.MustCompile(`^- (\d{4}-\d{2}-\d{2} \d{2}:\d{2}) — Olga asked: ("(?:[^"\\]|\\.)*")(?: \((.*)\))?\s*$`)

// ParseOlgaRequests reads requests.md. An entry starts with
// `- <time> — Olga asked: "<words>"` and may continue on indented lines:
// `Thread: <kind>:<id>` names her conversation, the rest is Liber's write-up.
// Anything else is ignored.
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
			if th, ok := strings.CutPrefix(t, "Thread:"); ok && cur.Thread == "" {
				cur.Thread = strings.TrimSpace(th)
			} else {
				cur.Need = strings.TrimSpace(cur.Need + "\n" + t)
			}
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
		body += "\n" + r.Need + "\n"
	} else if r.Why != "" {
		body += "\nWhy Liber couldn't do it: " + r.Why + "\n"
	}
	return Proposal{ID: r.ID, Type: TypeOlgaRequest, Action: "Olga asked: " + title, Agent: "olga", Body: body}
}

// OlgaRequestByID finds one request in requests.md.
func OlgaRequestByID(text, id string) (OlgaRequest, bool) {
	for _, r := range ParseOlgaRequests(text) {
		if r.ID == id {
			return r, true
		}
	}
	return OlgaRequest{}, false
}

// OlgaAnswer is Benjamin's decision on one request, written to
// system/olga/answers/<id>.json for her server to deliver into the
// conversation it came from.
type OlgaAnswer struct {
	ID       string `json:"id"`
	Thread   string `json:"thread,omitempty"`
	Asked    string `json:"asked"`
	Decision string `json:"decision"` // done | wont
	Note     string `json:"note,omitempty"`
	At       string `json:"at"`
}
