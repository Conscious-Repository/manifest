package recruiting

import (
	"crypto/sha256"
	"encoding/hex"
	"regexp"
	"strings"
)

// REJECTION — the email an applicant gets when the owner rejects them in
// triage (social graph plan phase 5). The words live in ONE template the
// owner edits in place (templates/rejection.md, seeded once, never
// overwritten); this file only fills it. Sending is not here: the server
// prepares an owner-approval email, and only approving that exact email
// sends it (manifestmcp, owner-only by construction).

const RejectionTemplateFile = "templates/rejection.md"

func init() {
	SeedFiles[RejectionTemplateFile] = `---
subject: Your application for {role_title} at AION
---
Hi {first_name},

Thank you for applying for the {role_title} role at AION, and for the time you put into your application.

We've decided not to move forward with your application for this role. We'll keep your details on file and will reach out if a better fit opens up.

Best,
Ben
`
}

var rejectionPlaceholder = regexp.MustCompile(`\{[a-zA-Z_]+\}`)

// RenderRejection fills the template. The subject is the frontmatter's
// `subject:`; the body is everything after it. A placeholder this function
// does not know is an error, never sent as literal braces.
func RenderRejection(tpl, firstName, roleTitle string) (subject, body string, err error) {
	firstName, roleTitle = strings.TrimSpace(firstName), strings.TrimSpace(roleTitle)
	if firstName == "" {
		return "", "", errf("the record has no first name to address")
	}
	if roleTitle == "" {
		return "", "", errf("the record has no role title to name")
	}
	tpl = strings.ReplaceAll(tpl, "\r\n", "\n")
	if rest, ok := strings.CutPrefix(tpl, "---\n"); ok {
		if fm, after, ok := strings.Cut(rest, "\n---\n"); ok {
			for _, line := range strings.Split(fm, "\n") {
				if v, ok := strings.CutPrefix(strings.TrimSpace(line), "subject:"); ok {
					subject = strings.TrimSpace(v)
				}
			}
			tpl = after
		}
	}
	fill := strings.NewReplacer("{first_name}", firstName, "{role_title}", roleTitle)
	subject, body = fill.Replace(subject), strings.TrimSpace(fill.Replace(tpl))+"\n"
	for _, s := range []string{subject, body} {
		if m := rejectionPlaceholder.FindString(s); m != "" {
			return "", "", errf("the rejection template names %s — only {first_name} and {role_title} are filled", m)
		}
	}
	if subject == "" || strings.TrimSpace(body) == "" {
		return "", "", errf("the rejection template needs a subject: line and a body")
	}
	return subject, body, nil
}

// RejectionDraft is one applicant's rendered rejection, ready to propose.
type RejectionDraft struct {
	Candidate  string   `json:"candidate"`
	To         []string `json:"to"`
	Subject    string   `json:"subject"`
	Body       string   `json:"body"`
	Revision   string   `json:"revision"` // sha256 of exactly what would be sent
	SeenBefore []string `json:"seenBefore"`
}

// RejectionFor renders the template for one applicant.
func (s *Store) RejectionFor(id string) (RejectionDraft, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	slug, doc, err := s.resolve(id)
	if err != nil {
		return RejectionDraft{}, err
	}
	c := s.candidateView(slug, doc)
	out := RejectionDraft{Candidate: c.ID, SeenBefore: s.seenBefore(slug, c)}
	if email := strings.TrimSpace(doc.Profile()["email"]); email != "" {
		out.To = []string{email}
	}
	if err := validRecipients(out.To); err != nil {
		return out, err
	}
	title := ""
	for _, a := range c.Applications {
		if a.ID == c.AshbyApplicationID && strings.TrimSpace(a.Title) != "" {
			title = a.Title
		}
	}
	if title == "" && strings.TrimSpace(c.Role) != "" {
		slug := strings.TrimPrefix(strings.TrimSpace(c.Role), "role/")
		title = s.LoadRole(slug).View(slug, 0).Title
	}
	first := ""
	if f := strings.Fields(c.Name); len(f) > 0 {
		first = f[0]
	}
	out.Subject, out.Body, err = RenderRejection(s.raw(RejectionTemplateFile), first, title)
	if err != nil {
		return out, err
	}
	h := sha256.Sum256([]byte(strings.Join(out.To, ",") + "\x00" + out.Subject + "\x00" + out.Body))
	out.Revision = hex.EncodeToString(h[:])
	return out, nil
}

// seenBefore says — as a flag, never a filter — when this applicant has
// been here before: an earlier application on the same Ashby record, another
// record with the same name, or a pass on the same name.
func (s *Store) seenBefore(slug string, c Candidate) []string {
	out := []string{}
	for _, a := range c.Applications {
		if a.ID != c.AshbyApplicationID {
			out = append(out, "earlier application: "+strings.TrimSpace(a.Title+" · "+a.Status))
		}
	}
	key := normalizeKey(c.Name)
	for _, other := range s.CandidateSlugs() {
		if other == slug || key == "" {
			continue
		}
		od := s.LoadCandidate(other)
		if normalizeKey(od.Get("name")) == key {
			ov := s.candidateView(other, od)
			out = append(out, "another record with this name: "+ov.ID+" ("+ov.Stage+")")
		}
	}
	if p, ok := s.PassedSet()[key]; ok {
		out = append(out, "passed on before"+strings.TrimSpace(" "+p.Reason))
	}
	return out
}
