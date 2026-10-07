package olgachat

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"strings"
	"sync"
	"time"

	"manifest/jev"
)

// Voice answers one composed prompt (Liber's Hermes profile on Sol).
type Voice interface {
	// Ask answers one composed prompt; images are absolute paths of photos
	// she attached to this message (none for most turns).
	Ask(ctx context.Context, prompt string, images []string) (VoiceAnswer, error)
}

type VoiceAnswer struct {
	Text   string
	Model  string
	Tokens int
}

// Router is the Jev route judgment (nil or erroring → the voice decides).
type Router interface {
	Route(ctx context.Context, in jev.RouteInput) (*jev.RouteAdvice, error)
}

// Planner gives the voice its context and applies proposals through the
// app's own handlers. Implemented by the server.
type Planner interface {
	TaskContext(taskID string) (map[string]any, error)
	TaskInfo(taskID string) (title string, shared bool, ok bool)
	// Validate turns a suggestion into an appliable proposal and the line she
	// reads, or refuses it (unknown task, foreign area, invalid plan patch).
	Validate(taskID string, p RawProposal) (*Proposal, string, error)
	Apply(p *Proposal) (string, error)
	// ErrConflict from Apply means the data changed since the card was made.
}

// MaxImagesPerMessage bounds the photos in one message.
const MaxImagesPerMessage = 6

// ErrConflict: the record changed since the suggestion was made.
var ErrConflict = errors.New("changed since")

// BuildResult is what a builder run produced.
type BuildResult struct {
	Summary       string
	Files         []string
	NeedsBenjamin string // a request outside her layer, in words
	NoChange      bool
	Model         string
	Session       string
	Problem       string // the change could not be offered (gate failed, build broke); logged
	Gate          string // the phone check: passed | skipped | failed-once
}

// Builder makes, previews and ships app changes (nil → app changes are noted
// for Benjamin instead).
type Builder interface {
	Build(ctx context.Context, ch *Change, brief string, recent []Exchange, images []string) (BuildResult, error)
	PreviewPath(ch *Change) string
	Discard(ch *Change) error
	// Use and Undo ship to her live app; the process restarts afterwards,
	// and Recover settles the card.
	Use(ctx context.Context, ch *Change) error
	Undo(ctx context.Context, ch *Change) error
	// Settled reports the outcome of a deploy that restarted the process:
	// "live", "failed" or "" when none is recorded for this change.
	Settled(ch *Change) string
}

// Logger records one line per turn for Benjamin.
type Logger func(entry map[string]any)

type Service struct {
	Store   *Store
	Voice   Voice
	Router  Router
	Planner Planner
	Builder Builder
	Log     Logger
	Notes   func(text string) error // append to system/olga/requests.md
	Now     func() time.Time
	// Restart exits so systemd starts the app again on the newly installed
	// build (after Use or Undo).
	Restart func()

	VoiceTimeout time.Duration
	BuildTimeout time.Duration
	RouteBudget  time.Duration

	mu    sync.Mutex
	locks map[string]*sync.Mutex
	subs  map[string]map[chan struct{}]bool
}

func (s *Service) now() time.Time {
	if s.Now != nil {
		return s.Now()
	}
	return time.Now()
}

func key(t *Thread) string {
	if t.Kind == KindTask {
		return "task:" + t.TaskID
	}
	return "app:" + t.ID
}

func (s *Service) lock(k string) func() {
	s.mu.Lock()
	if s.locks == nil {
		s.locks = map[string]*sync.Mutex{}
	}
	m := s.locks[k]
	if m == nil {
		m = &sync.Mutex{}
		s.locks[k] = m
	}
	s.mu.Unlock()
	m.Lock()
	return m.Unlock
}

// Subscribe returns a channel that ticks when the thread changes.
func (s *Service) Subscribe(k string) (chan struct{}, func()) {
	ch := make(chan struct{}, 1)
	s.mu.Lock()
	if s.subs == nil {
		s.subs = map[string]map[chan struct{}]bool{}
	}
	if s.subs[k] == nil {
		s.subs[k] = map[chan struct{}]bool{}
	}
	s.subs[k][ch] = true
	s.mu.Unlock()
	return ch, func() {
		s.mu.Lock()
		delete(s.subs[k], ch)
		s.mu.Unlock()
	}
}

