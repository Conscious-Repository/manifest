package construction

// FixtureAdapter serves visibly synthetic sources (testdata/roof-wall/
// sources.json + source-pages.txt) so the research protocol can be proved
// hermetically: retained pages, exact and normalised quotes, a contradicting
// source, a prompt-injection source, a 404, a rate limit, a timeout, a
// paywall, a scanned PDF and fabricated citations. Every source it returns is
// marked fictional. It is wired only by tests or an explicit fixture option;
// production has no fixture adapter.

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

type fixtureFile struct {
	SchemaVersion int             `json:"schemaVersion"`
	Notice        string          `json:"notice"`
	Sources       []fixtureSource `json:"sources"`
}

type fixtureSource struct {
	Key          string    `json:"key"`
	Locator      string    `json:"locator"`
	Title        string    `json:"title"`
	Publisher    string    `json:"publisher"`
	Class        string    `json:"class"`
	Access       string    `json:"access"`
	Jurisdiction string    `json:"jurisdiction,omitempty"`
	Edition      string    `json:"edition,omitempty"`
	PublishedAt  string    `json:"publishedAt,omitempty"`
	URL          string    `json:"url,omitempty"`
	Outcome      string    `json:"outcome"`
	Message      string    `json:"message,omitempty"`
	Pages        string    `json:"pages,omitempty"`
	Mime         string    `json:"mime,omitempty"`
	Content      string    `json:"content,omitempty"`
	Passages     []Passage `json:"passages"`
}

// FixtureAdapter is the synthetic source adapter.
type FixtureAdapter struct {
	Notice  string
	sources []fixtureSource
	pages   map[string][]string
	// Fail is consulted on each Acquire call (1-based) for failure
	// injection; Delay holds each call (cancellation tests).
	Fail  func(call int) error
	Delay time.Duration
	mu    sync.Mutex
	calls int
}

// LoadFixtureAdapter reads the fixture directory and refuses anything that
// is not visibly synthetic.
func LoadFixtureAdapter(dir string) (*FixtureAdapter, error) {
	raw, err := os.ReadFile(filepath.Join(dir, "sources.json"))
	if err != nil {
		return nil, err
	}
	var f fixtureFile
	d := json.NewDecoder(strings.NewReader(string(raw)))
	d.DisallowUnknownFields()
	if err := d.Decode(&f); err != nil {
		return nil, fmt.Errorf("fixture sources: %w", err)
	}
	if f.SchemaVersion != 1 || !strings.Contains(strings.ToUpper(f.Notice), "SYNTHETIC") {
		return nil, fmt.Errorf("fixture sources must declare schemaVersion 1 and a SYNTHETIC notice")
	}
	pages, err := loadFixturePages(filepath.Join(dir, "source-pages.txt"))
	if err != nil {
		return nil, err
	}
	for _, s := range f.Sources {
		if !strings.HasPrefix(s.Locator, "fixture://") || !strings.Contains(s.Title, "FIXTURE") ||
			!(strings.Contains(strings.ToLower(s.Publisher), "fictional") || strings.Contains(strings.ToLower(s.Publisher), "synthetic")) {
			return nil, fmt.Errorf("fixture source %q is not visibly synthetic", s.Key)
		}
		if s.Pages != "" && pages[s.Pages] == nil {
			return nil, fmt.Errorf("fixture source %q names missing pages %q", s.Key, s.Pages)
		}
		if _, ok := acquireOutcomes[s.Outcome]; !ok {
			return nil, fmt.Errorf("fixture source %q has unknown outcome %q", s.Key, s.Outcome)
		}
	}
	return &FixtureAdapter{Notice: f.Notice, sources: f.Sources, pages: pages}, nil
}

// loadFixturePages reads "=== key ===" documents whose pages are separated
// by "--- page ---" lines; '#' lines before the first document are comments.
func loadFixturePages(path string) (map[string][]string, error) {
	fh, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer fh.Close()
	out := map[string][]string{}
	key := ""
	var cur []string
	var page strings.Builder
	flushPage := func() {
		if key != "" {
			cur = append(cur, strings.Trim(page.String(), "\n"))
		}
		page.Reset()
	}
	flushDoc := func() {
		if key != "" {
			flushPage()
			out[key] = cur
		}
		cur = nil
	}
	sc := bufio.NewScanner(fh)
	sc.Buffer(make([]byte, 0, 64<<10), 1<<20)
	for sc.Scan() {
		line := sc.Text()
		switch {
		case strings.HasPrefix(line, "=== ") && strings.HasSuffix(line, " ==="):
			flushDoc()
			key = strings.TrimSuffix(strings.TrimPrefix(line, "=== "), " ===")
		case line == "--- page ---":
			flushPage()
		case key == "" && (strings.HasPrefix(line, "#") || strings.TrimSpace(line) == ""):
		default:
			page.WriteString(line + "\n")
		}
	}
	flushDoc()
	return out, sc.Err()
}

func (f *FixtureAdapter) Name() string { return "synthetic-fixture" }
func (f *FixtureAdapter) Kind() string { return "fixture" }

func (f *FixtureAdapter) Acquire(ctx context.Context, req AcquireRequest) ([]Acquired, error) {
	f.mu.Lock()
	f.calls++
	call := f.calls
	f.mu.Unlock()
	if f.Delay > 0 {
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-time.After(f.Delay):
		}
	}
	if f.Fail != nil {
		if err := f.Fail(call); err != nil {
			return nil, err
		}
	}
	var out []Acquired
	for _, s := range f.sources {
		if len(out) >= req.Budget {
			break
		}
		a := Acquired{Locator: s.Locator, Outcome: s.Outcome, Message: s.Message, Mime: s.Mime, Passages: s.Passages,
			Source: Source{Title: s.Title, Publisher: s.Publisher, Class: s.Class, Fictional: true, URL: s.URL, Access: s.Access,
				Jurisdiction: s.Jurisdiction, Edition: s.Edition, PublishedAt: s.PublishedAt, FinalURL: s.URL}}
		switch {
		case s.Pages != "":
			a.Content = []byte(strings.Join(f.pages[s.Pages], "\f"))
			if a.Mime == "" {
				a.Mime = "text/plain; charset=utf-8"
			}
		case s.Content != "":
			a.Content = []byte(s.Content)
		}
		if s.Outcome != "retained" && s.Outcome != "extraction-unavailable" {
			a.Content = nil
			if s.Access == "restricted" {
				a.Source.OmissionReason = "access restricted: not retrieved, not bypassed; metadata only"
			}
		}
		out = append(out, a)
	}
	return out, nil
}
