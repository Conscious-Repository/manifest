// Package agentchat is the file-as-truth store for Hermes-family chat
// sessions (agent-chat plan Phase 1, §3.2/§3.6): the conversations the owner
// has with Alfred (the default Hermes profile) and with any named Hermes
// profile from the cockpit CHAT tab.
//
// Layout — INSIDE the primary harness tree so the files sync across devices
// like the spirit sessions beside them (plan Q7):
//
//	<harness>/artifacts/chats/<agent>/<id>.md
//
// One session file = frontmatter (session, agent, profile, title, created,
// updated, status, turns, charge_spent_usd, model, hermes_session) + turn
// blocks in EXACTLY the spirit-session grammar,
//
//	## Turn N — <who> · <RFC3339>[ · $<usd>]
//
// so the dashboard's transcript renderer (48-chat.js parseChatTurns) is shared
// byte-for-byte. Assistant turns wrap the reply in a `### Step 1 — say` step
// for the same reason (the renderer markdown-renders a say step).
//
// Writer discipline: MANIFEST is the single writer (unlike spirit sessions,
// which the engine rewrites). Every write is tmp+rename under a per-thread
// mutex. While a turn is in flight the file carries `status: thinking`; a
// second send during that window is retained in the same session file. Request
// receipts deduplicate retries, and recovery resumes only messages that had not
// started. Interrupted provider calls are never silently replayed.
package agentchat

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"manifest/mdfm"
)

// Status values.
const (
	StatusIdle     = "idle"
	StatusThinking = "thinking"
)

// idRe mirrors the spirit engine's session-id grammar (spirits.chatSessionIDRe).
var idRe = regexp.MustCompile(`^[0-9]{8}-[0-9]{6}-[0-9a-z]{2,8}$`)

// agentRe is the addressable-agent slug rule (the Hermes profile name rule).
var agentRe = regexp.MustCompile(`^[a-z0-9][a-z0-9-]{0,31}$`)

// ValidAgent reports whether name is an acceptable agent slug.
func ValidAgent(name string) bool { return agentRe.MatchString(name) }

// ValidID reports whether id is a well-formed session id.
func ValidID(id string) bool { return idRe.MatchString(id) }

// Session is a parsed session frontmatter row (the JSON shape mirrors
// spirits.ChatSessionSummary so the rail/transcript code needs no branches).
type Origin struct {
	Mode           string              `json:"mode,omitempty"`
	Context        string              `json:"context,omitempty"`
	HistoryOmitted int                 `json:"historyOmitted,omitempty"`
	Backend        string              `json:"backend,omitempty"`
	Agent          string              `json:"agent"`
	ID             string              `json:"id"`
	Task           string              `json:"task,omitempty"`
	Prompt         string              `json:"prompt,omitempty"`
	Artifacts      []ArtifactReference `json:"artifacts,omitempty"`
}

func validOrigin(o Origin) bool {
	if o.Mode != "" && o.Mode != "side" && (o.Mode != "continue" || o.Backend != "terminal") {
		return false
	}
	if o.Backend == "" {
		return ValidAgent(o.Agent) && ValidID(o.ID)
	}
	if o.Backend != "terminal" || (o.Agent != "claude" && o.Agent != "codex") || len(o.ID) < 8 || len(o.ID) > 32 {
		return false
	}
	for _, c := range o.ID {
		if !strings.ContainsRune("0123456789abcdef", c) {
			return false
		}
	}
	return true
}

func originJSON(o *Origin) string {
	if o == nil {
		return ""
	}
	b, _ := json.Marshal(o)
	return string(b)
}

type Session struct {
	Sharing         *ShareState `json:"sharing,omitempty"`
	Origin          *Origin     `json:"origin,omitempty"`
	Deliveries      []Delivery  `json:"deliveries,omitempty"`
	CreateRequest   string      `json:"-"`
	CreateSignature string      `json:"-"`
	ID              string      `json:"id"`
	Agent           string      `json:"agent"`
	Profile         string      `json:"profile"` // "" = the default Hermes profile
	Title           string      `json:"title"`
	Created         string      `json:"created"`
	Updated         string      `json:"updated"`
	Status          string      `json:"status"` // idle | thinking
	Turns           int         `json:"turns"`
	SpentUSD        float64     `json:"spentUsd"`
	Model           string      `json:"model"`
	// HermesSession is the Hermes-side session id of the LAST turn (usage
	// report session_id) — a pointer for `hermes sessions search`, never fed
	// back in (see package hermes).
	HermesSession string `json:"hermesSession,omitempty"`
	// Task is the todo this conversation was promoted into ("→ task", plan
	// §3.4f) — the transcript's link back to the board. One per session: a
	// second promote overwrites it (the newest task is the live one).
	Task string `json:"task,omitempty"`
}