func (s *Service) notify(k string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for ch := range s.subs[k] {
		select {
		case ch <- struct{}{}:
		default:
		}
	}
}

// ThreadKey is the subscription key for an app thread id or a task id.
func ThreadKey(kind, id string) string { return kind + ":" + id }

// load gets a thread by reference.
func (s *Service) load(ref Ref) (*Thread, error) {
	if ref.Kind == KindTask {
		title, shared, ok := s.Planner.TaskInfo(ref.ID)
		if !ok {
			return nil, os.ErrNotExist
		}
		t, err := s.Store.Task(ref.ID, shared)
		if err != nil {
			return nil, err
		}
		if t == nil {
			t = &Thread{ID: "task-" + TaskKey(ref.ID), Kind: KindTask, TaskID: ref.ID, Shared: shared, Created: s.now().UTC()}
		}
		t.TaskTitle = title
		return t, nil
	}
	return s.Store.App(ref.ID)
}

// Ref names a thread: an app thread id, or a task id.
type Ref struct{ Kind, ID string }

// Get returns a thread for display (a task thread that doesn't exist yet is
// returned empty).
func (s *Service) Get(ref Ref) (*Thread, error) { return s.load(ref) }

// NewApp starts an app conversation.
func (s *Service) NewApp() (*Thread, error) {
	t := &Thread{ID: NewID("c-"), Kind: KindApp, Created: s.now().UTC()}
	return t, nil
}

var ErrBusy = errors.New("busy")

// Send adds her message and starts Liber's answer. A message sent while
// Liber is still answering is queued and answered next.
func (s *Service) Send(ref Ref, text string, images ...string) (*Thread, error) {
	text = strings.TrimSpace(text)
	if (text == "" && len(images) == 0) || len(text) > 8000 {
		return nil, errors.New("write a message of up to 8000 characters")
	}
	if len(images) > MaxImagesPerMessage {
		return nil, fmt.Errorf("send up to %d photos at a time", MaxImagesPerMessage)
	}
	var t *Thread
	var err error
	if ref.Kind == KindApp && ref.ID == "" {
		t, err = s.NewApp()
	} else {
		t, err = s.load(ref)
	}
	if err != nil {
		return nil, err
	}
	unlock := s.lock(key(t))
	defer unlock()
	if ref.Kind == KindApp && ref.ID != "" {
		if t, err = s.load(ref); err != nil {
			return nil, err
		}
	} else if ref.Kind == KindTask {
		if fresh, err := s.load(ref); err == nil {
			t = fresh
		}
	}
	for _, id := range images {
		if s.Store.ImagePath(id, t.Kind == KindTask && t.Shared) == "" {
			return nil, errors.New("a photo didn't finish uploading — add it again")
		}
	}
	busy := t.Busy()
	olga := Turn{ID: NewID("t-"), Who: "olga", Text: text, Images: images, At: s.now().UTC(), Queued: busy}
	t.Turns = append(t.Turns, olga)
	if t.Title == "" && t.Kind == KindApp {
		t.Title = clipWords(firstNonEmpty(text, "Photo"), 60)
	}
	if !busy {
		t.Turns = append(t.Turns, Turn{ID: NewID("t-"), Who: "liber", At: s.now().UTC(), Status: StatusThinking})
	}
	if err := s.Store.Save(t); err != nil {
		return nil, err
	}
	s.notify(key(t))
	if !busy {
		go s.answer(t.Kind, refOf(t), olga.ID, t.Turns[len(t.Turns)-1].ID, "")
	}
	return t, nil
}

func refOf(t *Thread) Ref {
	if t.Kind == KindTask {
		return Ref{KindTask, t.TaskID}
	}
	return Ref{KindApp, t.ID}
}

func clipWords(s string, n int) string {
	s = strings.Join(strings.Fields(s), " ")
	if len(s) <= n {
		return s
	}
	cut := strings.LastIndex(s[:n], " ")
	if cut < 20 {
		cut = n
	}
	return s[:cut] + "…"
}

