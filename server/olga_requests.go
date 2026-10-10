package server

import (
	"os"
	"path/filepath"
	"strconv"
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
