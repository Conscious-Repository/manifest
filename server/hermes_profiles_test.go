package server

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestHermesProfilesResolveUntruncatedModel(t *testing.T) {
	bin := filepath.Join(t.TempDir(), "hermes")
	script := `#!/bin/sh
if [ "$2" = list ]; then
  echo '◆default  deepseek-v4-flash-vision-e  running  —  —'
else
  printf 'Profile: default\nModel: deepseek-v4-flash-vision-exp (custom)\n'
fi
`
	if err := os.WriteFile(bin, []byte(script), 0700); err != nil {
		t.Fatal(err)
	}
	profiles, err := hermesProfiles(context.Background(), bin, os.Environ())
	if err != nil || len(profiles) != 1 || profiles[0].Model != "deepseek-v4-flash-vision-exp" {
		t.Fatal(profiles, err)
	}
	// Never fall back to the shortened table cell when full resolution fails.
	script = strings.Replace(script, "printf 'Profile: default\\nModel: deepseek-v4-flash-vision-exp (custom)\\n'", "exit 1", 1)
	if err := os.WriteFile(bin, []byte(script), 0700); err != nil {
		t.Fatal(err)
	}
	if _, err = hermesProfiles(context.Background(), bin, os.Environ()); err == nil {
		t.Fatal("accepted truncated model on failed show")
	}
}

func TestHermesProfileNameRule(t *testing.T) {
	ok := []string{"default", "scratch", "a-b", "x1", "recruiter2"}
	bad := []string{"", "Ab", "a b", "-a", "a/b", "../x", "a.b", strings.Repeat("a", 33), "scratch;rm"}
	for _, n := range ok {
		if !hermesProfileNameRe.MatchString(n) {
			t.Errorf("%q should be a valid profile name", n)
		}
	}
	for _, n := range bad {
		if hermesProfileNameRe.MatchString(n) {
			t.Errorf("%q should be refused", n)
		}
	}
}

func TestParseHermesProfileShow(t *testing.T) {
	text := `
Profile: scratch
Path:    /home/b/.hermes/profiles/scratch
Model:   claude-opus-5 (custom)
Gateway: stopped
Skills:  77
.env:    exists
SOUL.md: missing
Alias:   scratch → hermes -p scratch  (/home/b/.local/bin/scratch)
`
	p := parseHermesProfileShow(text)
	if p.Name != "scratch" || p.Path != "/home/b/.hermes/profiles/scratch" || p.Model != "claude-opus-5 (custom)" || p.Gateway != "stopped" {
		t.Fatalf("show parse wrong: %+v", p)
	}
	if p.Skills == nil || *p.Skills != 77 || !p.EnvFile || p.SoulFile {
		t.Fatalf("skills/env/soul wrong: %+v", p)
	}
	if p.Alias != "scratch" || p.AliasPath != "/home/b/.local/bin/scratch" || p.Target != "-p scratch" {
		t.Fatalf("alias wrong: %+v", p)
	}
	// the default profile prints no Alias line and a Model line; skills unknown
	q := parseHermesProfileShow("Profile: default\nPath: /home/b/.hermes\nGateway: running\n")
	if q.Alias != "" || q.Skills != nil || q.Model != "" || q.Target != "-p default" {
		t.Fatalf("default parse wrong: %+v", q)
	}
}

func TestParseHermesDescribeAndCreate(t *testing.T) {
	if parseHermesDescribe("(no description set for 'default')") != "" {
		t.Error("no-description sentinel should read as empty")
	}
	if parseHermesDescribe("  scouts domains for AION  \n") != "scouts domains for AION" {
		t.Error("description text should be trimmed")
	}
	path, wrapper := parseHermesCreateOutput(`
Profile 'scratch' created at /home/b/.hermes/profiles/scratch
77 bundled skills synced.
Wrapper created: /home/b/.local/bin/scratch

Next steps:
  scratch chat               Start chatting
`)
	if path != "/home/b/.hermes/profiles/scratch" || wrapper != "/home/b/.local/bin/scratch" {
		t.Errorf("create parse = %q / %q", path, wrapper)
	}
}

// The routes refuse non-slug names before any shell-out (server-side gate).
func TestProfileRoutesRefuseNonSlug(t *testing.T) {
	s := &Server{}
	mux := http.NewServeMux()
	mux.HandleFunc("POST /api/profiles", s.handleProfileCreate)
	mux.HandleFunc("GET /api/profiles/{name}", s.handleProfileShow)
	mux.HandleFunc("POST /api/profiles/{name}/describe", s.handleProfileDescribe)
	mux.HandleFunc("POST /api/profiles/{name}/export", s.handleProfileExport)
	cases := []struct{ method, url, body string }{
		{"POST", "/api/profiles", `{"name":"Bad Name"}`},
		{"POST", "/api/profiles", `{"name":"ok","cloneFrom":"../x"}`},
		{"GET", "/api/profiles/Bad", ""},
		{"POST", "/api/profiles/a.b/describe", `{"text":"x"}`},
		{"POST", "/api/profiles/a%20b/export", ""},
	}
	for _, c := range cases {
		req := httptest.NewRequest(c.method, c.url, strings.NewReader(c.body))
		rec := httptest.NewRecorder()
		mux.ServeHTTP(rec, req)
		if rec.Code != http.StatusBadRequest {
			t.Errorf("%s %s → %d, want 400", c.method, c.url, rec.Code)
		}
	}
	// an empty description is refused too (there is no clear op in the CLI)
	req := httptest.NewRequest("POST", "/api/profiles/ok/describe", strings.NewReader(`{"text":"   "}`))
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Errorf("empty describe → %d, want 400", rec.Code)
	}
}