// update loads the thread under its lock, applies fn, saves and notifies.
func (s *Service) update(ref Ref, fn func(t *Thread) error) (*Thread, error) {
	t, err := s.load(ref)
	if err != nil {
		return nil, err
	}
	unlock := s.lock(key(t))
	defer unlock()
	if t, err = s.load(ref); err != nil {
		return nil, err
	}
	if err := fn(t); err != nil {
		return t, err
	}
	if err := s.Store.Save(t); err != nil {
		return t, err
	}
	s.notify(key(t))
	return t, nil
}

// unavailable is the one calm line she sees when a model can't answer.
const unavailable = "I can't do that right now — Benjamin's been told. Please try again a little later."

// answer runs one Liber turn for her message olgaID into turn liberID.
// forced: "build" when she tapped Make this change (restatement in olgaText).
func (s *Service) answer(kind string, ref Ref, olgaID, liberID, forced string) {
	start := s.now()
	entry := map[string]any{"at": start.UTC().Format(time.RFC3339), "thread": ref.Kind + ":" + ref.ID, "kind": kind}
	defer func() {
		entry["ms"] = s.now().Sub(start).Milliseconds()
		if s.Log != nil {
			s.Log(entry)
		}
		s.next(ref)
	}()
	t, err := s.load(ref)
	if err != nil {
		entry["error"] = err.Error()
		return
	}
	msg, images := "", []string(nil)
	if tu := t.Turn(olgaID); tu != nil {
		msg = tu.Text
		for _, id := range tu.Images {
			if p := s.Store.ImagePath(id, t.Kind == KindTask && t.Shared); p != "" {
				images = append(images, p)
			}
		}
		if len(images) > 0 {
			entry["images"] = len(images)
			if msg == "" {
				msg = "(Olga sent a photo without a message.)"
			}
		}
	}
	recent := Recent(t, RecentTurns, true)
	if forced == "" {
		recent = trimTo(recent, olgaID, t)
	}
	route, why := s.route(ref, t, msg, forced, entry)
	if kind == KindTask {
		s.taskTurn(t, ref, msg, recent, liberID, images, route, entry)
		entry["why"] = why
		return
	}
	entry["route"], entry["why"] = route, why
	switch {
	case strings.HasPrefix(route, "continue:"):
		s.buildTurn(ref, msg, recent, liberID, strings.TrimPrefix(route, "continue:"), images, entry)
	case route == jev.RouteBuild:
		s.buildTurn(ref, msg, recent, liberID, "", images, entry)
	case route == jev.RouteConfirm:
		s.voiceTurn(ref, ModeConfirm, nil, recent, msg, liberID, images, entry, func(tu *Turn, r Reply) {
			line := firstNonEmpty(r.Restatement, clipWords(msg, 140))
			tu.Cards = append(tu.Cards, Card{ID: NewID("k-"), Kind: CardConfirm, State: StatePending, Summary: line, Updated: s.now().UTC()})
		})
	default:
		jevDown := entry["jev"] == "unavailable"
		s.voiceTurn(ref, ModeTalk, nil, recent, msg, liberID, images, entry, func(tu *Turn, r Reply) {
			// Only when Jev could not judge does the voice's own read add a
			// card; it can never start a build (plan §3.4 step 4).
			if jevDown && r.Route == "confirm" && r.Restatement != "" {
				tu.Cards = append(tu.Cards, Card{ID: NewID("k-"), Kind: CardConfirm, State: StatePending, Summary: r.Restatement, Updated: s.now().UTC()})
			}
		})
	}
}

// trimTo keeps the exchanges before her message (the message itself is
// passed separately).
func trimTo(rec []Exchange, olgaID string, t *Thread) []Exchange {
	if tu := t.Turn(olgaID); tu != nil && len(rec) > 0 && rec[len(rec)-1].Who == "Olga" && rec[len(rec)-1].Text == tu.Text {
		return rec[:len(rec)-1]
	}
	return rec
}

func firstNonEmpty(v ...string) string {
	for _, s := range v {
		if strings.TrimSpace(s) != "" {
			return strings.TrimSpace(s)
		}
	}
	return ""
}

