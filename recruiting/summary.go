package recruiting

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"regexp"
	"strings"
	"time"

	"manifest/recruiting/sources"
)

// These fields are derived operational data, never owner-authored record prose.
type CandidateSummary struct {
	Text        string             `json:"text"`
	Model       string             `json:"model"`
	Agent       string             `json:"agent"`
	Fingerprint string             `json:"fingerprint"`
	GeneratedAt time.Time          `json:"generatedAt"`
	Evidence    []sources.Evidence `json:"evidence"`
}
type SummaryPacket struct {
	Version     string              `json:"version"`
	Name        string              `json:"name"`
	Role        Role                `json:"role"`
	Brief       []sources.BriefItem `json:"brief"`
	Evidence    []sources.Evidence  `json:"evidence"`
	Location    string              `json:"location"`
	Connections []PathClaim         `json:"connections"`
}
type SummaryComplete func(context.Context, SummaryPacket) (reply, model string, err error)

// Summarize is a second, private pass after enhancement. Snapshot + compare
// avoids holding the queue lock over inference or overwriting a newer decision.
func (r *RunStore) Summarize(ctx context.Context, runID, draftID string, complete SummaryComplete, now time.Time) (Run, error) {
	r.mu.Lock()
	run, err := r.load(runID)
	if err != nil {
		r.mu.Unlock()
		return Run{}, err
	}
	i, err := run.find(draftID)
	if err != nil {
		r.mu.Unlock()
		return Run{}, err
	}
	d := run.Drafts[i]
	if d.Draft.Brief == nil || d.Enhancement == nil || !d.Enhancement.Brief {
		r.mu.Unlock()
		return r.Get(runID)
	}
	original, _ := json.Marshal(d)
	finder := r.pathFinder()
	projected := r.project(run, finder)
	names := map[string]string{}
	for _, person := range finder.people {
		names[person.ID] = person.Name
	}
	names[d.CandidateID] = d.Draft.Name
	for _, key := range extKeysOfDraft(d.Draft) {
		names[key] = d.Draft.Name
	}
	connections := append([]PathClaim(nil), projected.Drafts[i].Paths...)
	for j := range connections {
		hops := strings.Split(connections[j].Path, pathSep)
		for k, id := range hops {
			if name := names[id]; name != "" {
				hops[k] = name
			}
		}
		connections[j].Path = strings.Join(hops, " → ")
	}
	roleID := d.Draft.Role
	if roleID == "" {
		roleID = run.Scope.Role
	}
	role := r.store.roleView(roleID)
	if len(role.Posting) > 12000 {
		role.Posting = role.Posting[:12000]
	}
	evidence := append([]sources.Evidence(nil), d.Draft.Brief.Evidence...)
	for j := range evidence {
		evidence[j].RetrievedAt = time.Time{}
	}
	packet := SummaryPacket{Version: "kairos-candidate-v1", Name: d.Draft.Name, Role: role, Brief: d.Draft.Brief.Items, Evidence: evidence, Location: d.Draft.Location, Connections: connections}
	bytes, _ := json.Marshal(packet)
	hash := sha256.Sum256(bytes)
	fingerprint := hex.EncodeToString(hash[:])
	if d.Summary != nil && d.Summary.Fingerprint == fingerprint && d.SummaryError == "" {
		r.mu.Unlock()
		return projected, nil
	}
	r.mu.Unlock()
	var summary *CandidateSummary
	failure := ""
	if complete == nil {
		failure = "Kairos is unavailable; enhanced evidence is still available."
	} else {
		reply, model, callErr := complete(ctx, packet)
		if callErr != nil {
			failure = "Kairos could not finish the summary. Enhance again to retry."
		} else {
			text, parseErr := summaryText(reply, packet)
			if parseErr != nil {
				failure = "Kairos returned an unsupported summary. Enhance again to retry."
			} else {
				summary = &CandidateSummary{Text: text, Model: model, Agent: "kairos-private", Fingerprint: fingerprint, GeneratedAt: now.UTC(), Evidence: d.Draft.Brief.Evidence}
			}
		}
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	latest, err := r.load(runID)
	if err != nil {
		return Run{}, err
	}
	j, err := latest.find(draftID)
	if err != nil {
		return Run{}, err
	}
	current, _ := json.Marshal(latest.Drafts[j])
	if string(current) != string(original) {
		return Run{}, fmt.Errorf("candidate changed during Kairos summary; retry from current state")
	}
	latest.Drafts[j].Summary = summary // never present an old summary as the new result
	latest.Drafts[j].SummaryError = failure
	if err := r.writeRun(latest, nil); err != nil {
		return Run{}, err
	}
	return r.project(latest, nil), nil
}

func summaryText(raw string, p SummaryPacket) (string, error) {
	type sentence struct {
		Text     string `json:"text"`
		Evidence []int  `json:"evidence"`
	}
	var result struct {
		Competencies sentence `json:"competencies"`
		Relevance    sentence `json:"relevance"`
	}
	if json.Unmarshal([]byte(strings.TrimSpace(raw)), &result) != nil {
		return "", fmt.Errorf("invalid JSON")
	}
	parts := []string{}
	for _, s := range []sentence{result.Competencies, result.Relevance} {
		text := strings.TrimSpace(strings.TrimRight(strings.TrimSpace(s.Text), ".!?"))
		if text == "" || len(strings.Fields(text)) > 40 || strings.ContainsAny(text, "\n\r!?") || regexp.MustCompile(`\.\s+`).MatchString(text) || len(s.Evidence) == 0 {
			return "", fmt.Errorf("invalid sentence")
		}
		for _, i := range s.Evidence {
			if i < 0 || i >= len(p.Evidence) || p.Evidence[i].URLOrFile == "" || p.Evidence[i].Snippet == "" {
				return "", fmt.Errorf("invalid citation")
			}
		}
		label := "Competencies: "
		if len(parts) == 1 {
			label = "AION relevance: "
		}
		if len(parts) == 1 && p.Role.Title == "" {
			text = "not established from the supplied role context"
		}
		parts = append(parts, label+text+".")
	}
	location := "current location not established"
	if strings.TrimSpace(p.Location) != "" {
		location = "recorded location: " + strings.TrimSpace(p.Location) + " (current location unverified)"
	}
	connections := "no mutual connections recorded"
	if len(p.Connections) > 0 {
		// Graph paths are evidence of a possible introduction, never a verified
		// personal relationship. Preserve the route's own uncertainty and age.
		path := p.Connections[0]
		connections = "possible introduction path: " + path.Path
		if path.Inferred {
			connections += " (inferred)"
		}
		if path.Observed != "" {
			connections += "; oldest evidence " + path.Observed
		}
	}
	parts = append(parts, strings.ToUpper(location[:1])+location[1:]+"; "+connections+".")
	return strings.Join(parts, " "), nil
}
