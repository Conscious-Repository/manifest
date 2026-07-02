package spirits

// The RITUALS board + in-app markdown editing (plans/spirits-console-upgrade.md).
// The dashboard is a convenient window onto the excalibur markdown: it reads
// every ritual for the board and lets the user edit the harness's own config
// files (spirit identity/cornerstone, ritual files, chargebook) through a
// strict allow-list. It never writes the knowledge vault's notes, and never
// touches engine/, vessel/, artifacts/, or memories/ (memories belong to the
// spirits). Cron parsing + lint here mirror the engine so the board's "invalid"
// verdict matches what the engine actually skips.

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/robfig/cron/v3"

	"manifest/mdfm"
)

// RitualRow is one row of the RITUALS board.
type RitualRow struct {
	Spirit         string  `json:"spirit"`
	Ritual         string  `json:"ritual"`
	Path           string  `json:"path"` // repo-relative ritual file, for the editor
	Cadence        string  `json:"cadence"`
	CadenceHuman   string  `json:"cadenceHuman"`
	NextFire       string  `json:"nextFire"` // RFC3339; "" for on-demand/invalid
	CeilingUSD     float64 `json:"ceilingUsd"`
	CeilingDefault bool    `json:"ceilingDefault"` // ceiling came from the chargebook default
	LastOutcome    string  `json:"lastOutcome"`    // "" = never run
	LastRunID      string  `json:"lastRunId"`
	Valid          bool    `json:"valid"`
	Error          string  `json:"error"`
}

// Rituals builds the board: every ritual across all spirits, joined with the
// latest matching run report and the chargebook default ceiling. next-fire and
// validity are computed here (fresh, engine-independent); if the engine has
// recorded a ritual invalid in ritual-status.json, that reason wins.
func (s *Store) Rituals(now time.Time) []RitualRow {
	def := s.chargebookDefault()
	latest := map[string]RunSummary{} // "spirit/ritual" → newest run
	for _, r := range s.Runs() {      // Runs() is newest-first
		k := r.Spirit + "/" + r.Ritual
		if _, ok := latest[k]; !ok {
			latest[k] = r
		}
	}
	engErr := s.engineRitualErrors()

	var rows []RitualRow
	spDir := filepath.Join(s.root, "spirits")
	entries, _ := os.ReadDir(spDir)
	for _, sp := range entries {
		if !sp.IsDir() {
			continue
		}
		rdir := filepath.Join(spDir, sp.Name(), "rituals")
		rits, _ := os.ReadDir(rdir)
		for _, rf := range rits {
			if rf.IsDir() || !strings.HasSuffix(rf.Name(), ".md") {
				continue
			}
			rows = append(rows, s.ritualRow(sp.Name(), rdir, rf.Name(), def, now, latest, engErr))
		}
	}
	sort.Slice(rows, func(i, j int) bool {
		if rows[i].Spirit != rows[j].Spirit {
			return rows[i].Spirit < rows[j].Spirit
		}
		return rows[i].Ritual < rows[j].Ritual
	})
	return rows
}

func (s *Store) ritualRow(spirit, rdir, file string, def float64, now time.Time, latest map[string]RunSummary, engErr map[string]string) RitualRow {
	stem := strings.TrimSuffix(file, ".md")
	rel := filepath.ToSlash(filepath.Join("spirits", spirit, "rituals", file))
	row := RitualRow{Spirit: spirit, Ritual: stem, Path: rel, Valid: true, CeilingUSD: def, CeilingDefault: true}

	b, err := os.ReadFile(filepath.Join(rdir, file))
	if err != nil {
		row.Valid, row.Error, row.CadenceHuman = false, "unreadable file", "—"
		return row
	}
	fm, _ := mdfm.Split(string(b))
	if n := strings.TrimSpace(fm["ritual"]); n != "" {
		row.Ritual = n
	}
	row.Cadence = strings.TrimSpace(fm["cadence"])
	if v := strings.TrimSpace(fm["charge_usd"]); v != "" {
		if f, e := strconv.ParseFloat(v, 64); e == nil {
			row.CeilingUSD, row.CeilingDefault = f, false
		}
	}

	if errs, _ := lintRitualFM(fm); len(errs) > 0 {
		row.Valid, row.Error = false, errs[0]
	}
	if e, ok := engErr[spirit+"/"+row.Ritual]; ok && e != "" { // engine is authoritative on what it skips
		row.Valid, row.Error = false, e
	}

	switch {
	case row.Cadence == "":
		row.CadenceHuman = "on-demand"
	case row.Valid:
		row.CadenceHuman = humanCadence(row.Cadence)
		if sched, e := cron.ParseStandard(row.Cadence); e == nil {
			row.NextFire = sched.Next(now).Format(time.RFC3339)
		}
	default:
		row.CadenceHuman = row.Cadence // invalid: show the raw string
	}

	if lr, ok := latest[spirit+"/"+row.Ritual]; ok {
		row.LastOutcome, row.LastRunID = lr.Outcome, lr.ID
	}
	return row
}