// route applies the rules, then Jev, then falls back to the voice.
func (s *Service) route(ref Ref, t *Thread, msg, forced string, entry map[string]any) (string, string) {
	if forced != "" {
		return forced, "rule: she tapped Make this change"
	}
	if s.Router == nil {
		entry["jev"] = "unavailable"
		return jev.RouteTalk, "no router"
	}
	var recent []string
	for _, e := range Recent(t, 4, true) {
		recent = append(recent, e.Who+": "+clipWords(e.Text, 400))
	}
	budget := s.RouteBudget
	if budget <= 0 {
		budget = 1500 * time.Millisecond
	}
	ctx, cancel := context.WithTimeout(context.Background(), budget)
	defer cancel()
	a, err := s.Router.Route(ctx, jev.RouteInput{Message: msg, Recent: recent, Screen: "chat"})
	if err != nil {
		entry["jev"] = "unavailable"
		entry["jevError"] = err.Error()
		return jev.RouteTalk, "router unavailable"
	}
	entry["jev"] = map[string]any{"changeApp": a.Intent.Probabilities["change_app"], "confidence": a.Intent.Confidence, "specific": a.Specific.Score}
	// A follow-up while a change is waiting for her continues that change.
	if a.Route != jev.RouteTalk {
		if ch := openChange(t); ch != nil {
			return "continue:" + ch.ID, strings.Join(a.Policy, "; ") + "; rule: a change is open"
		}
	}
	return a.Route, strings.Join(a.Policy, "; ")
}

func openChange(t *Thread) *Change {
	if t.Server == nil {
		return nil
	}
	var best *Change
	for _, ch := range t.Server.Changes {
		if ch.State == StateReady && (best == nil || ch.Updated.After(best.Updated)) {
			best = ch
		}
	}
	return best
}

// voiceTurn asks the voice and writes its answer into the Liber turn.
func (s *Service) voiceTurn(ref Ref, mode Mode, ctxData map[string]any, recent []Exchange, msg, liberID string, images []string, entry map[string]any, cards func(*Turn, Reply)) (Reply, bool) {
	prompt := Compose(mode, ctxData, recent, msg, s.now())
	if len(images) > 0 {
		prompt += fmt.Sprintf("\n(Olga attached %d photo(s) to her message; they are included with it. Look at them closely.)\n", len(images))
	}
	to := s.VoiceTimeout
	if to <= 0 {
		to = 3 * time.Minute
	}
	ctx, cancel := context.WithTimeout(context.Background(), to)
	defer cancel()
	ans, err := s.Voice.Ask(ctx, prompt, images)
	entry["voiceModel"], entry["tokens"] = ans.Model, ans.Tokens
	if err != nil {
		entry["error"] = err.Error()
		_, _ = s.update(ref, func(t *Thread) error {
			if tu := t.Turn(liberID); tu != nil {
				tu.Text, tu.Status = unavailable, StatusFailed
			}
			return nil
		})
		return Reply{}, false
	}
	r := ParseReply(ans.Text)
	if strings.TrimSpace(r.Text) == "" {
		r.Text = "Sorry — I lost my train of thought. Could you say that again?"
	}
	_, _ = s.update(ref, func(t *Thread) error {
		tu := t.Turn(liberID)
		if tu == nil {
			return nil
		}
		tu.Text, tu.Status = r.Text, StatusDone
		if cards != nil {
			cards(tu, r)
		}
		if t.Server == nil {
			t.Server = &Server{}
		}
		if t.Server.TurnModels == nil {
			t.Server.TurnModels = map[string]string{}
		}
		t.Server.TurnModels[liberID] = ans.Model
		return nil
	})
	return r, true
}

