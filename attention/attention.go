// Package attention is ARCHITECTURE §5 as code: the closed set of attention
// kinds the FEED renders, each a registered Source declaring its lifecycle.
// It generalizes the signals package's Emitter/Service shape — the cleanest of
// the three formerly hand-merged lanes — without owning any kind's card shape:
// a finding is an engine-authored markdown item, a signal a computed
// condition, a notice an externally-sourced portal card, a receipt an
// autonomous-errand trail. The feed handler iterates the registry instead of
// hand-merging fields; the proposals lane is NOT here by design (§5 amendment
// C3: approvals are an authorization queue rendered in FEED, not attention).
package attention

import (
	"net/url"
	"time"
)

// Lifecycle is a kind's declared verb set + clearing policy (§5). The verbs
// live with the kind's own store/handlers; the declaration is the contract.
type Lifecycle string

const (
	// LifecycleKeptDiscarded: findings — kept/discarded/snoozed verdicts,
	// written to item frontmatter; the tune ritual reads them as quality signal.
	LifecycleKeptDiscarded Lifecycle = "kept-discarded"
	// LifecycleDismissSnoozeAutoclear: signals — dismiss re-arms on hash
	// change, snooze lapses, and the card clears ITSELF when the condition
	// resolves. Signals may carry domain quick-actions (amendment C4).
	LifecycleDismissSnoozeAutoclear Lifecycle = "dismiss-snooze-autoclear"
	// LifecycleDismissExpire: notices — external items that can be dismissed
	// and age out; the external system is the source of truth.
	LifecycleDismissExpire Lifecycle = "dismiss-expire"
	// LifecyclePermanent: receipts — an append-only trail of autonomous work;
	// never dismissed, only read.
	LifecyclePermanent Lifecycle = "permanent"
)

// Card is one attention card. Each kind keeps its own card shape (they render
// differently by design); the taxonomy is about registration, not structure.
type Card any

// Source is one registered attention kind. Active returns the cards to render
// (q carries the client's view filters — kinds that don't filter ignore it);
// Count is the kind's badge contribution, which may be narrower than Active
// (findings count only the inbox slice).
type Source interface {
	Kind() string
	Lifecycle() Lifecycle
	Active(now time.Time, q url.Values) []Card
	Count(now time.Time) int
}

// Registry is the ordered set of attention sources the feed iterates.
type Registry struct {
	sources []Source
}

// Register appends a source (construction-time; order = response order).
func (r *Registry) Register(s Source) { r.sources = append(r.sources, s) }

// Sources returns the registered kinds in order.
func (r *Registry) Sources() []Source { return r.sources }

// Badge sums every kind's badge contribution.
func (r *Registry) Badge(now time.Time) int {
	n := 0
	for _, s := range r.sources {
		n += s.Count(now)
	}
	return n
}

// EmptySource declares a kind that exists in the taxonomy with no machinery
// behind it yet — the receipts slot (§5: declared, EMPTY; building the errand
// loop is a feature pass, not this one).
type EmptySource struct {
	K string
	L Lifecycle
}

func (e EmptySource) Kind() string                            { return e.K }
func (e EmptySource) Lifecycle() Lifecycle                    { return e.L }
func (e EmptySource) Active(time.Time, url.Values) []Card     { return []Card{} }
func (e EmptySource) Count(time.Time) int                     { return 0 }
