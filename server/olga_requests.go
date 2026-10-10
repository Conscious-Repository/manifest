package server

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"manifest/approvals"
)

// Olga's requests for Benjamin (approvals.TypeOlgaRequest). Her own server
// appends them to system/olga/requests.md in the shared vault; this files each
// new entry into the primary Approvals inbox, keyed by the entry, so a card he
// has marked Done or Won't do never comes back. It runs when the feed or its
// badge is read, and only re-reads the file when it has changed.
var olgaRequestsSeen struct {
	sync.Mutex
	stamp string
}

func (s *Server) syncOlgaRequests() {
	if s.approvals == nil || s.vault == nil || !s.vault.Enabled() {
		return
	}
	path := filepath.Join(s.vault.VaultRoot(), "system", "olga", "requests.md")
	fi, err := os.Stat(path)
	if err != nil {
		return
	}
	stamp := path + "|" + fi.ModTime().Format(time.RFC3339Nano) + "|" + strconv.FormatInt(fi.Size(), 10)
	olgaRequestsSeen.Lock()
	defer olgaRequestsSeen.Unlock()
	if olgaRequestsSeen.stamp == stamp {
		return
	}
	b, err := os.ReadFile(path)
	if err != nil {
		return
	}
	for _, r := range approvals.ParseOlgaRequests(string(b)) {
		if _, err := s.approvals.ProposeOnce(approvals.OlgaRequestProposal(r)); err != nil {
			return // try again on the next read
		}
	}
	olgaRequestsSeen.stamp = stamp
}

// answerOlga hands Benjamin's decision on one of her requests back to her
// server, which posts it into the conversation the request came from. The
// answer file is written once: a repeated decision changes nothing.
func (s *Server) answerOlga(p approvals.Proposal, decision, note string) error {
	if s.vault == nil || !s.vault.Enabled() {
		return errors.New("vault unavailable")
	}
	root := s.vault.VaultRoot()
	b, _ := os.ReadFile(filepath.Join(root, "system", "olga", "requests.md"))
	req, ok := approvals.OlgaRequestByID(string(b), p.ID)
	if !ok {
		req = approvals.OlgaRequest{ID: p.ID, Asked: strings.TrimPrefix(p.Action, "Olga asked: ")}
	}
	a := approvals.OlgaAnswer{ID: p.ID, Thread: req.Thread, Asked: req.Asked, Decision: decision,
		Note: strings.TrimSpace(note), At: time.Now().UTC().Format(time.RFC3339)}
	js, err := json.MarshalIndent(a, "", " ")
	if err != nil {
		return err
	}
	return s.vault.UpdateCap("olga-answers", "system/olga/answers/"+p.ID+".json", func(before []byte) ([]byte, error) {
		if len(before) > 0 {
			return before, nil
		}
		return append(js, '\n'), nil
	})
}