// taskTurn: planning about one task; suggestions become cards. A request to
// change the app itself, asked from a task, becomes a "Make this change" card
// here too — a task chat never builds on its own (plan §3.4 rule 1).
func (s *Service) taskTurn(t *Thread, ref Ref, msg string, recent []Exchange, liberID string, images []string, route string, entry map[string]any) {
	ctxData, err := s.Planner.TaskContext(t.TaskID)
	if err != nil {
		entry["contextError"] = err.Error()
	}
	wantsChange := route == jev.RouteBuild || route == jev.RouteConfirm || strings.HasPrefix(route, "continue:")
	if strings.HasPrefix(route, "continue:") {
		// she's adjusting a change she's looking at: carry straight on
		s.buildTurn(ref, msg, recent, liberID, strings.TrimPrefix(route, "continue:"), images, entry)
		return
	}
	entry["route"] = "talk"
	if wantsChange {
		entry["route"] = "confirm"
		s.voiceTurn(ref, ModeConfirm, nil, recent, msg, liberID, images, entry, func(tu *Turn, r Reply) {
			line := firstNonEmpty(r.Restatement, clipWords(msg, 140))
			tu.Cards = append(tu.Cards, Card{ID: NewID("k-"), Kind: CardConfirm, State: StatePending, Summary: line, Updated: s.now().UTC()})
		})
		return
	}
	jevDown := entry["jev"] == "unavailable"
	var dropped []string
	s.voiceTurn(ref, ModeTask, ctxData, recent, msg, liberID, images, entry, func(tu *Turn, r Reply) {
		if jevDown && r.Route == "confirm" && r.Restatement != "" {
			tu.Cards = append(tu.Cards, Card{ID: NewID("k-"), Kind: CardConfirm, State: StatePending, Summary: r.Restatement, Updated: s.now().UTC()})
		}
		for _, raw := range r.Proposals {
			if len(tu.Cards) >= 3 {
				dropped = append(dropped, raw.Kind+": more than three suggestions")
				continue
			}
			p, line, err := s.Planner.Validate(t.TaskID, raw)
			if err != nil {
				dropped = append(dropped, raw.Kind+": "+err.Error())
				continue
			}
			tu.Cards = append(tu.Cards, Card{ID: NewID("k-"), Kind: CardProposal, State: StatePending, Summary: line, Proposal: p, Updated: s.now().UTC()})
		}
	})
	if len(dropped) > 0 {
		entry["droppedProposals"] = dropped
	}
}

// next answers a queued message, if any, once a turn has ended.
func (s *Service) next(ref Ref) {
	var olgaID, liberID string
	t, err := s.update(ref, func(t *Thread) error {
		if t.Busy() {
			return errors.New("busy")
		}
		for i := range t.Turns {
			if t.Turns[i].Queued {
				t.Turns[i].Queued = false
				olgaID = t.Turns[i].ID
				liber := Turn{ID: NewID("t-"), Who: "liber", At: s.now().UTC(), Status: StatusThinking}
				t.Turns = append(t.Turns, liber)
				liberID = liber.ID
				return nil
			}
		}
		return errors.New("nothing queued")
	})
	if err != nil || olgaID == "" {
		return
	}
	go s.answer(t.Kind, ref, olgaID, liberID, "")
}

// ---- cards ----

// Act performs a card action: apply | decline (proposals), make (confirm),
// use | discard | undo (changes).
func (s *Service) Act(ref Ref, cardID, action string) (*Thread, error) {
	var start func()
	t, err := s.update(ref, func(t *Thread) error {
		tu, c := t.Card(cardID)
		if c == nil {
			return os.ErrNotExist
		}
		switch {
		case c.Kind == CardProposal && action == "decline" && c.State == StatePending:
			c.State, c.Message = StateDeclined, "Not now"
		case c.Kind == CardProposal && action == "apply" && c.State == StatePending:
			msg, err := s.Planner.Apply(c.Proposal)
			switch {
			case errors.Is(err, ErrConflict):
				c.State, c.Message = StateConflict, "This changed since — ask again."
			case err != nil:
				c.State, c.Message = StateFailed, "That didn't work: "+plain(err)
			default:
				c.State, c.Message = StateApplied, firstNonEmpty(msg, "Done")
			}
		case c.Kind == CardConfirm && action == "decline" && c.State == StatePending:
			c.State, c.Message = StateDeclined, "Not now"
		case c.Kind == CardConfirm && action == "make" && c.State == StatePending:
			if t.Busy() {
				return ErrBusy
			}
			c.State = StateApplied
			olga := Turn{ID: NewID("t-"), Who: "olga", Text: "Yes — make this change: " + c.Summary, At: s.now().UTC()}
			liber := Turn{ID: NewID("t-"), Who: "liber", At: s.now().UTC(), Status: StatusThinking}
			t.Turns = append(t.Turns, olga, liber)
			brief := c.Summary
			start = func() { go s.startBuild(ref, olga.ID, liber.ID, brief) }
		case c.Kind == CardChange:
			ch := t.Server.change(c.ChangeID)
			if ch == nil {
				return os.ErrNotExist
			}
			if s.Builder == nil {
				return errors.New("changes are not available")
			}
			switch {
			case action == "discard" && ch.State == StateReady:
				_ = s.Builder.Discard(ch)
				ch.State, c.State, c.Message = StateDiscarded, StateDiscarded, "Dropped. Your app is unchanged."
			case action == "use" && ch.State == StateReady:
				ch.State, c.State, c.Message = StateDeploying, StateDeploying, "Putting it live…"
				start = func() { go s.ship(ref, c.ID, ch.ID, false) }
			case action == "undo" && ch.State == StateLive && s.now().Before(ch.LiveUntil):
				ch.State, c.State, c.Message = StateDeploying, StateDeploying, "Putting it back…"
				start = func() { go s.ship(ref, c.ID, ch.ID, true) }
			default:
				return errors.New("that isn't possible any more — refresh")
			}
			ch.Updated = s.now().UTC()
		default:
			return errors.New("that isn't possible any more — refresh")
		}
		_ = tu
		c.Updated = s.now().UTC()
		return nil
	})
	if err == nil && start != nil {
		start()
	}
	return t, err
}

