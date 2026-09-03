package realestate

import (
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"
)

// Transfer matching (owner ask 2026-09-03): money moving BETWEEN entities —
// OODA Fund I funding Garden SPE — shows up as two workbench rows, a
// withdrawal on one entity and a deposit on the other, and used to be handled
// by skipping both ("transfer" was only a dismiss reason). The matcher pairs
// them so one approval books both sides into their entity admin ledgers.
//
// The rules follow standard reconciliation practice (ordered, exact-first):
//   1. amounts equal to the cent, opposite direction, DIFFERENT entities,
//      both still in play (pending/assigned — applied and skipped never match)
//   2. dates within a settlement window (3 days)
//   3. a shared bank reference number in both descriptions upgrades the match
//      (a REF # is a trail to the source transaction, not a coincidental
//      total); a description naming the peer entity upgrades it too
//   4. one-to-one: strongest pairs claim first, a row pairs at most once
// Matches are SUGGESTIONS — nothing books until the owner approves the pair.

// TransferMatch rides on List copies (derived, never persisted — the
// MerchantKey pattern): the peer row this one appears to mirror.
type TransferMatch struct {
	PeerID     string `json:"peerId"`
	PeerEntity string `json:"peerEntity"`
	PeerDate   string `json:"peerDate"`
	Why        string `json:"why"` // human line: what matched
	Ref        bool   `json:"ref,omitempty"`
}

var refTokenRe = regexp.MustCompile(`\d{6,}`)

// refTokens pulls long digit runs (bank REF #s, confirmation numbers) from a
// row's text — account-mask fragments (****6356) are stripped first so a
// shared bank isn't mistaken for a shared reference.
func refTokens(text string) map[string]bool {
	out := map[string]bool{}
	cleaned := regexp.MustCompile(`\*+\d+`).ReplaceAllString(text, "")
	for _, tok := range refTokenRe.FindAllString(cleaned, -1) {
		out[tok] = true
	}
	return out
}

// dateDist is the |a-b| day distance for YYYY-MM-DD strings (a parse failure
// is "far", never a match).
func dateDist(a, b string) int {
	ta, errA := time.Parse("2006-01-02", strings.TrimSpace(a))
	tb, errB := time.Parse("2006-01-02", strings.TrimSpace(b))
	if errA != nil || errB != nil {
		return 99
	}
	d := int(ta.Sub(tb).Hours() / 24)
	if d < 0 {
		d = -d
	}
	return d
}

// MatchTransfers pairs in-play rows across entities. names maps entity slug →
// display name (for the description-mentions-peer signal and the Why line).
func MatchTransfers(rows []StatementRow, names map[string]string) map[string]TransferMatch {
	inPlay := func(r StatementRow) bool {
		return (r.State == "pending" || r.State == "assigned") && r.Entity != "" && r.Amount > 0
	}
	type cand struct {
		out, in  int // indices: expense side, deposit side
		dist     int
		ref      bool
		mentions bool
	}
	var cands []cand
	for i, a := range rows {
		if !inPlay(a) || a.Inflow {
			continue
		}
		aRefs := refTokens(a.Vendor + " " + a.Note)
		for j, b := range rows {
			if !inPlay(b) || !b.Inflow || b.Entity == a.Entity || b.Amount != a.Amount {
				continue
			}
			dist := dateDist(a.Date, b.Date)
			if dist > 3 {
				continue
			}
			c := cand{out: i, in: j, dist: dist}
			for tok := range refTokens(b.Vendor + " " + b.Note) {
				if aRefs[tok] {
					c.ref = true
					break
				}
			}
			text := strings.ToUpper(a.Vendor + " " + a.Note + " " + b.Vendor + " " + b.Note)
			for slug, name := range names {
				if slug == a.Entity || slug == b.Entity {
					if n := strings.ToUpper(strings.TrimSpace(name)); n != "" && strings.Contains(text, n) {
						c.mentions = true
						break
					}
				}
			}
			cands = append(cands, c)
		}
	}
	// strongest first: shared reference, then peer named, then closest dates
	sort.SliceStable(cands, func(x, y int) bool {
		a, b := cands[x], cands[y]
		if a.ref != b.ref {
			return a.ref
		}
		if a.mentions != b.mentions {
			return a.mentions
		}
		return a.dist < b.dist
	})
	taken := map[int]bool{}
	out := map[string]TransferMatch{}
	for _, c := range cands {
		if taken[c.out] || taken[c.in] {
			continue
		}
		taken[c.out], taken[c.in] = true, true
		a, b := rows[c.out], rows[c.in]
		label := func(slug string) string {
			if n := strings.TrimSpace(names[slug]); n != "" {
				return n
			}
			return slug
		}
		why := "same amount"
		switch {
		case c.ref:
			why = "same amount + shared bank REF"
		case c.mentions:
			why = "same amount + description names the peer"
		}
		if c.dist == 0 {
			why += ", same day"
		} else {
			why += ", " + strconv.Itoa(c.dist) + "d apart"
		}
		out[a.ID] = TransferMatch{PeerID: b.ID, PeerEntity: label(b.Entity), PeerDate: b.Date, Why: why, Ref: c.ref}
		out[b.ID] = TransferMatch{PeerID: a.ID, PeerEntity: label(a.Entity), PeerDate: a.Date, Why: why, Ref: c.ref}
	}
	return out
}