// Turn is one parsed turn block.
type Turn struct {
	N    int
	Who  string
	At   string
	USD  string
	Text string
}

// Store is one chats root. Safe for concurrent use.
type Store struct {
	root string

	mu       sync.Mutex // guards per-thread lock map
	createMu sync.Mutex // serializes idempotent conversation creation
	locks    map[string]*sync.Mutex
}

// New opens a store rooted at dir (e.g. <harness>/artifacts/chats). The
// directory is created lazily on the first write.
func New(dir string) *Store {
	return &Store{root: dir, locks: map[string]*sync.Mutex{}}
}

// Root returns the chats root.
func (s *Store) Root() string { return s.root }

func key(agent, id string) string { return agent + "/" + id }

func (s *Store) path(agent, id string) string {
	return filepath.Join(s.root, agent, id+".md")
}

// lock returns the per-thread mutex, creating it on first use.
func (s *Store) lock(agent, id string) *sync.Mutex {
	s.mu.Lock()
	defer s.mu.Unlock()
	k := key(agent, id)
	m, ok := s.locks[k]
	if !ok {
		m = &sync.Mutex{}
		s.locks[k] = m
	}
	return m
}

// ---- parse / render ----

func parse(content string) (Session, string) {
	fm, body := mdfm.Split(content)
	sess := Session{
		ID: fm["session"], Agent: fm["agent"], Profile: fm["profile"], Title: fm["title"],
		Created: fm["created"], Updated: fm["updated"], Status: fm["status"],
		Model: fm["model"], HermesSession: fm["hermes_session"], Task: fm["task"],
	}
	_ = json.Unmarshal([]byte(fm["deliveries"]), &sess.Deliveries)
	_ = json.Unmarshal([]byte(fm["origin"]), &sess.Origin)
	if fm["sharing"] != "" {
		if err := json.Unmarshal([]byte(fm["sharing"]), &sess.Sharing); err != nil || sess.Sharing == nil {
			sess.Sharing = &ShareState{State: "invalid"} // Never reopen a corrupt sharing fence.
		}
	}
	sess.CreateRequest = fm["create_request"]
	sess.CreateSignature = fm["create_signature"]
	sess.Turns, _ = strconv.Atoi(fm["turns"])
	sess.SpentUSD, _ = strconv.ParseFloat(fm["charge_spent_usd"], 64)
	if sess.Status == "" {
		sess.Status = StatusIdle
	}
	return sess, strings.TrimSpace(body)
}

func render(sess Session, body string) string {
	return (&mdfm.Writer{}).
		Set("session", sess.ID).
		Set("origin", originJSON(sess.Origin)).
		Set("sharing", shareJSON(sess.Sharing)).
		Set("create_request", sess.CreateRequest).
		Set("create_signature", sess.CreateSignature).
		Set("deliveries", deliveryJSON(sess.Deliveries)).
		Set("agent", sess.Agent).
		Set("profile", sess.Profile).
		Set("title", sess.Title).
		Set("created", sess.Created).
		Set("updated", sess.Updated).
		SetRaw("status", sess.Status).
		SetRaw("turns", strconv.Itoa(sess.Turns)).
		SetRaw("charge_spent_usd", fmt.Sprintf("%.4f", sess.SpentUSD)).
		Set("model", sess.Model).
		Set("hermes_session", sess.HermesSession).
		Set("task", sess.Task).
		String(body)
}

var turnRe = regexp.MustCompile(`(?m)^## Turn (\d+) — (.+?) · (\S+)( · \$([\d.]+))?$`)

// ParseTurns splits a session body into turn blocks (the Go twin of
// 48-chat.js parseChatTurns; used to compose the prompt window).
func ParseTurns(body string) []Turn {
	locs := turnRe.FindAllStringSubmatchIndex(body, -1)
	var out []Turn
	for i, m := range locs {
		n, _ := strconv.Atoi(body[m[2]:m[3]])
		t := Turn{N: n, Who: strings.TrimSpace(body[m[4]:m[5]]), At: body[m[6]:m[7]]}
		if m[10] >= 0 {
			t.USD = body[m[10]:m[11]]
		}
		end := len(body)
		if i+1 < len(locs) {
			end = locs[i+1][0]
		}
		t.Text = strings.TrimSpace(body[m[1]:end])
		out = append(out, t)
	}
	return out
}

var (
	sayStepRe = regexp.MustCompile(`(?m)^### Step \d+ — say[ \t]*$`)
	anyStepRe = regexp.MustCompile(`(?m)^### Step \d+ — `)
)

// SayBody returns the reply text of an assistant turn: the body of its `say`
// step when present (whatever its number — a portal reply puts a trace step
// first), up to the next step, else the whole text.
func SayBody(text string) string {
	loc := sayStepRe.FindStringIndex(text)
	if loc == nil {
		return strings.TrimSpace(text)
	}
	rest := text[loc[1]:]
	if next := anyStepRe.FindStringIndex(rest); next != nil {
		rest = rest[:next[0]]
	}
	return strings.TrimSpace(rest)
}