func (sv *Server) change(id string) *Change {
	if sv == nil {
		return nil
	}
	return sv.Changes[id]
}

// plain keeps a refusal readable without machinery words.
func plain(err error) string {
	m := err.Error()
	if Leaks(m) || len(m) > 200 {
		return "something went wrong."
	}
	return m
}

// ---- builds ----

// startBuild is the hand-off: the voice acknowledges and writes the brief,
// then the builder runs. restatement is the agreed one-liner from a confirm.
func (s *Service) startBuild(ref Ref, olgaID, liberID, restatement string) {
	entry := map[string]any{"at": s.now().UTC().Format(time.RFC3339), "thread": ref.Kind + ":" + ref.ID, "kind": KindApp, "route": "build", "why": "rule: she tapped Make this change"}
	start := s.now()
	t, err := s.load(ref)
	if err != nil {
		return
	}
	s.buildTurn(ref, "Make this change: "+restatement, Recent(t, RecentTurns, true), liberID, "", s.recentImages(t), entry)
	entry["ms"] = s.now().Sub(start).Milliseconds()
	if s.Log != nil {
		s.Log(entry)
	}
	s.next(ref)
}

// buildTurn: hand-off (voice), build (builder), relay (voice).
// continueID resumes an existing change ("continue:<id>" routes).
func (s *Service) buildTurn(ref Ref, msg string, recent []Exchange, liberID, continueID string, images []string, entry map[string]any) {
	if s.Builder == nil {
		s.noteForBenjamin(ref, msg, liberID, entry, "app changes are switched off")
		return
	}
	r, ok := s.voiceTurn(ref, ModeHandoff, nil, recent, msg, liberID, images, entry, nil)
	if !ok {
		return
	}
	brief := firstNonEmpty(r.Brief, msg)
	ch := &Change{ID: NewID("ch-"), Brief: brief, State: StateBuilding, Created: s.now().UTC(), Updated: s.now().UTC()}
	cardID := NewID("k-")
	_, err := s.update(ref, func(t *Thread) error {
		if t.Server == nil {
			t.Server = &Server{}
		}
		if t.Server.Changes == nil {
			t.Server.Changes = map[string]*Change{}
		}
		tu := t.Turn(liberID)
		if tu != nil {
			tu.Status = StatusWorking
		}
		if prev := t.Server.change(continueID); prev != nil {
			// the same change, adjusted: its card goes back to "working"
			ch = prev
			ch.Brief += "\n\nFollow-up from Olga: " + brief
			ch.State, ch.Updated = StateBuilding, s.now().UTC()
			if id := cardFor(t, ch.ID); id != "" {
				cardID = id
				_, c := t.Card(id)
				c.State, c.Message, c.Summary = StateBuilding, "", "Working on your change…"
			}
			return nil
		}
		t.Server.Changes[ch.ID] = ch
		if tu != nil {
			tu.Cards = append(tu.Cards, Card{ID: cardID, Kind: CardChange, State: StateBuilding, ChangeID: ch.ID, Summary: "Working on your change…", Updated: s.now().UTC()})
		}
		return nil
	})
	if err != nil {
		return
	}
	to := s.BuildTimeout
	if to <= 0 {
		to = 10 * time.Minute
	}
	ctx, cancel := context.WithTimeout(context.Background(), to)
	if continueID != "" {
		brief = ch.Brief
	}
	res, berr := s.Builder.Build(ctx, ch, brief, recent, images)
	cancel()
	entry["builderModel"], entry["files"], entry["problem"], entry["gate"] = res.Model, res.Files, res.Problem, res.Gate
	if berr != nil {
		entry["error"] = berr.Error()
	}
	state, summary, note := StateReady, res.Summary, ""
	switch {
	case berr != nil || res.Problem != "":
		state, summary = StateFailed, "I couldn't finish that one."
	case res.NeedsBenjamin != "":
		state, summary, note = StateDiscarded, "This needs a change Benjamin makes. I've written it up for him.", res.NeedsBenjamin
	case res.NoChange:
		state, summary = StateDiscarded, "Nothing needed changing."
	}
	if note != "" && s.Notes != nil {
		_ = s.Notes(fmt.Sprintf("- %s — Olga asked: %q\n  What it would take: %s\n", s.now().Format("2006-01-02 15:04"), msg, note))
	}
	built := ch
	_, _ = s.update(ref, func(t *Thread) error {
		_, c := t.Card(cardID)
		ch := t.Server.change(built.ID)
		if c != nil {
			c.State, c.Summary, c.Updated = state, firstNonEmpty(summary, "Your change is ready to look at."), s.now().UTC()
			if state == StateDiscarded && note != "" {
				c.Kind = CardNote
			}
		}
		if ch != nil {
			ch.State, ch.Files, ch.Session, ch.Updated = state, res.Files, firstNonEmpty(res.Session, ch.Session), s.now().UTC()
			ch.Worktree = firstNonEmpty(built.Worktree, ch.Worktree)
		}
		if tu := t.Turn(liberID); tu != nil {
			tu.Status = StatusDone
		}
		return nil
	})
	// Relay in her assistant's own voice.
	result := map[string]any{"result": map[string]any{"outcome": state, "summary": summary, "needsBenjamin": note != ""}}
	relayID := NewID("t-")
	_, _ = s.update(ref, func(t *Thread) error {
		t.Turns = append(t.Turns, Turn{ID: relayID, Who: "liber", At: s.now().UTC(), Status: StatusThinking})
		return nil
	})
	s.voiceTurn(ref, ModeRelay, result, nil, "", relayID, nil, map[string]any{}, nil)
}

