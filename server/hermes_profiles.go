package server

// Hermes PROFILES — agents plan Phase 5 (§4.2 Agents card, §4.3 agent page,
// §8 contract).
//
// A profile is an isolated Hermes instance (`~/.hermes/profiles/<name>`: its
// own config, keys, SOUL.md, skills, sessions, cron) reached with
// `hermes -p <name>` or the wrapper alias `<name>`. Manifest NEVER
// reimplements profile logic: every endpoint here shells out to
// `hermes profile …` and projects what it printed — list, show, describe,
// create, export. Nothing is stored server-side; every request re-asks the
// CLI. The name rule is the CLI's (lowercase alphanumerics, hyphens allowed;
// mixed case is lowercased by Hermes, so we refuse it instead of guessing).

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"
)

// hermesProfileNameRe is the slug rule enforced client- and server-side.
var hermesProfileNameRe = regexp.MustCompile(`^[a-z0-9][a-z0-9-]{0,31}$`)

// hermesProfileShow is the projection of `hermes profile show <name>` plus
// `hermes profile describe <name>` (the description) — path, model, gateway,
// skills count, SOUL.md / .env presence, the alias line.
type hermesProfileShow struct {
	Name        string `json:"name"`
	Path        string `json:"path"`
	Model       string `json:"model"`
	Gateway     string `json:"gateway"`
	Skills      *int   `json:"skills"` // nil = the CLI did not say
	EnvFile     bool   `json:"envFile"`
	SoulFile    bool   `json:"soulFile"`
	Alias       string `json:"alias"`       // wrapper name ("" = no wrapper)
	AliasPath   string `json:"aliasPath"`   // wrapper script path
	Target      string `json:"target"`      // "-p <name>" — how the runner reaches it
	Description string `json:"description"` // "" = none set
	Raw         string `json:"raw"`         // the CLI's own text, for the tooltip
}

// hermesProfileCmd runs `hermes profile <args…>` bounded, combined output.
func hermesProfileCmd(ctx context.Context, bin string, env []string, timeout time.Duration, args ...string) (string, error) {
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, bin, append([]string{"profile"}, args...)...)
	cmd.Env = env
	cmd.Stdin = nil // never interactive: the CLI must not wait on a prompt
	var out bytes.Buffer
	cmd.Stdout = &out
	cmd.Stderr = &out
	err := cmd.Run()
	text := strings.TrimSpace(out.String())
	if err != nil {
		if ctx.Err() == context.DeadlineExceeded {
			return text, fmt.Errorf("hermes profile %s timed out after %s", args[0], timeout)
		}
		return text, fmt.Errorf("%v: %s", err, snipRunes(text, 400))
	}
	return text, nil
}

// hermesProfileCommand is the verbatim line echoed to the UI (basename only).
func hermesProfileCommand(bin string, args ...string) string {
	return filepath.Base(bin) + " profile " + strings.Join(args, " ")
}

// parseHermesProfileShow reads the `Key:   value` lines of `hermes profile
// show`. Tolerant: unknown keys are ignored, a missing Model line = unset.
func parseHermesProfileShow(text string) hermesProfileShow {
	p := hermesProfileShow{Raw: strings.TrimSpace(text)}
	for _, line := range strings.Split(text, "\n") {
		k, v, ok := strings.Cut(line, ":")
		if !ok {
			continue
		}
		k, v = strings.TrimSpace(k), strings.TrimSpace(v)
		switch k {
		case "Profile":
			p.Name = v
		case "Path":
			p.Path = v
		case "Model":
			p.Model = v
		case "Gateway":
			p.Gateway = v
		case "Skills":
			if n, err := strconv.Atoi(strings.Fields(v + " x")[0]); err == nil {
				p.Skills = &n
			}
		case ".env":
			p.EnvFile = strings.HasPrefix(v, "exists")
		case "SOUL.md":
			p.SoulFile = strings.HasPrefix(v, "exists")
		case "Alias":
			// "scratch → hermes -p scratch  (/home/b/.local/bin/scratch)"
			name, rest, _ := strings.Cut(v, "→")
			p.Alias = strings.TrimSpace(name)
			if i := strings.LastIndex(rest, "("); i >= 0 {
				p.AliasPath = strings.TrimSuffix(strings.TrimSpace(rest[i+1:]), ")")
			}
		}
	}
	if p.Name != "" {
		p.Target = "-p " + p.Name
	}
	return p
}

// parseHermesDescribe: `hermes profile describe <name>` prints the text, or
// "(no description set for '<name>')".
func parseHermesDescribe(text string) string {
	t := strings.TrimSpace(text)
	if strings.HasPrefix(t, "(no description set") {
		return ""
	}
	return t
}

