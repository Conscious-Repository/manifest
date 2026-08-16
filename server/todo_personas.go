package server

// Personas (persona plan Phase 1): owner guidance to agents on HOW to respond
// to an intent-tagged ask — one system-zone record per intent at
// <SystemRoot>/agents/personas/<intent>.md, seeded write-once, hand-editable
// in Obsidian or the #/note view. The mention token grows an optional
// `::<intent>` suffix (`agent:hermes::brief`) — typeahead-inserted,
// structurally recorded, never new syntax. The bare token stays the ONLY form
// [owner::] ever holds: intent is per-message, never per-assignment.
//
// The `model:` frontmatter key is the Phase 3 slot — parsed, shipped in the
// listing, consumed by NOTHING. When a per-persona model lands, it either (a)
// rides the spool JSON as a `model` field (the chat spool already carries
// one, so the run spool growing the same field is contract-consistent), or
// (b) maps to a portal switch via the existing cornerstone editor. Choosing
// between them is a later decision; nothing here depends on it.

import (
	"log"
	"net/http"
	"os"
	"path"
	"regexp"
	"sort"
	"strings"

	"manifest/mdfm"
)

type personasCfg struct {
	absDir  string // filesystem dir for listing
	relRoot string // vault-relative slash root for note-view links
}

// UsePersonas wires the persona layer (absDir for os.ReadDir, relRoot for
// vault-relative links). Only writes go through capabilities; listing is a read.
func (s *Server) UsePersonas(absDir, relRoot string) {
	if strings.TrimSpace(absDir) == "" {
		return
	}
	s.personasCfg = &personasCfg{absDir: absDir, relRoot: path.Clean(relRoot)}
}

type persona struct {
	Intent  string `json:"intent"`
	Model   string `json:"model,omitempty"` // Phase 3 slot — parsed, unused
	Enabled bool   `json:"enabled"`
	Prompt  string `json:"-"` // body; never shipped in list payloads
	Rel     string `json:"rel"`
}

var personaIntentRe = regexp.MustCompile(`^[a-z0-9-]{1,16}$`)

// maxPersonaPrompt keeps a persona preamble from crowding the task text out
// of a work order — over-long prompts are truncated at load, with a log line.
const maxPersonaPrompt = 2000

// personas loads every enabled-or-not persona record, keyed by intent.
// Lint at load: the intent field must match the filename stem and the slug
// grammar — a mismatch skips the file with a log line.
func (s *Server) personas() map[string]persona {
	out := map[string]persona{}
	if s.personasCfg == nil {
		return out
	}
	entries, err := os.ReadDir(s.personasCfg.absDir)
	if err != nil {
		return out
	}
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".md") {
			continue
		}
		stem := strings.TrimSuffix(e.Name(), ".md")
		raw, err := os.ReadFile(s.personasCfg.absDir + string(os.PathSeparator) + e.Name())
		if err != nil {
			continue
		}
		fm, body := mdfm.Split(string(raw))
		intent := strings.TrimSpace(fm["intent"])
		if intent != stem || !personaIntentRe.MatchString(intent) {
			log.Printf("personas: %s skipped (intent %q must match the filename stem and [a-z0-9-]{1,16})", e.Name(), intent)
			continue
		}
		prompt := strings.TrimSpace(body)
		if len(prompt) > maxPersonaPrompt {
			log.Printf("personas: %s prompt over %d chars — truncated in work orders", e.Name(), maxPersonaPrompt)
			prompt = prompt[:maxPersonaPrompt]
		}
		out[intent] = persona{
			Intent:  intent,
			Model:   strings.TrimSpace(fm["model"]),
			Enabled: strings.ToLower(strings.TrimSpace(fm["enabled"])) != "false",
			Prompt:  prompt,
			Rel:     s.personasCfg.relRoot + "/" + e.Name(),
		}
	}
	return out
}

// persona resolves one ENABLED intent ("", false when unknown or disabled).
func (s *Server) persona(intent string) (persona, bool) {
	if intent == "" {
		return persona{}, false
	}
	p, ok := s.personas()[intent]
	if !ok || !p.Enabled {
		return persona{}, false
	}
	return p, true
}

// personaPhase maps an intent to its work-order phase: `plan` drafts (the §12
// lane), every other persona answers in the thread (a do-not-execute phase).
func personaPhase(intent string) string {
	if intent == "plan" {
		return "plan"
	}
	return "comment"
}

// splitAgentToken: "agent:hermes::brief" → ("agent:hermes", "brief");
// "agent:hermes" → ("agent:hermes", ""). Non-agent tokens pass through with
// no intent. The bare token stays the ONLY form [owner::] ever holds.
func splitAgentToken(tok string) (base, intent string) {
	if !strings.HasPrefix(tok, "agent:") {
		return tok, ""
	}
	if i := strings.Index(tok, "::"); i >= 0 {
		return tok[:i], strings.TrimSpace(tok[i+2:])
	}
	return tok, ""
}

// SeedPersonas — the three seeded intents (owner's Q1 set), write-once via
// vaultwriter.CreateRecord; editing afterwards is Obsidian or the note view.
var SeedPersonas = map[string]string{
	"brief": `---
intent: brief
model:
enabled: true
---
You are answering a quick tagged ask on the owner's todo thread.
Prefer brevity: a few sentences, direct, no preamble — but when a real
answer genuinely needs more room, take it. Answer in the thread; do not
write or modify a plan unless the ask is explicitly about the plan.
Never execute anything.
`,
	"info": `---
intent: info
model:
enabled: true
---
The owner is asking for information. Answer from what you can read —
the vault, the task's thread and plan, and (when your tools allow) the
web. Keep it tight but complete: lead with the answer, cite where it
came from when that helps. Your reply goes to the thread as a comment.
Never touch the plan. Never execute anything.
`,
	"plan": `---
intent: plan
model:
enabled: true
---
The owner wants a plan. Produce a complete, concrete, numbered plan —
zero questions inside it (if you cannot proceed without answers, your
ENTIRE brief is the questions instead, under a leading '# questions'
heading). If a plan already exists on the task and the situation has
changed, REPLACE it with the updated plan rather than appending. Plan
over execute: never do the work in this phase — the owner fires the
plan explicitly when it is right.
`,
}

// handlePersonas lists the persona records (no editor UI — records are edited
// as vault notes).
func (s *Server) handlePersonas(w http.ResponseWriter, r *http.Request) {
	all := s.personas()
	intents := make([]string, 0, len(all))
	for k := range all {
		intents = append(intents, k)
	}
	sort.Strings(intents)
	out := make([]persona, 0, len(all))
	for _, k := range intents {
		out = append(out, all[k])
	}
	writeJSON(w, map[string]any{"personas": out})
}