func cardFor(t *Thread, changeID string) string {
	for _, tu := range t.Turns {
		for _, c := range tu.Cards {
			if c.ChangeID == changeID {
				return c.ID
			}
		}
	}
	return ""
}

func (s *Service) noteForBenjamin(ref Ref, msg, liberID string, entry map[string]any, why string) {
	if s.Notes != nil {
		_ = s.Notes(fmt.Sprintf("- %s — Olga asked: %q (%s)\n", s.now().Format("2006-01-02 15:04"), msg, why))
	}
	entry["noted"] = why
	_, _ = s.update(ref, func(t *Thread) error {
		if tu := t.Turn(liberID); tu != nil {
			tu.Status, tu.Text = StatusDone, "That's a change Benjamin will need to make — I've written it up for him."
			tu.Cards = append(tu.Cards, Card{ID: NewID("k-"), Kind: CardNote, State: StateDiscarded, Summary: "Noted for Benjamin", Updated: s.now().UTC()})
		}
		return nil
	})
}

// ship runs Use or Undo. The process restarts when it succeeds; Recover
// settles the card on the next start.
func (s *Service) ship(ref Ref, cardID, changeID string, undo bool) {
	t, err := s.load(ref)
	if err != nil {
		return
	}
	ch := t.Server.change(changeID)
	if ch == nil {
		return
	}
	cp := *ch
	ctx, cancel := context.WithTimeout(context.Background(), 8*time.Minute)
	defer cancel()
	if undo {
		err = s.Builder.Undo(ctx, &cp)
	} else {
		err = s.Builder.Use(ctx, &cp)
	}
	if s.Log != nil {
		e := map[string]any{"at": s.now().UTC().Format(time.RFC3339), "thread": ref.Kind + ":" + ref.ID, "kind": "deploy", "undo": undo, "change": changeID}
		if err != nil {
			e["error"] = err.Error()
		}
		s.Log(e)
	}
	_, _ = s.update(ref, func(t *Thread) error {
		ch := t.Server.change(changeID)
		_, c := t.Card(cardID)
		if ch == nil || c == nil {
			return nil
		}
		ch.Commit = firstNonEmpty(cp.Commit, ch.Commit)
		if err != nil {
			back := StateReady
			if undo {
				back = StateLive
			}
			ch.State, c.State, c.Message = back, back, "That didn't work, so nothing changed. Try again in a little while."
			return nil
		}
		// Deployed; the restart is pending. Recover settles it.
		return nil
	})
	if err == nil && s.Restart != nil {
		time.Sleep(300 * time.Millisecond)
		s.Restart()
	}
}