// --- file editing (allow-listed) ---

// LintResult is returned by WriteFile: OK=false with Errors blocks the save;
// Warnings are advisory and never block.
type LintResult struct {
	OK       bool     `json:"ok"`
	Errors   []string `json:"errors,omitempty"`
	Warnings []string `json:"warnings,omitempty"`
}

// ReadFile returns an allow-listed harness file's contents. allowed=false means
// the path is off the editor allow-list (caller → 404).
func (s *Store) ReadFile(rel string) (content string, allowed bool, err error) {
	clean, ok := allowedEditPath(rel)
	if !ok {
		return "", false, nil
	}
	b, err := os.ReadFile(filepath.Join(s.root, filepath.FromSlash(clean)))
	if err != nil {
		return "", true, err
	}
	return string(b), true, nil
}

// WriteFile lints then writes an allow-listed file. allowed=false → 404. Hard
// lint errors block the write (LintResult.OK=false); warnings are returned but
// the write proceeds.
func (s *Store) WriteFile(rel, content string) (res LintResult, allowed bool, err error) {
	clean, ok := allowedEditPath(rel)
	if !ok {
		return LintResult{}, false, nil
	}
	errs, warns := s.lintFile(clean, content)
	if len(errs) > 0 {
		return LintResult{OK: false, Errors: errs, Warnings: warns}, true, nil
	}
	abs := filepath.Join(s.root, filepath.FromSlash(clean))
	if err := os.WriteFile(abs, []byte(content), 0o644); err != nil {
		return LintResult{}, true, err
	}
	return LintResult{OK: true, Warnings: warns}, true, nil
}

// --- scaffolding (quick create) ---

const ritualTemplate = `---
ritual: %s
charge_usd: %.2f
max_steps: 12
---
# %s

On-demand ritual — no ` + "`cadence`" + ` is set, so it only runs when you
trigger it (console "Run now" / "Ask a scout", or ` + "`excalibur run`" + `).
Add ` + "`cadence: 0 7 * * *`" + ` (a 5-field cron) to the frontmatter to
schedule it.

TODO: describe what this ritual should do.
`

// ScaffoldRitual creates spirits/<spirit>/rituals/<name>.md from a template
// (on-demand by default) and returns its repo-relative path.
func (s *Store) ScaffoldRitual(spirit, name string) (string, error) {
	if !validSlug(spirit) || !validSlug(name) {
		return "", fmt.Errorf("names must be lowercase letters, digits, - or _")
	}
	sdir := filepath.Join(s.root, "spirits", spirit)
	if fi, err := os.Stat(sdir); err != nil || !fi.IsDir() {
		return "", fmt.Errorf("spirit %q does not exist", spirit)
	}
	rdir := filepath.Join(sdir, "rituals")
	if err := os.MkdirAll(rdir, 0o755); err != nil {
		return "", err
	}
	abs := filepath.Join(rdir, name+".md")
	if _, err := os.Stat(abs); err == nil {
		return "", fmt.Errorf("ritual %q already exists", name)
	}
	body := fmt.Sprintf(ritualTemplate, name, s.chargebookDefault(), name)
	if err := os.WriteFile(abs, []byte(body), 0o644); err != nil {
		return "", err
	}
	return filepath.ToSlash(filepath.Join("spirits", spirit, "rituals", name+".md")), nil
}

const identityTemplate = `---
name: %s
available_spellbooks: []
---
# %s

TODO: describe what this spirit is and what it watches.
`

const cornerstoneTemplate = `---
portal:: claude-sub
writable: [artifacts/runs]
available_spellbooks: []
---
# Cornerstone — how %s behaves

TODO: describe behavior. This spirit fails closed: with no spellbooks and only
artifacts/runs writable, it can run but produce nothing until you deliberately
widen ` + "`available_spellbooks`" + ` and ` + "`writable`" + ` above. The
warden's next audit reviews any widening.
`

