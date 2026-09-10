package gmailsend

// Sender accounts BY DOMAIN (owner decision 2026-09-10). Manifest wears more
// than one hat — recruiting mail goes out as ben@aion.bio, OODA and personal
// correspondence each go out from their OWN account — and the account is
// chosen by the correspondence domain the route is acting for (aion.bio,
// ooda.group, …), never by guessing. There is NO fallback: a domain without
// a registered sender fails loud (ErrNoSender) before any network call, so
// OODA or personal mail can never quietly leave from the recruiting account.
//
// Config shape (main.go builds the registry; nothing here reads config):
//
//	GMAIL_SEND_FROM                the recruiting sender (default ben@aion.bio);
//	                               token at TokenPath(dataDir) — unchanged.
//	GMAIL_SEND_SENDERS             extra senders, "domain=from" pairs separated
//	                               by commas or whitespace; a bare "from" maps
//	                               its own domain. Example:
//	                               "ooda.group=ben@ooda.group me@example.com"
//	config.json "mailSenders"      the same mapping as a JSON object:
//	                               {"ooda.group": "ben@ooda.group"}
//
// Every extra sender's token is its own file, SenderTokenPath(dataDir, from)
// = <dataDir>/gmail-send/<from>.json (dir 0700, file 0600), minted through
// the same paste-back flow at gmail.send and revocable on its own.

import (
	"context"
	"errors"
	"fmt"
	"net/mail"
	"path/filepath"
	"sort"
	"strings"
	"sync"
)

// ErrNoSender: the domain (or explicit From) has no registered sender
// account. Raised before any network call; nothing is sent from any other
// account in its place.
var ErrNoSender = errors.New("gmailsend: no sender account is configured for this domain — add it to mailSenders (config.json) or GMAIL_SEND_SENDERS; mail is never sent from another domain's account")

// Domain is the lowercased domain part of an address ("" when malformed).
func Domain(addr string) string {
	addr = strings.ToLower(strings.TrimSpace(addr))
	i := strings.LastIndex(addr, "@")
	if i <= 0 || i == len(addr)-1 {
		return ""
	}
	return addr[i+1:]
}

// SenderTokenPath is the per-sender token file for a registry sender:
// <dataDir>/gmail-send/<from>.json. Distinct from TokenPath (the recruiting
// sender's legacy token.json) so connecting a second account never overwrites
// the first. The address is lowercased and reduced to [a-z0-9@._-].
func SenderTokenPath(dataDir, from string) string {
	from = strings.ToLower(strings.TrimSpace(from))
	var b strings.Builder
	for _, r := range from {
		switch {
		case r >= 'a' && r <= 'z', r >= '0' && r <= '9', r == '@', r == '.', r == '_', r == '-':
			b.WriteRune(r)
		default:
			b.WriteByte('_')
		}
	}
	return filepath.Join(dataDir, "gmail-send", b.String()+".json")
}

// SenderSpec is one domain → From mapping.
type SenderSpec struct {
	Domain string
	From   string
}

// ParseSenders reads the GMAIL_SEND_SENDERS shape: "domain=from" pairs
// separated by commas or whitespace; a bare address maps its own domain.
// A malformed entry is an error (a typo must not silently drop a sender).
func ParseSenders(s string) ([]SenderSpec, error) {
	var out []SenderSpec
	for _, f := range strings.FieldsFunc(s, func(r rune) bool { return r == ',' || r == ' ' || r == '\t' || r == '\n' || r == ';' }) {
		f = strings.TrimSpace(f)
		if f == "" {
			continue
		}
		domain, from := "", f
		if i := strings.Index(f, "="); i >= 0 {
			domain, from = f[:i], f[i+1:]
		}
		sp, err := NewSenderSpec(domain, from)
		if err != nil {
			return nil, err
		}
		out = append(out, sp)
	}
	return out, nil
}

// NewSenderSpec validates one mapping. An empty domain is the From's own.
func NewSenderSpec(domain, from string) (SenderSpec, error) {
	from = strings.ToLower(strings.TrimSpace(from))
	if a, err := mail.ParseAddress(from); err != nil || a.Address != from || Domain(from) == "" {
		return SenderSpec{}, fmt.Errorf("gmailsend: sender %q is not a bare email address", from)
	}
	domain = strings.ToLower(strings.TrimSpace(strings.TrimPrefix(domain, "@")))
	if domain == "" {
		domain = Domain(from)
	}
	if strings.ContainsAny(domain, "@/ \t") {
		return SenderSpec{}, fmt.Errorf("gmailsend: %q is not a domain", domain)
	}
	return SenderSpec{Domain: domain, From: from}, nil
}

// Registry maps correspondence domains to their sender clients. Each client
// is still the single-sender Client (one From, one token, one lock); the
// registry only chooses WHICH one a route sends through — and refuses when
// there is none.
type Registry struct {
	mu       sync.RWMutex
	byDomain map[string]*Client
	byFrom   map[string]string // from → domain
}

