package server

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"path/filepath"
	"time"
)

// Persist the board link before any request capable of creating a process. A
// restart at any launch boundary retains both the writer lane and the same ID.
func (s *Server) createBoardHerdrSession(kind, cwd, name, brief, model string) (termSession, error) {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		return termSession{}, err
	}
	now := time.Now().UTC().Format(time.RFC3339)
	se := termSession{ID: hex.EncodeToString(b[:8]), Version: terminalRowVersion, Backend: "herdr", Kind: kind, Cwd: cwd, Name: name, BoardBrief: brief, Model: model, CreatedAt: now, LastUsed: now, LaunchPhase: "intent"}
	if h := s.terminal.herdr; h != nil {
		se.Runtime = terminalIdentity{ManifestID: se.ID, Backend: "herdr", Host: h.Host, Session: h.Session}
	}
	if kind == "claude" {
		b[6] = b[6]&0x0f | 0x40
		b[8] = b[8]&0x3f | 0x80
		se.ResumeID = fmt.Sprintf("%x-%x-%x-%x-%x", b[:4], b[4:6], b[6:8], b[8:10], b[10:])
	}
	if err := s.terminal.upsertChecked(se); err != nil {
		return se, err
	}
	if err := boardWrite(filepath.Join(filepath.Dir(brief), "session"), []byte(se.ID)); err != nil {
		return se, err
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	return s.launchHerdr(ctx, se)
}