func (s *Store) read(agent, id string) (Session, string, error) {
	b, err := os.ReadFile(s.path(agent, id))
	if err != nil {
		return Session{}, "", err
	}
	fm, _ := mdfm.Split(string(b))
	if raw := fm["deliveries"]; raw != "" {
		var ds []Delivery
		if err := json.Unmarshal([]byte(raw), &ds); err != nil {
			return Session{}, "", errors.New("invalid delivery journal")
		}
	}
	sess, body := parse(string(b))
	if sess.ID == "" {
		sess.ID = id
	}
	if sess.Agent == "" {
		sess.Agent = agent
	}
	return sess, body, nil
}

// write is tmp+rename; the caller holds the thread lock.
func (s *Store) write(agent, id string, sess Session, body string) error {
	p := s.path(agent, id)
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		return err
	}
	tmp := p + ".tmp"
	if err := os.WriteFile(tmp, []byte(render(sess, body)), 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, p)
}

// update applies fn to the session under its lock and persists the result.
func (s *Store) update(agent, id string, fn func(*Session, *string) error) (Session, error) {
	if !ValidAgent(agent) || !ValidID(id) {
		return Session{}, errors.New("bad agent/session")
	}
	m := s.lock(agent, id)
	m.Lock()
	defer m.Unlock()
	sess, body, err := s.read(agent, id)
	if err != nil {
		return Session{}, errors.New("no such session")
	}
	before := render(sess, body)
	frozen := sess.Sharing != nil
	if err := fn(&sess, &body); err != nil {
		return Session{}, err
	}
	if frozen {
		if render(sess, body) != before {
			return Session{}, ErrShared
		}
		return sess, nil
	}
	sess.Updated = now()
	return sess, s.write(agent, id, sess, body)
}

func now() string { return time.Now().Format(time.RFC3339) }

// ---- API ----

// List returns an agent's sessions, newest-updated first.
func (s *Store) List(agent string) []Session {
	out := []Session{}
	if !ValidAgent(agent) {
		return out
	}
	entries, _ := os.ReadDir(filepath.Join(s.root, agent))
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".md") {
			continue
		}
		id := strings.TrimSuffix(e.Name(), ".md")
		if !ValidID(id) {
			continue
		}
		if sess, _, err := s.read(agent, id); err == nil {
			out = append(out, sess)
		}
	}
	sort.Slice(out, func(i, j int) bool {
		a, b := out[i].Updated, out[j].Updated
		if a == "" {
			a = out[i].Created
		}
		if b == "" {
			b = out[j].Created
		}
		return a > b
	})
	return out
}

// Agents lists every agent directory that holds at least one session.
func (s *Store) Agents() []string {
	entries, _ := os.ReadDir(s.root)
	var out []string
	for _, e := range entries {
		if e.IsDir() && ValidAgent(e.Name()) && len(s.List(e.Name())) > 0 {
			out = append(out, e.Name())
		}
	}
	sort.Strings(out)
	return out
}

// Get returns one session, its raw turn body, and the persisted queue.
func (s *Store) Get(agent, id string) (Session, string, []string, bool) {
	if !ValidAgent(agent) || !ValidID(id) {
		return Session{}, "", nil, false
	}
	sess, body, err := s.read(agent, id)
	if err != nil {
		return Session{}, "", nil, false
	}
	return sess, body, s.Queued(agent, id), true
}

// Create writes a fresh session skeleton and returns its id.
func (s *Store) Create(agent, profile, title, model string) (string, error) {
	return s.CreateOnce(agent, profile, title, model, "")
}

func (s *Store) create(agent, profile, title, model, requestID, signature string, origin *Origin) (string, error) {
	if !ValidAgent(agent) {
		return "", errors.New("bad agent name")
	}
	if profile = strings.TrimSpace(profile); profile != "" && !ValidAgent(profile) {
		return "", errors.New("bad profile name")
	}
	b := make([]byte, 2)
	_, _ = rand.Read(b)
	id := time.Now().Format("20060102-150405") + "-" + hex.EncodeToString(b)
	title = strings.TrimSpace(title)
	if title == "" {
		title = "new conversation"
	}
	ts := now()
	sess := Session{ID: id, Agent: agent, Profile: profile, Title: title, Created: ts, Updated: ts,
		Origin: origin, Status: StatusIdle, Model: strings.TrimSpace(model), CreateRequest: requestID, CreateSignature: signature}
	if origin != nil {
		sess.Task = origin.Task
	}
	m := s.lock(agent, id)
	m.Lock()
	defer m.Unlock()
	return id, s.write(agent, id, sess, "")
}

