package consume

import (
	"encoding/json"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"sync"
	"time"

	"golang.org/x/net/publicsuffix"
)

// Site credentials — the session cookie that lets the reader fetch what the
// owner already pays for.
//
// ⚠ THIS IS THE HEAVIEST SECRET THIS APP STORES. An API key is scoped; a
// session cookie is the owner logged in. Whoever holds it can act as him on
// that site, not merely read. Six properties keep that contained, and each has
// a test:
//
//	never in the vault      — keyed by host HERE, and nothing is added to
//	                          renderSubLine's `known` table
//	never in a URL          — Subscribe refuses a tokenized feed URL, because
//	                          [url:: …] IS written to the vault, a synced repo
//	never logged            — redactURL fronts every error that carries a URL
//	never echoed            — rows carry a mask and field NAMES only
//	never cross-host        — a redirect off the credential's host drops it
//	revocable               — delete, and the next poll is anonymous again
//
// Keyed by registrable domain rather than by subscription because that is what
// a cookie actually is: one set on .substack.com covers every publication
// there, so pasting it once unlocks all of them and there is only ever one copy
// on disk. A publication on its own domain gets its own entry, exactly as the
// browser would.

// SiteCreds stores one cookie per registrable domain under
// <dataDir>/consume/sites/<host>.json, mode 0600 — the portals/store.go
// discipline, which is the closest existing keyed secret store.
type SiteCreds struct {
	dir string
	mu  sync.Mutex
}

// NewSiteCreds roots the store.
func NewSiteCreds(dataDir string) *SiteCreds {
	return &SiteCreds{dir: filepath.Join(dataDir, "consume", "sites")}
}

// siteFile is the on-disk shape. The cookie, when it was added, and whether the
// last signed-in poll still came back as a preview — which is how an expired
// session is detected without asking the owner to notice.
type siteFile struct {
	// Host is stored rather than recovered from the filename: safeName maps a
	// dot to a dash, so substack.com becomes substack-com on disk and reading
	// the host back off the filename would hand out an id that matches no
	// subscription.
	Host    string `json:"host"`
	Cookie  string `json:"cookie"`
	Added   string `json:"added,omitempty"`
	Expired bool   `json:"expired,omitempty"`
	SeenOK  string `json:"seenOk,omitempty"` // last poll that returned full content
}

var envUnsafe = regexp.MustCompile(`[^A-Z0-9]`)

// envKey builds MANIFEST_CONSUME_COOKIE_<HOST>, following portals/store.go.
func envKey(host string) string {
	return "MANIFEST_CONSUME_COOKIE_" + envUnsafe.ReplaceAllString(strings.ToUpper(host), "_")
}

// SiteKey is the credential key for a URL: its registrable domain.
//
// This is cookie semantics, not a heuristic. A cookie set on `.substack.com` is
// sent to every `*.substack.com` host, so `buildingoptimism.substack.com` and
// `noahpinion.substack.com` share one credential — and a publication on
// `noahpinion.blog` correctly gets its own, because the browser would not send
// the substack.com cookie there either.
func SiteKey(rawURL string) string {
	u, err := url.Parse(strings.TrimSpace(rawURL))
	if err != nil || u.Host == "" {
		return ""
	}
	host := strings.ToLower(u.Hostname())
	if host == "" {
		return ""
	}
	if etld1, err := publicsuffix.EffectiveTLDPlusOne(host); err == nil && etld1 != "" {
		return etld1
	}
	return host
}

func (s *SiteCreds) path(host string) string {
	return filepath.Join(s.dir, safeName(host)+".json")
}

func (s *SiteCreds) load(host string) siteFile {
	var f siteFile
	if b, err := os.ReadFile(s.path(host)); err == nil {
		_ = json.Unmarshal(b, &f)
	}
	return f
}