// ScaffoldSpirit creates the standard tree for a new spirit with fail-closed
// defaults (claude-sub portal, only artifacts/runs writable, no spellbooks).
func (s *Store) ScaffoldSpirit(name string) error {
	if !validSlug(name) {
		return fmt.Errorf("name must be lowercase letters, digits, - or _")
	}
	sdir := filepath.Join(s.root, "spirits", name)
	if _, err := os.Stat(sdir); err == nil {
		return fmt.Errorf("spirit %q already exists", name)
	}
	for _, d := range []string{"rituals", "memories/window", "memories/archive"} {
		if err := os.MkdirAll(filepath.Join(sdir, d), 0o755); err != nil {
			return err
		}
	}
	files := map[string]string{
		"identity.md":               fmt.Sprintf(identityTemplate, name, name),
		"cornerstone.md":            fmt.Sprintf(cornerstoneTemplate, name),
		"memories/long-term.md":     fmt.Sprintf("# long-term memory — %s\n", name),
		"memories/window/.gitkeep":  "",
		"memories/archive/.gitkeep": "",
	}
	for rel, content := range files {
		if err := os.WriteFile(filepath.Join(sdir, filepath.FromSlash(rel)), []byte(content), 0o644); err != nil {
			return err
		}
	}
	return nil
}

// --- lint (mirrors the engine's per-ritual validity) ---

func (s *Store) lintFile(rel, content string) (errs, warns []string) {
	fm, _ := mdfm.Split(content)
	switch {
	case isRitualPath(rel):
		return lintRitualFM(fm)
	case strings.HasSuffix(rel, "/cornerstone.md"):
		return s.lintCornerstoneFM(rel, fm)
	case rel == "chargebook.md":
		return lintChargebookFM(fm), nil
	}
	return nil, nil // identity.md: nothing hard to lint
}

func lintRitualFM(fm map[string]string) (errs, warns []string) {
	if v := strings.TrimSpace(fm["cadence"]); v != "" {
		if _, err := cron.ParseStandard(v); err != nil {
			errs = append(errs, fmt.Sprintf("cadence %q is not a valid 5-field cron: %v", v, err))
		}
	}
	if v := strings.TrimSpace(fm["charge_usd"]); v != "" {
		if _, err := strconv.ParseFloat(v, 64); err != nil {
			errs = append(errs, fmt.Sprintf("charge_usd %q is not a number", v))
		}
	}
	if v := strings.TrimSpace(fm["max_steps"]); v != "" {
		if _, err := strconv.Atoi(v); err != nil {
			errs = append(errs, fmt.Sprintf("max_steps %q is not an integer", v))
		}
	}
	return errs, warns
}

func (s *Store) lintCornerstoneFM(rel string, fm map[string]string) (errs, warns []string) {
	spiritName := ""
	if p := strings.Split(rel, "/"); len(p) >= 2 {
		spiritName = p[1]
	}
	portal := strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(fm["portal"]), ":"))
	if portal == "" {
		errs = append(errs, "cornerstone must name a portal (portal:: <name>)")
	} else if !s.portalExists(portal) {
		errs = append(errs, fmt.Sprintf("portal %q has no def in grimoire/portals/", portal))
	}
	writable := mdfm.List(fm["writable"])
	if len(writable) == 0 {
		warns = append(warns, "writable is empty — this spirit can write nothing (fails closed)")
	}
	for _, wdir := range writable {
		c := filepath.ToSlash(filepath.Clean(wdir))
		if filepath.IsAbs(wdir) || c == ".." || strings.HasPrefix(c, "../") {
			errs = append(errs, fmt.Sprintf("writable %q must be a relative path inside the harness (no ..)", wdir))
			continue
		}
		seg := strings.SplitN(c, "/", 2)[0]
		ownMem := c == "spirits/"+spiritName+"/memories" || strings.HasPrefix(c, "spirits/"+spiritName+"/memories/")
		if seg != "artifacts" && seg != "questbook" && !ownMem {
			warns = append(warns, fmt.Sprintf("writable %q is outside artifacts/ and questbook/ — the warden will review this widening", wdir))
		}
	}
	return errs, warns
}

func lintChargebookFM(fm map[string]string) (errs []string) {
	if v := strings.TrimSpace(fm["default_run_ceiling_usd"]); v == "" {
		errs = append(errs, "default_run_ceiling_usd is required")
	} else if _, err := strconv.ParseFloat(v, 64); err != nil {
		errs = append(errs, fmt.Sprintf("default_run_ceiling_usd %q is not a number", v))
	}
	for k, v := range fm {
		if strings.HasPrefix(k, "price.") || strings.HasPrefix(k, "cast.") {
			if _, err := strconv.ParseFloat(strings.TrimSpace(v), 64); err != nil {
				errs = append(errs, fmt.Sprintf("%s %q is not a number", k, v))
			}
		}
	}
	return errs
}

// --- helpers ---

var slugRe = regexp.MustCompile(`^[a-z0-9][a-z0-9_-]*$`)

