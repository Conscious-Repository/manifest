package contacts

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

// MEETING MEMORY (2026-10-03). Who you met is a fact about the past; it does
// not stop being true when a calendar sign-in expires. Every successful pull
// is merged into a file under DataDir (derived data, never the vault), and a
// pull that comes back empty — a dead token, an outage, Google's 7-day test
// tokens — is answered from it. Before this, one expired sign-in erased every
// "last met" date and every meeting tie in the Network graph at once.

// meetingRetryTTL is how soon an empty pull is retried: an empty answer is
// most often a failure, and caching it for meetingCacheTTL kept the remembered
// meetings standing in for half an hour after a reconnect.
const meetingRetryTTL = 2 * time.Minute

type meetingMemory struct {
	Parties []MeetingParty `json:"parties"`
	// PulledAt is the last pull that returned meetings: what the memory is
	// current through.
	PulledAt time.Time `json:"pulledAt"`
}

func (s *Service) meetingMemoryPath() string {
	if s.store == nil || s.store.path == "" {
		return ""
	}
	return filepath.Join(filepath.Dir(s.store.path), "contacts-meetings.json")
}

func (s *Service) loadMeetingMemory() meetingMemory {
	var m meetingMemory
	p := s.meetingMemoryPath()
	if p == "" {
		return m
	}
	if b, err := os.ReadFile(p); err == nil {
		_ = json.Unmarshal(b, &m)
	}
	return m
}

func (s *Service) saveMeetingMemory(m meetingMemory) {
	p := s.meetingMemoryPath()
	if p == "" {
		return
	}
	b, err := json.Marshal(m)
	if err != nil {
		return
	}
	tmp := p + ".tmp"
	if os.WriteFile(tmp, b, 0o600) == nil {
		_ = os.Rename(tmp, p)
	}
}

func partyKey(p MeetingParty) string {
	emails := append([]string(nil), p.Emails...)
	sort.Strings(emails)
	return p.Date + "\x00" + p.Title + "\x00" + strings.Join(emails, ",")
}

// mergeParties is the union of a fresh pull and the memory, newest first,
// keeping only meetings inside the window (a meeting older than the window
// leaves the memory the way it leaves the pull).
func mergeParties(fresh, remembered []MeetingParty, oldest string) []MeetingParty {
	seen := map[string]bool{}
	var out []MeetingParty
	for _, list := range [][]MeetingParty{fresh, remembered} {
		for _, p := range list {
			if p.Date < oldest || len(p.Emails) == 0 {
				continue
			}
			k := partyKey(p)
			if seen[k] {
				continue
			}
			seen[k] = true
			out = append(out, p)
		}
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].Date > out[j].Date })
	return out
}

// MeetingsAsOf is when the meeting record was last refreshed from the
// calendar, and whether the latest pull came back empty (the dates shown are
// the remembered ones). Zero time: no pull has ever returned meetings.
func (s *Service) MeetingsAsOf() (pulledAt time.Time, remembered bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.meetings == nil {
		return time.Time{}, false
	}
	return s.meetings.pulledAt, s.meetings.remembered
}