// parseHermesCreateOutput pulls the wrapper path and the profile path out of
// the CLI's create narration ("Profile 'x' created at …", "Wrapper created:
// …"). Both optional — --no-alias prints no wrapper line.
func parseHermesCreateOutput(text string) (path, wrapper string) {
	for _, line := range strings.Split(text, "\n") {
		t := strings.TrimSpace(line)
		if strings.HasPrefix(t, "Wrapper created:") {
			wrapper = strings.TrimSpace(strings.TrimPrefix(t, "Wrapper created:"))
		} else if strings.HasPrefix(t, "Profile '") && strings.Contains(t, " created at ") {
			_, after, _ := strings.Cut(t, " created at ")
			path = strings.TrimSpace(after)
		}
	}
	return
}

// hermesProfileShowFull = show + describe for one profile.
func (s *Server) hermesProfileShowFull(ctx context.Context, name string) (hermesProfileShow, error) {
	bin, env := s.hermesBin(), s.hermesEnv()
	text, err := hermesProfileCmd(ctx, bin, env, 10*time.Second, "show", name)
	if err != nil {
		return hermesProfileShow{}, err
	}
	p := parseHermesProfileShow(text)
	if p.Name == "" {
		p.Name = name
		p.Target = "-p " + name
	}
	if d, derr := hermesProfileCmd(ctx, bin, env, 10*time.Second, "describe", name); derr == nil {
		p.Description = parseHermesDescribe(d)
	}
	return p, nil
}

// profileNameOr400 validates the path value; false = already answered.
func profileNameOr400(w http.ResponseWriter, name string) bool {
	if !hermesProfileNameRe.MatchString(name) {
		http.Error(w, "profile name must be lowercase letters, digits or hyphens (1–32 chars)", http.StatusBadRequest)
		return false
	}
	return true
}

// ---- handlers ----

// GET /api/profiles → exactly what `hermes profile list` reports.
func (s *Server) handleProfilesList(w http.ResponseWriter, r *http.Request) {
	profiles, err := hermesProfiles(r.Context(), s.hermesBin(), s.hermesEnv())
	out := map[string]any{"data": profiles, "bin": s.hermesBin(), "command": hermesProfileCommand(s.hermesBin(), "list")}
	if err != nil {
		out["data"] = []hermesProfile{}
		out["degraded"] = err.Error()
	}
	writeJSON(w, out)
}

// GET /api/profiles/{name} → the show projection + its cron jobs + recent
// fires (read from the profile's own state root, exactly like the default).
func (s *Server) handleProfileShow(w http.ResponseWriter, r *http.Request) {
	name := r.PathValue("name")
	if !profileNameOr400(w, name) {
		return
	}
	p, err := s.hermesProfileShowFull(r.Context(), name)
	if err != nil {
		w.WriteHeader(http.StatusNotFound)
		writeJSON(w, map[string]any{"ok": false, "error": err.Error(), "command": hermesProfileCommand(s.hermesBin(), "show", name)})
		return
	}
	out := map[string]any{"ok": true, "profile": p, "isDefault": p.Path != "" && filepath.Clean(p.Path) == filepath.Clean(s.hermesHome())}
	// cron jobs + fires from the profile's own tree (files first, D4)
	jobs := []hermesJob{}
	notes := []string{}
	fires := []hermesFire{}
	// a profile that has never scheduled anything has no cron/ at all — that
	// is "no jobs", not a degraded read
	if _, cerr := os.Stat(filepath.Join(p.Path, "cron")); p.Path != "" && cerr == nil {
		if b, ferr := os.ReadFile(filepath.Join(p.Path, "cron", "jobs.json")); ferr == nil {
			if js, perr := parseHermesJobsJSON(b); perr == nil {
				jobs = js
			} else {
				notes = append(notes, "jobs.json: "+perr.Error())
			}
		} else if !os.IsNotExist(ferr) {
			notes = append(notes, "jobs.json: "+ferr.Error())
		}
		byID := map[string]hermesJob{}
		for _, j := range jobs {
			byID[j.ID] = j
		}
		var fnotes []string
		fires, fnotes = s.hermesFiresIn(p.Path, hermesSince("7d"), byID, out["isDefault"] == true)
		notes = append(notes, fnotes...)
	}
	if len(fires) > 20 {
		fires = fires[:20]
	}
	out["jobs"] = jobs
	out["fires"] = fires
	out["degraded"] = notes
	writeJSON(w, out)
}

// GET /api/profiles/{name}/soul → SOUL.md from the profile's path (read-only;
// Hermes owns writes — the raw drawer opens it without a Save).
func (s *Server) handleProfileSoul(w http.ResponseWriter, r *http.Request) {
	name := r.PathValue("name")
	if !profileNameOr400(w, name) {
		return
	}
	p, err := s.hermesProfileShowFull(r.Context(), name)
	if err != nil || p.Path == "" {
		http.Error(w, "profile not found", http.StatusNotFound)
		return
	}
	path := filepath.Join(p.Path, "SOUL.md")
	b, rerr := os.ReadFile(path)
	if rerr != nil {
		writeJSON(w, map[string]any{"path": path, "exists": false, "content": ""})
		return
	}
	if len(b) > 512*1024 {
		b = append(b[:512*1024], []byte("\n… (truncated)")...)
	}
	writeJSON(w, map[string]any{"path": path, "exists": true, "content": string(b)})
}

