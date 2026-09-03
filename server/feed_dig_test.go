package server

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"manifest/feed"
	"manifest/hermes"
	"manifest/spirits"
)

// An alfred card is one the Hermes do-bot wrote straight into the feed dir —
// the same shape feed/alfred_probe_test.go pins.
const digAlfredCard = `---
type: paper
id: epigenetic-clocks-58f7e088
title: Responsiveness of epigenetic aging biomarkers
why: tests whether epigenetic clocks respond to interventions
link: https://www.nature.com/articles/s41591-026-04562-9
source: Nature Medicine
agent: alfred
profile: daily
domain: Aging biology
date: 2026-08-25T01:10:00Z
status: new
confidence: medium
tags: [epigenetic-clocks, aging]
---
Across 51 studies, 16 clocks were tested.
`

func digFixture(t *testing.T) (*Server, string) {
	t.Helper()
	root := t.TempDir()
	dir := filepath.Join(root, "artifacts", "feed")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "epigenetic-clocks-58f7e088.md"), []byte(digAlfredCard), 0o644); err != nil {
		t.Fatal(err)
	}
	s := New(nil, nil, nil)
	s.UseSpirits(spirits.NewStore(root))
	return s, root
}

func digStub(t *testing.T, script string) *hermes.Runner {
	t.Helper()
	stub := filepath.Join(t.TempDir(), "hermes")
	if err := os.WriteFile(stub, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	return hermes.NewRunner(hermes.Config{Enabled: true, Bin: stub})
}

func postDig(s *Server, id string) *httptest.ResponseRecorder {
	w := httptest.NewRecorder()
	s.Handler().ServeHTTP(w, httptest.NewRequest("POST", "/api/feed/"+id+"/dig", nil))
	return w
}

// A dig on an alfred card runs a Hermes turn (not a spirit ritual): the CLI
// gets the card + the harness paths, the scout's skills, a per-card session,
// and the DEFAULT toolsets — and the card it writes serves from the inbox.
func TestFeedDigAlfredRunsHermes(t *testing.T) {
	s, root := digFixture(t)
	argsFile := filepath.Join(t.TempDir(), "args")
	t.Setenv("DIG_ARGS", argsFile)
	t.Setenv("DIG_FEED_DIR", filepath.Join(root, "artifacts", "feed"))
	s.UseHermes(digStub(t, `#!/bin/sh
printf '%s\0' "$@" > "$DIG_ARGS"
cat > "$DIG_FEED_DIR/clocks-dig-abc123.md" <<'CARD'
---
type: artifact
id: clocks-dig-abc123
title: Do clocks move?
why: they do, slowly
link: artifacts/library/clocks-dig.md
agent: alfred
profile: targeted
date: 2026-09-02T12:00:00Z
status: new
confidence: medium
---
Digest of the brief.
CARD
printf 'they do, slowly — clocks-dig-abc123.md'
`), "web,memory")

	w := postDig(s, "epigenetic-clocks-58f7e088")
	if w.Code != http.StatusAccepted {
		t.Fatalf("dig: want 202, got %d: %s", w.Code, w.Body.String())
	}
	var resp map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatal(err)
	}
	if resp["spooled"] != true || resp["spirit"] != "alfred" || resp["runtime"] != "hermes-agent" {
		t.Fatalf("response = %v", resp)
	}

	// the turn runs in the background — wait for the in-flight marker to clear
	deadline := time.Now().Add(5 * time.Second)
	for {
		s.hermes.mu.Lock()
		busy := s.hermes.digging["epigenetic-clocks-58f7e088"]
		s.hermes.mu.Unlock()
		if !busy {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("dig never finished")
		}
		time.Sleep(20 * time.Millisecond)
	}

	raw, err := os.ReadFile(argsFile)
	if err != nil {
		t.Fatalf("stub never ran: %v", err)
	}
	args := strings.Split(strings.TrimRight(string(raw), "\x00"), "\x00") // NUL-separated: the prompt spans lines
	if args[0] != "-z" {
		t.Fatalf("argv = %q", args)
	}
	prompt := args[1]
	for _, want := range []string{
		"Responsiveness of epigenetic aging biomarkers",
		"https://www.nature.com/articles/s41591-026-04562-9",
		root + "/artifacts/library/",
		root + "/artifacts/feed/",
		"agent: alfred",
		"profile: targeted",
	} {
		if !strings.Contains(prompt, want) {
			t.Errorf("prompt missing %q:\n%s", want, prompt)
		}
	}
	rest := strings.Join(args[2:], " ")
	if !strings.Contains(rest, "--skills "+digSkills) {
		t.Errorf("skills not preloaded: %q", rest)
	}
	if !strings.Contains(rest, "--resume manifest-feed-dig-epigenetic-clocks-58f7e088") {
		t.Errorf("no per-card session: %q", rest)
	}
	if strings.Contains(rest, "-t web,memory") {
		t.Errorf("dig must run with the default toolsets, not the read-only plan scope: %q", rest)
	}

	items := s.spirits.Feed.List(feed.Filter{Status: "inbox"}, time.Now())
	if len(items) != 2 {
		t.Fatalf("inbox should hold the original + the dig card, got %d", len(items))
	}
	var card *feed.Item
	for i := range items {
		if items[i].ID == "clocks-dig-abc123" {
			card = &items[i]
		}
	}
	if card == nil || card.Agent != "alfred" || card.Profile != "targeted" || card.Type != "artifact" {
		t.Fatalf("dig card did not serve: %+v", items)
	}
}

// Without the runner wired, an alfred card still answers the honest 422 —
// nothing silently re-routes to a paused spirit.
func TestFeedDigAlfredWithoutHermesIs422(t *testing.T) {
	s, _ := digFixture(t)
	w := postDig(s, "epigenetic-clocks-58f7e088")
	if w.Code != http.StatusUnprocessableEntity || !strings.Contains(w.Body.String(), "no on-demand ritual") {
		t.Fatalf("want 422 honest error, got %d: %s", w.Code, w.Body.String())
	}
}

// A second dig while one is in flight is refused with the same 409 shape the
// ritual path uses.
func TestFeedDigAlfredInFlightIs409(t *testing.T) {
	s, _ := digFixture(t)
	s.UseHermes(digStub(t, "#!/bin/sh\nexit 1\n"), "")
	s.hermes.mu.Lock()
	s.hermes.digging["epigenetic-clocks-58f7e088"] = true
	s.hermes.mu.Unlock()
	w := postDig(s, "epigenetic-clocks-58f7e088")
	if w.Code != http.StatusConflict || !strings.Contains(w.Body.String(), `"active":true`) {
		t.Fatalf("want 409 active, got %d: %s", w.Code, w.Body.String())
	}
}
