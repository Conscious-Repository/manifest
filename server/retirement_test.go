package server

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"manifest/spirits"
)

func TestPhase2RetirementLaunchPaths(t *testing.T) {
	root := t.TempDir()
	st := spirits.NewStore(root).WithHarnessName("excalibur")
	pairs := [][2]string{{"concierge", "briefing"}, {"ea-coordinator", "waiting-on"}, {"sage", "skill-cast"}}
	write := func(rel, body string) {
		t.Helper()
		p := filepath.Join(root, rel)
		if err := os.MkdirAll(filepath.Dir(p), 0755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(body), 0644); err != nil {
			t.Fatal(err)
		}
	}
	for _, p := range pairs {
		// Enabled/on-demand fixtures deliberately prove that code closes bypasses
		// independently of the deployed markdown's cadence and enabled flag.
		write("spirits/"+p[0]+"/rituals/"+p[1]+".md", "---\nritual: "+p[1]+"\n---\nfixture\n")
		write("artifacts/runs/old-"+p[1]+".md", "---\nrun: old-"+p[1]+"\nspirit: "+p[0]+"\nritual: "+p[1]+"\noutcome: completed\n---\npreserved history\n")
	}
	for _, r := range []string{"email-sync", "granola-sync", "pocket-sync"} {
		write("spirits/ea-coordinator/rituals/"+r+".md", "---\nritual: "+r+"\n---\nconnector fixture\n")
	}
	write("skills/example/SKILL.md", "---\nname: example\n---\nfixture\n")
	st.WithSkillsRoot(filepath.Join(root, "skills"))
	s := &Server{}
	s.UseHarnesses([]Harness{{Name: "excalibur", Spirits: st}})
	for _, p := range pairs {
		w := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodPost, "/api/spirits/run-now", strings.NewReader(fmt.Sprintf(`{"spirit":%q,"ritual":%q}`, p[0], p[1])))
		s.handleSpiritsRunNow(w, req)
		if w.Code != http.StatusConflict || !strings.Contains(w.Body.String(), "retired/paused") || !strings.Contains(w.Body.String(), "#/agents/ritual/"+p[0]+"/"+p[1]) {
			t.Fatalf("refusal: %d %s", w.Code, w.Body.String())
		}
		if err := st.SpoolRunNow(p[0], p[1], "fixture", ""); err == nil {
			t.Fatal("direct launch allowed")
		}
		_, body, ok := st.Run("old-" + p[1])
		if !ok || !strings.Contains(body, "preserved history") {
			t.Fatal("history inaccessible")
		}
		res, allowed, err := st.WriteFile("spirits/"+p[0]+"/rituals/"+p[1]+".md", "---\nenabled: true\n---\nfixture\n")
		if err != nil || !allowed || res.OK {
			t.Fatal("resume allowed")
		}
	}
	w := httptest.NewRecorder()
	s.handleSpiritsRituals(w, httptest.NewRequest("GET", "/api/spirits/rituals", nil))
	var data struct {
		Data []spirits.RitualRow `json:"data"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &data); err != nil {
		t.Fatal(err)
	}
	retired := 0
	for _, r := range data.Data {
		if r.Retired {
			retired++
			if r.Enabled || r.NextFire != "" || !strings.Contains(r.PausedReason, "#/agents/") {
				t.Fatalf("bad row: %+v", r)
			}
		}
	}
	if retired != 3 {
		t.Fatalf("retired=%d", retired)
	}
	for sp, rr := range st.Spirits() {
		for _, r := range rr {
			if st.RetirementReason(sp, r) != "" {
				t.Fatal("retired picker entry")
			}
		}
	}
	cast := st.Castables(time.Now())
	if len(cast) != 3 {
		t.Fatalf("connector castables=%d", len(cast))
	}
	for _, c := range cast {
		if st.RetirementReason(c.Spirit, c.Ritual) != "" {
			t.Fatal("retired cast")
		}
	}
	sp, r := delegateTargetFor(&Harness{Name: "excalibur", Spirits: st})
	if st.RetirementReason(sp, r) != "" {
		t.Fatal("retired delegation")
	}
	w = httptest.NewRecorder()
	s.handleDelegateTargets(w, httptest.NewRequest("GET", "/api/delegate/targets", nil))
	for _, p := range pairs {
		if strings.Contains(w.Body.String(), `"ritual":"`+p[1]+`"`) {
			t.Fatal("retired destination")
		}
	}
	if _, err := os.Stat(filepath.Join(root, "vessel")); !os.IsNotExist(err) {
		t.Fatal("refusal created runtime/spool state")
	}
	entries, err := os.ReadDir(filepath.Join(root, "artifacts/runs"))
	if err != nil || len(entries) != 3 {
		t.Fatal("refusal changed run artifacts")
	}
}