// AppendTurn appends one turn block (who = "user", "system", or the agent
// name) and returns its number. usd > 0 stamps the charge on the heading and
// accrues it on the session.
func (s *Store) AppendTurn(agent, id, who, text string, usd float64) (int, error) {
	n := 0
	_, err := s.update(agent, id, func(sess *Session, body *string) error {
		text = strings.TrimSpace(text)
		if text == "" {
			return errors.New("empty turn")
		}
		sess.Turns++
		n = sess.Turns
		head := fmt.Sprintf("## Turn %d — %s · %s", n, who, now())
		if usd > 0 {
			head += fmt.Sprintf(" · $%.4f", usd)
			sess.SpentUSD += usd
		}
		if *body != "" {
			*body += "\n\n"
		}
		*body += head + "\n\n" + text
		return nil
	})
	return n, err
}

// SetStatus flips the frontmatter status flag (thinking ↔ idle).
func (s *Store) SetStatus(agent, id, status string) error {
	_, err := s.update(agent, id, func(sess *Session, _ *string) error {
		sess.Status = status
		return nil
	})
	return err
}

// SetHermesSession records the Hermes-side session id of the last turn.
func (s *Store) SetHermesSession(agent, id, hermesSession string) error {
	_, err := s.update(agent, id, func(sess *Session, _ *string) error {
		sess.HermesSession = strings.TrimSpace(hermesSession)
		return nil
	})
	return err
}

// SetTask records the todo this session was promoted into.
func (s *Store) SetTask(agent, id, task string) error {
	_, err := s.update(agent, id, func(sess *Session, _ *string) error {
		sess.Task = strings.TrimSpace(task)
		return nil
	})
	return err
}

// Rename retitles a session.
func (s *Store) Rename(agent, id, title string) error {
	title = strings.TrimSpace(title)
	if title == "" {
		return errors.New("title cannot be empty")
	}
	_, err := s.update(agent, id, func(sess *Session, _ *string) error {
		sess.Title = title
		return nil
	})
	return err
}

// Delete removes a session (refused while a turn is in flight — the goroutine
// would resurrect it on landing).
func (s *Store) Delete(agent, id string) error {
	if !ValidAgent(agent) || !ValidID(id) {
		return errors.New("bad agent/session")
	}
	m := s.lock(agent, id)
	m.Lock()
	defer m.Unlock()
	if sess, _, err := s.read(agent, id); err == nil && sess.Sharing != nil {
		return ErrShared
	}
	if s.InFlight(agent, id) {
		return errors.New("session has pending work — try again in a moment")
	}
	if err := os.Remove(s.path(agent, id)); err != nil {
		return errors.New("no such session")
	}
	return nil
}

// InFlight includes accepted work so a pending instruction cannot disappear
// through deletion while waiting for its worker.
func (s *Store) InFlight(agent, id string) bool {
	sess, _, err := s.read(agent, id)
	if err != nil {
		return false
	}
	for _, d := range sess.Deliveries {
		if d.State == DeliveryRunning || d.State == DeliveryQueued {
			return true
		}
	}
	return sess.Status == StatusThinking
}
func (s *Store) Queued(agent, id string) []string {
	sess, _, err := s.read(agent, id)
	if err != nil {
		return nil
	}
	var out []string
	for _, d := range sess.Deliveries {
		if d.State == DeliveryQueued {
			out = append(out, d.Text)
		}
	}
	return out
}

// Recover repairs sessions left `thinking` by a process that died mid-turn:
// the flag goes back to idle and a system turn records the interruption. Call
// once at startup, before any turn can start. Returns the ids repaired.
func (s *Store) Recover() []string {
	var fixed []string
	for _, agent := range s.Agents() {
		for _, sess := range s.List(agent) {
			if sess.Status != StatusThinking {
				continue
			}
			_, err := s.update(agent, sess.ID, func(x *Session, body *string) error {
				interrupted := false
				queued := false
				for i := range x.Deliveries {
					d := &x.Deliveries[i]
					switch d.State {
					case DeliveryRunning:
						d.State = DeliveryInterrupted
						d.Error = "Server restarted; provider delivery is uncertain and was not replayed"
						d.Updated = now()
						interrupted = true
					case DeliveryQueued:
						queued = true
					}
				}
				// Sessions created by the old transport have no receipt for the active turn.
				if interrupted || len(x.Deliveries) == 0 {
					appendTurn(x, body, "system", "The previous turn was interrupted by a restart. It was not replayed; review its result before asking to run it again.", 0)
				}
				x.Status = StatusIdle
				if queued {
					x.Status = StatusThinking
				}
				return nil
			})
			if err == nil {
				fixed = append(fixed, key(agent, sess.ID))
			}
		}
	}
	return fixed
}
