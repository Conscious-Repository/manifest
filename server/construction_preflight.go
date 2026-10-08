package server

// Construction native preflight (plan §6, P8): what must be true before an
// autonomous construction step is sent, checked from the running server. A
// failed check makes native research unavailable (no silent substitution,
// no cloud fallback). An "unverified" check is a guarantee this process
// cannot observe (the runner enforcing its -t scope) — the same trust every
// native chat turn already places in the runner — so it is reported, not
// blocking.

import (
	"net/http"
	"strings"

	"manifest/construction"
)

type constructionCheck struct {
	Name   string `json:"name"`
	Status string `json:"status"` // pass | fail | unverified
	Detail string `json:"detail"`
}

type constructionPreflightReport struct {
	Ready           bool                `json:"ready"`
	AllowUnverified bool                `json:"allowUnverified"`
	Checks          []constructionCheck `json:"checks"`
}

// ConstructionNoTools is the explicit empty toolset production gives
// construction steps (-t none, as the extractor duties use): a step gets its
// retained packet as data and replies with JSON, so it needs no tools.
const ConstructionNoTools = "none"

// toolsets that would give a construction step fetch, shell or write power.
var constructionForbiddenTools = []string{"web", "browser", "browse", "fetch", "search", "http", "terminal", "shell", "code", "files", "file", "write", "edit", "mcp", "vault", "memory"}

func (s *Server) constructionPreflight() constructionPreflightReport {
	var out constructionPreflightReport
	add := func(name, status, detail string) {
		out.Checks = append(out.Checks, constructionCheck{Name: name, Status: status, Detail: detail})
	}
	c := s.construction
	if s.agentChat == nil {
		add("native-store", "fail", "no native chat store is wired here")
	} else {
		add("native-store", "pass", "native deliveries are accepted, claimed and finished in the agent chat store")
	}
	if s.hermes == nil || s.hermes.runner == nil || !s.hermes.runner.Enabled() {
		add("runner", "fail", "the native runner is not enabled")
	} else {
		add("runner", "pass", "the existing runner executes the step")
	}
	add("exact-bytes", "pass", "the packet is embedded verbatim from the retained artifact after its hash is verified")
	add("page-extraction", "pass", "text sources carry page maps; PDFs are retained without extraction and need owner excerpts")
	scope := strings.TrimSpace(c.opts.AgentToolsets)
	if scope == "" {
		add("bounded-tools", "fail", "no explicit bounded toolset is configured for construction steps (the profile default could include file, shell or web tools)")
	} else {
		bad := ""
		for _, t := range strings.Split(strings.ToLower(scope), ",") {
			for _, f := range constructionForbiddenTools {
				if strings.TrimSpace(t) == f {
					bad = t
				}
			}
		}
		switch {
		case bad != "":
			add("bounded-tools", "fail", "the construction toolset includes "+bad)
		case strings.EqualFold(scope, ConstructionNoTools):
			add("bounded-tools", "pass", "steps run with the explicit empty toolset (-t none)")
		default:
			add("bounded-tools", "unverified", "toolset "+scope+" is requested per step (-t); the runner enforces it as for every native chat turn")
		}
	}
	add("source-fetch", map[bool]string{true: "pass", false: "fail"}[scope != "" && !strings.Contains(strings.ToLower(scope), "web")], "construction steps receive retained text only and no fetch authority")
	if strings.EqualFold(scope, ConstructionNoTools) {
		add("no-vault-write", "pass", "the step is granted no tools, so it has no write tool")
	} else {
		add("no-vault-write", "unverified", "the step has no write tool in its requested scope; the agent's OS-level file access is the runner's, as for every native chat turn")
	}
	out.AllowUnverified = c.opts.AllowUnverifiedNative
	out.Ready = true
	for _, ch := range out.Checks {
		if ch.Status == "fail" {
			out.Ready = false
		}
	}
	return out
}

func init() {
	constructionNativeReadyHook = func(s *Server) (bool, []string) {
		p := s.constructionPreflight()
		if p.Ready {
			notes := []string{}
			for _, ch := range p.Checks {
				if ch.Status == "unverified" {
					notes = append(notes, ch.Name+": "+ch.Detail)
				}
			}
			return true, notes
		}
		var why []string
		for _, ch := range p.Checks {
			if ch.Status == "fail" {
				why = append(why, ch.Name+": "+ch.Detail)
			}
		}
		return false, []string{"native agent research is unavailable here — " + strings.Join(why, "; ")}
	}
	constructionAgentToolHook = func(s *Server) string {
		if ready, _ := s.constructionNativeReady(); ready {
			return "available (in-process capability; drafts and proposals only)"
		}
		return "unavailable"
	}
}

func (s *Server) handleConstructionPreflight(w http.ResponseWriter, r *http.Request) {
	if _, _, ok := s.constructionBegin(w, r, false); !ok {
		return
	}
	constructionJSON(w, map[string]any{"preflight": s.constructionPreflight(), "notice": construction.NonApprovalNotice})
}