// POST /api/profiles {name, cloneFrom?, description?} → `hermes profile create`.
func (s *Server) handleProfileCreate(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Name        string `json:"name"`
		CloneFrom   string `json:"cloneFrom"`
		Description string `json:"description"`
	}
	if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
		http.Error(w, "bad json", http.StatusBadRequest)
		return
	}
	in.Name = strings.TrimSpace(in.Name)
	in.CloneFrom = strings.TrimSpace(in.CloneFrom)
	if !profileNameOr400(w, in.Name) {
		return
	}
	if in.CloneFrom != "" && !hermesProfileNameRe.MatchString(in.CloneFrom) {
		http.Error(w, "clone-from must name an existing profile", http.StatusBadRequest)
		return
	}
	args := []string{"create", in.Name}
	if in.CloneFrom != "" {
		args = append(args, "--clone-from", in.CloneFrom)
	}
	if d := strings.TrimSpace(in.Description); d != "" {
		args = append(args, "--description", d)
	}
	bin := s.hermesBin()
	command := hermesProfileCommand(bin, args...)
	// skill sync on create takes a few seconds; a clone copies a tree
	text, err := hermesProfileCmd(r.Context(), bin, s.hermesEnv(), 120*time.Second, args...)
	if err != nil {
		w.WriteHeader(http.StatusBadGateway)
		writeJSON(w, map[string]any{"ok": false, "command": command, "output": snipRunes(text, 2000), "error": err.Error()})
		return
	}
	path, wrapper := parseHermesCreateOutput(text)
	out := map[string]any{
		"ok": true, "name": in.Name, "command": command, "output": snipRunes(text, 2000),
		"path": path, "wrapper": wrapper, "target": "-p " + in.Name,
	}
	if wrapper != "" {
		out["alias"] = in.Name + " chat"
	}
	writeJSON(w, out)
}

// POST /api/profiles/{name}/describe {text} → `hermes profile describe <name> --text …`.
func (s *Server) handleProfileDescribe(w http.ResponseWriter, r *http.Request) {
	name := r.PathValue("name")
	if !profileNameOr400(w, name) {
		return
	}
	var in struct {
		Text string `json:"text"`
	}
	if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
		http.Error(w, "bad json", http.StatusBadRequest)
		return
	}
	text := strings.TrimSpace(in.Text)
	if text == "" {
		http.Error(w, "description text is empty", http.StatusBadRequest)
		return
	}
	bin := s.hermesBin()
	args := []string{"describe", name, "--text", text}
	command := hermesProfileCommand(bin, "describe", name, "--text", strconv.Quote(text))
	out, err := hermesProfileCmd(r.Context(), bin, s.hermesEnv(), 20*time.Second, args...)
	if err != nil {
		w.WriteHeader(http.StatusBadGateway)
		writeJSON(w, map[string]any{"ok": false, "command": command, "output": snipRunes(out, 1000), "error": err.Error()})
		return
	}
	// re-read: the description is whatever Hermes now reports, not what we sent
	desc := text
	if d, derr := hermesProfileCmd(r.Context(), bin, s.hermesEnv(), 10*time.Second, "describe", name); derr == nil {
		desc = parseHermesDescribe(d)
	}
	writeJSON(w, map[string]any{"ok": true, "command": command, "description": desc, "output": snipRunes(out, 1000)})
}

// POST /api/profiles/{name}/export → `hermes profile export <name> -o
// <dataDir>/profile-exports/<name>-<ts>.tar.gz`; returns the archive path.
func (s *Server) handleProfileExport(w http.ResponseWriter, r *http.Request) {
	name := r.PathValue("name")
	if !profileNameOr400(w, name) {
		return
	}
	dir := filepath.Join(os.TempDir(), "manifest-profile-exports")
	if s.hosts != nil && strings.TrimSpace(s.hosts.Data.DataDir) != "" {
		dir = filepath.Join(s.hosts.Data.DataDir, "profile-exports")
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		http.Error(w, "export dir: "+err.Error(), http.StatusInternalServerError)
		return
	}
	dest := filepath.Join(dir, name+"-"+time.Now().Format("20060102-150405")+".tar.gz")
	bin := s.hermesBin()
	args := []string{"export", name, "-o", dest}
	command := hermesProfileCommand(bin, args...)
	out, err := hermesProfileCmd(r.Context(), bin, s.hermesEnv(), 120*time.Second, args...)
	if err != nil {
		w.WriteHeader(http.StatusBadGateway)
		writeJSON(w, map[string]any{"ok": false, "command": command, "output": snipRunes(out, 1000), "error": err.Error()})
		return
	}
	res := map[string]any{"ok": true, "command": command, "path": dest, "output": snipRunes(out, 1000)}
	if st, serr := os.Stat(dest); serr == nil {
		res["bytes"] = st.Size()
	} else {
		// trust the CLI's own line over our guess if it wrote elsewhere
		res["bytes"] = 0
		res["warning"] = "archive not found at the requested path — see output"
	}
	writeJSON(w, res)
}
