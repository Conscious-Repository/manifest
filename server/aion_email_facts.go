package server

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"

	"manifest/aion"
	"manifest/vaultindex"
)

// aionEmailThreads projects the personal-email worker's output for the mail
// digest WITHOUT a second mail pipeline: the synced thread notes the vault
// index already carries (category aion — the worker's workspace tag — with a
// gmail-thread-id), the people they link, and the extractor's candidates
// for them from the domain-extraction jobs. The note text is loaded only to
// feed the sensitivity scan (EmailThread.Text is never rendered).
func aionEmailThreads(ix *vaultindex.Index, vaultRoot, jobsDir string, tm aion.TierMap) ([]aion.EmailThread, error) {
	if ix == nil {
		return nil, nil
	}
	rows, err := ix.DB().Query(`SELECT n.path, n.name, n.date, n.gmail_thread_id
		FROM notes n JOIN note_categories c ON c.path = n.path
		WHERE c.category = ? AND n.gmail_thread_id != ''
		ORDER BY n.path`, aion.TranscriptCategory)
	if err != nil {
		return nil, err
	}
	type row struct{ path, name, date, thread string }
	var found []row
	for rows.Next() {
		var r row
		if err := rows.Scan(&r.path, &r.name, &r.date, &r.thread); err != nil {
			rows.Close()
			return nil, err
		}
		found = append(found, r)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return nil, err
	}
	actions := aionExtractorActions(jobsDir)
	var out []aion.EmailThread
	for _, r := range found {
		t := aion.EmailThread{Name: r.name, ThreadID: r.thread, Date: r.date}
		_, t.EndDate, _ = aion.SplitTranscriptName(r.name + ".md")
		t.Subject = aion.EmailSubjectFromName(r.name, r.thread)
		t.Tier, t.Mapped = tm.Tier(filepath.Base(r.path))
		prow, err := ix.DB().Query(`SELECT DISTINCT e.display FROM links l JOIN entities e ON e.key = l.target_key
			WHERE l.src_path = ? AND e.is_person = 1 AND e.display != '' ORDER BY e.display`, r.path)
		if err == nil {
			for prow.Next() {
				var d string
				if prow.Scan(&d) == nil {
					t.Senders = append(t.Senders, d)
				}
			}
			prow.Close()
		}
		if b, err := os.ReadFile(filepath.Join(vaultRoot, filepath.FromSlash(r.path))); err == nil {
			t.Text = string(b)
		}
		t.Actions = actions[r.path]
		out = append(out, t)
	}
	return out, nil
}

// aionExtractorActions reads the completed aion extraction jobs and maps each
// source document (vault-relative path) to the extractor's candidate lines —
// kind + title only, as the extractor itself titled them. Job files carry
// the full source text; only the action strings are kept.
func aionExtractorActions(jobsDir string) map[string][]aion.EmailAction {
	out := map[string][]aion.EmailAction{}
	entries, err := os.ReadDir(jobsDir)
	if err != nil {
		return out
	}
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".json") {
			continue
		}
		b, err := os.ReadFile(filepath.Join(jobsDir, e.Name()))
		if err != nil {
			continue
		}
		var job struct {
			State string `json:"state"`
			Input struct {
				Ritual    string `json:"ritual"`
				Documents []struct {
					Name string `json:"name"`
				} `json:"documents"`
			} `json:"input"`
			Candidates []struct {
				Action string `json:"action"`
				Body   string `json:"body"`
			} `json:"candidates"`
		}
		if json.Unmarshal(b, &job) != nil || job.Input.Ritual != "aion" || job.State != "completed" {
			continue
		}
		docs := map[string]bool{}
		for _, d := range job.Input.Documents {
			docs[d.Name] = true
		}
		for _, c := range job.Candidates {
			a, ok := aion.ParseExtractorAction(c.Action)
			if !ok {
				continue
			}
			// the candidate names its source as the first line of Body
			// ("Source: <doc>"); fall back to every document of a
			// single-source job
			src := ""
			if rest, ok := strings.CutPrefix(c.Body, "Source: "); ok {
				src = strings.TrimSpace(strings.SplitN(rest, "\n", 2)[0])
			}
			if src != "" && docs[src] {
				out[src] = append(out[src], a)
				continue
			}
			if len(docs) == 1 {
				for d := range docs {
					out[d] = append(out[d], a)
				}
			}
		}
	}
	return out
}