// NewRegistry builds an empty registry.
func NewRegistry() *Registry {
	return &Registry{byDomain: map[string]*Client{}, byFrom: map[string]string{}}
}

// Register maps domain → c. An empty domain is the sender's own. Re-registering
// the same client for the same domain is a no-op; a different client for an
// already-mapped domain (or a sender already mapped to another domain) is an
// error, so two accounts can never contend for one domain.
func (r *Registry) Register(domain string, c *Client) error {
	if c == nil {
		return errors.New("gmailsend: nil sender client")
	}
	sp, err := NewSenderSpec(domain, c.Sender())
	if err != nil {
		return err
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if have, ok := r.byDomain[sp.Domain]; ok {
		if have == c {
			return nil
		}
		return fmt.Errorf("gmailsend: domain %s already sends as %s (refusing %s)", sp.Domain, have.Sender(), c.Sender())
	}
	if d, ok := r.byFrom[sp.From]; ok {
		return fmt.Errorf("gmailsend: sender %s already serves domain %s (refusing %s)", sp.From, d, sp.Domain)
	}
	r.byDomain[sp.Domain] = c
	r.byFrom[sp.From] = sp.Domain
	return nil
}

// Domains lists the mapped domains, sorted.
func (r *Registry) Domains() []string {
	r.mu.RLock()
	defer r.mu.RUnlock()
	out := make([]string, 0, len(r.byDomain))
	for d := range r.byDomain {
		out = append(out, d)
	}
	sort.Strings(out)
	return out
}

// ForDomain is the sender for a correspondence domain, or ErrNoSender.
func (r *Registry) ForDomain(domain string) (*Client, error) {
	domain = strings.ToLower(strings.TrimSpace(strings.TrimPrefix(domain, "@")))
	if domain == "" {
		return nil, fmt.Errorf("%w (no domain named)", ErrNoSender)
	}
	r.mu.RLock()
	c, ok := r.byDomain[domain]
	r.mu.RUnlock()
	if !ok {
		return nil, fmt.Errorf("%w (domain %s)", ErrNoSender, domain)
	}
	return c, nil
}

// ForFrom is the sender registered for an exact From address, or ErrNoSender.
// Sharing a domain with a registered sender is not enough: only the account
// itself sends.
func (r *Registry) ForFrom(from string) (*Client, error) {
	from = strings.ToLower(strings.TrimSpace(from))
	r.mu.RLock()
	d, ok := r.byFrom[from]
	c := r.byDomain[d]
	r.mu.RUnlock()
	if !ok || c == nil {
		return nil, fmt.Errorf("%w (From %s)", ErrNoSender, from)
	}
	return c, nil
}

// Resolve picks the client for a send. With msg.From set, that exact account
// must be registered, and when domain is also given it must be that account's
// domain (ErrSenderMismatch otherwise). With msg.From empty, the domain's
// sender is chosen and stamped into msg.From. Neither path ever substitutes
// another account.
func (r *Registry) Resolve(domain string, msg Message) (*Client, Message, error) {
	domain = strings.ToLower(strings.TrimSpace(strings.TrimPrefix(domain, "@")))
	from := strings.ToLower(strings.TrimSpace(msg.From))
	if from == "" {
		c, err := r.ForDomain(domain)
		if err != nil {
			return nil, msg, err
		}
		msg.From = c.Sender()
		return c, msg, nil
	}
	c, err := r.ForFrom(from)
	if err != nil {
		return nil, msg, err
	}
	if domain != "" {
		r.mu.RLock()
		d := r.byFrom[from]
		r.mu.RUnlock()
		if d != domain {
			return nil, msg, fmt.Errorf("%w: From %s serves %s, not %s", ErrSenderMismatch, from, d, domain)
		}
	}
	msg.From = from
	return c, msg, nil
}

// Send resolves the sender for domain (or the message's explicit From) and
// sends through it. Doctrine unchanged: a route calls this after the owner
// approved exactly these bytes; an unmapped domain is ErrNoSender and nothing
// leaves — least of all from the recruiting account.
func (r *Registry) Send(ctx context.Context, domain string, msg Message) (Ref, error) {
	c, msg, err := r.Resolve(domain, msg)
	if err != nil {
		return Ref{}, err
	}
	return c.Send(ctx, msg)
}

// DomainState is one registry entry's probe: the domain plus the sender's
// State (never token material).
type DomainState struct {
	Domain string `json:"domain"`
	State
}

// Statuses is the offline probe for every registered sender, sorted by domain.
func (r *Registry) Statuses() []DomainState {
	out := []DomainState{}
	for _, d := range r.Domains() {
		c, err := r.ForDomain(d)
		if err != nil {
			continue
		}
		out = append(out, DomainState{Domain: d, State: c.Status()})
	}
	return out
}