func (s *SiteCreds) save(host string, f siteFile) error {
	if err := os.MkdirAll(s.dir, 0o700); err != nil {
		return err
	}
	b, err := json.MarshalIndent(f, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(s.path(host), b, 0o600) // owner-only
}

// Cookie returns the effective cookie for a URL's site, env override on top.
// ⚠ Callers must never log or echo the result.
func (s *SiteCreds) Cookie(rawURL string) string {
	host := SiteKey(rawURL)
	if host == "" {
		return ""
	}
	if v := strings.TrimSpace(os.Getenv(envKey(host))); v != "" {
		return v
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	return strings.TrimSpace(s.load(host).Cookie)
}

// Set stores a cookie for a site. An empty value clears it.
func (s *SiteCreds) Set(host, cookie string) error {
	host = strings.ToLower(strings.TrimSpace(host))
	if host == "" {
		return errNoHost
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if strings.TrimSpace(cookie) == "" {
		_ = os.Remove(s.path(host))
		return nil
	}
	f := s.load(host)
	f.Host = host
	f.Cookie = strings.TrimSpace(cookie)
	f.Added = time.Now().UTC().Format("2006-01-02")
	f.Expired = false // a fresh paste is innocent until a poll proves otherwise
	return s.save(host, f)
}

// Clear removes a site's credential.
func (s *SiteCreds) Clear(host string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	_ = os.Remove(s.path(strings.ToLower(strings.TrimSpace(host))))
}

// MarkResult records what a signed-in poll actually got back.
//
// ⚠ Only called after a SUCCESSFUL poll. A network failure is not an expired
// session, and telling the owner to re-authenticate because a feed 500'd would
// be worse than saying nothing.
func (s *SiteCreds) MarkResult(host string, stillPreview bool, now time.Time) {
	host = strings.ToLower(strings.TrimSpace(host))
	if host == "" {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	f := s.load(host)
	if f.Cookie == "" {
		return // no credential here; nothing to say about it
	}
	if f.Expired == stillPreview && (stillPreview || f.SeenOK != "") {
		return // nothing changed — don't rewrite the file on every poll
	}
	f.Expired = stillPreview
	if !stillPreview {
		f.SeenOK = now.UTC().Format(time.RFC3339)
	}
	_ = s.save(host, f)
}

// SiteStatus is one row of the credential panel. It carries a MASK and never a
// value — the portalRowView discipline.
type SiteStatus struct {
	Host    string `json:"host"`
	Masked  string `json:"masked"`
	Added   string `json:"added,omitempty"`
	Expired bool   `json:"expired"`
	FromEnv bool   `json:"fromEnv,omitempty"`
	Feeds   int    `json:"feeds"` // how many subscriptions this unlocks
}

// Sites lists the stored credentials, newest host order stable.
func (s *SiteCreds) Sites() []SiteStatus {
	s.mu.Lock()
	defer s.mu.Unlock()
	entries, err := os.ReadDir(s.dir)
	if err != nil {
		return nil
	}
	out := []SiteStatus{}
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".json") {
			continue
		}
		f := s.load(strings.TrimSuffix(e.Name(), ".json"))
		if f.Cookie == "" {
			continue
		}
		host := f.Host
		if host == "" {
			host = strings.TrimSuffix(e.Name(), ".json") // pre-Host files
		}
		st := SiteStatus{Host: host, Masked: maskTail(f.Cookie), Added: f.Added, Expired: f.Expired}
		if strings.TrimSpace(os.Getenv(envKey(host))) != "" {
			st.FromEnv = true
		}
		out = append(out, st)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Host < out[j].Host })
	return out
}

// Expired reports whether a site's stored session has stopped working.
func (s *SiteCreds) Expired(rawURL string) bool {
	host := SiteKey(rawURL)
	if host == "" {
		return false
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	f := s.load(host)
	return f.Cookie != "" && f.Expired
}

// maskTail is the house mask: last four, never more.
func maskTail(s string) string {
	s = strings.TrimSpace(s)
	if len(s) <= 4 {
		if s == "" {
			return ""
		}
		return "····" + s
	}
	return "····" + s[len(s)-4:]
}