func validSlug(s string) bool { return slugRe.MatchString(s) }

func isRitualPath(rel string) bool {
	p := strings.Split(rel, "/")
	return len(p) == 4 && p[0] == "spirits" && p[2] == "rituals" && strings.HasSuffix(p[3], ".md")
}

// allowedEditPath returns the cleaned repo-relative path if it is on the editor
// allow-list — spirits/<s>/identity.md, spirits/<s>/cornerstone.md,
// spirits/<s>/rituals/<r>.md, chargebook.md — else ("", false). Everything else
// (engine/, vessel/, artifacts/, memories/, traversal) is refused.
func allowedEditPath(rel string) (string, bool) {
	rel = filepath.ToSlash(filepath.Clean(strings.TrimSpace(rel)))
	if rel == "" || rel == "." || filepath.IsAbs(rel) || strings.Contains(rel, "..") {
		return "", false
	}
	if rel == "chargebook.md" {
		return rel, true
	}
	p := strings.Split(rel, "/")
	if len(p) == 3 && p[0] == "spirits" && validSlug(p[1]) && (p[2] == "identity.md" || p[2] == "cornerstone.md") {
		return rel, true
	}
	if len(p) == 4 && p[0] == "spirits" && validSlug(p[1]) && p[2] == "rituals" &&
		strings.HasSuffix(p[3], ".md") && validSlug(strings.TrimSuffix(p[3], ".md")) {
		return rel, true
	}
	return "", false
}

func (s *Store) chargebookDefault() float64 {
	b, err := os.ReadFile(filepath.Join(s.root, "chargebook.md"))
	if err != nil {
		return 0
	}
	fm, _ := mdfm.Split(string(b))
	f, _ := strconv.ParseFloat(strings.TrimSpace(fm["default_run_ceiling_usd"]), 64)
	return f
}

func (s *Store) portalExists(name string) bool {
	if !validSlugDot(name) {
		return false
	}
	_, err := os.Stat(filepath.Join(s.root, "grimoire", "portals", name+".md"))
	return err == nil
}

// portal names may contain dots (e.g. openai-compat.example); still no slashes.
var slugDotRe = regexp.MustCompile(`^[a-z0-9][a-z0-9._-]*$`)

func validSlugDot(s string) bool { return slugDotRe.MatchString(s) && !strings.Contains(s, "..") }

// engineRitualErrors reads the engine's ritual-status.json and returns the
// reason for each ritual the engine considers invalid (authoritative on what it
// actually skips). Empty when the engine hasn't run.
func (s *Store) engineRitualErrors() map[string]string {
	out := map[string]string{}
	b, err := os.ReadFile(filepath.Join(s.root, "vessel", "state", "ritual-status.json"))
	if err != nil {
		return out
	}
	var st struct {
		Rituals []struct {
			Spirit string `json:"spirit"`
			Ritual string `json:"ritual"`
			Valid  bool   `json:"valid"`
			Error  string `json:"error"`
		} `json:"rituals"`
	}
	if json.Unmarshal(b, &st) != nil {
		return out
	}
	for _, e := range st.Rituals {
		if !e.Valid && e.Error != "" {
			out[e.Spirit+"/"+e.Ritual] = e.Error
		}
	}
	return out
}

var dayNames = []string{"Sun", "Mon", "Tue", "Wed", "Thu", "Fri", "Sat"}

// humanCadence renders common cron shapes ("0 7 * * *" → "daily 7:00a",
// "30 7 * * 0" → "Sun 7:30a", "0 8 * * 1-5" → "weekdays 8:00a"); anything it
// doesn't recognize falls back to the raw expression.
func humanCadence(expr string) string {
	f := strings.Fields(expr)
	if len(f) != 5 {
		return expr
	}
	mn, e1 := strconv.Atoi(f[0])
	hr, e2 := strconv.Atoi(f[1])
	if e1 != nil || e2 != nil || f[2] != "*" || f[3] != "*" {
		return expr
	}
	t := clock(hr, mn)
	switch dow := f[4]; {
	case dow == "*":
		return "daily " + t
	case dow == "1-5":
		return "weekdays " + t
	default:
		if d, err := strconv.Atoi(dow); err == nil && d >= 0 && d <= 6 {
			return dayNames[d] + " " + t
		}
	}
	return expr
}

func clock(hr, mn int) string {
	ap, h := "a", hr
	switch {
	case hr == 0:
		h = 12
	case hr == 12:
		ap = "p"
	case hr > 12:
		h, ap = hr-12, "p"
	}
	return fmt.Sprintf("%d:%02d%s", h, mn, ap)
}