// Recover settles what a restart interrupted: answers that were running fail
// politely (once, at start), and deploys are settled from the launcher's
// verdict — which may land a few seconds after start, so SettleDeploys is
// also polled until nothing is waiting.
func (s *Service) Recover() {
	for _, t := range s.Store.List() {
		_, _ = s.update(refOf(t), func(t *Thread) error {
			for i := range t.Turns {
				tu := &t.Turns[i]
				if tu.Who == "liber" && (tu.Status == StatusThinking || tu.Status == StatusWorking) {
					tu.Status = StatusFailed
					if tu.Text == "" {
						tu.Text = "Sorry — I was interrupted. Please send that again."
					}
					for j := range tu.Cards {
						if tu.Cards[j].State == StateBuilding {
							tu.Cards[j].State, tu.Cards[j].Summary = StateFailed, "I was interrupted before the change was ready."
						}
					}
				}
				tu.Queued = false
			}
			if t.Server != nil {
				for _, ch := range t.Server.Changes {
					if ch.State == StateBuilding {
						ch.State = StateFailed
					}
				}
			}
			return nil
		})
	}
	s.SettleDeploys()
}

// SettleDeploys marks changes that were being shipped as live, undone or
// failed once the launcher has said; it reports whether any are still waiting.
func (s *Service) SettleDeploys() bool {
	if s.Builder == nil {
		return false
	}
	waiting := false
	for _, t := range s.Store.List() {
		if t.Server == nil {
			continue
		}
		pending := false
		for _, ch := range t.Server.Changes {
			pending = pending || ch.State == StateDeploying
		}
		if !pending {
			continue
		}
		_, _ = s.update(refOf(t), func(t *Thread) error {
			for _, ch := range t.Server.Changes {
				if ch.State != StateDeploying {
					continue
				}
				_, c := t.Card(cardFor(t, ch.ID))
				undoing := !ch.LiveUntil.IsZero() // only a live change has an undo window
				switch s.Builder.Settled(ch) {
				case "live":
					if undoing {
						ch.State = StateUndone
						if c != nil {
							c.State, c.Message = StateUndone, "Put back the way it was."
						}
					} else {
						ch.State, ch.LiveUntil = StateLive, s.now().UTC().Add(7*24*time.Hour)
						if c != nil {
							c.State, c.Message = StateLive, "Live — refresh to see it."
						}
					}
				case "failed":
					if undoing {
						ch.State = StateLive
					} else {
						ch.State = StateReady
					}
					if c != nil {
						c.State, c.Message = ch.State, "That didn't work, so I put things back."
					}
				default:
					waiting = true
				}
			}
			return nil
		})
	}
	return waiting
}

// MarshalPublic is the JSON Olga's browser receives.
func MarshalPublic(t *Thread) ([]byte, error) { return json.Marshal(t.Public()) }

// recentImages: the photos she sent in the last few messages (for a build
// started from a card, so her screenshots reach the builder).
func (s *Service) recentImages(t *Thread) []string {
	var out []string
	for i := len(t.Turns) - 1; i >= 0 && i >= len(t.Turns)-8; i-- {
		for _, id := range t.Turns[i].Images {
			if p := s.Store.ImagePath(id, t.Kind == KindTask && t.Shared); p != "" && len(out) < MaxImagesPerMessage {
				out = append(out, p)
			}
		}
	}
	return out
}
