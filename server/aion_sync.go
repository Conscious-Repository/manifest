package server

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"time"

	"manifest/aion"
	"manifest/teamportal"
)

type aionSyncJournal struct {
	ID       string                  `json:"id"`
	Kind     string                  `json:"kind"`
	Item     string                  `json:"item"`
	Set      map[string]string       `json:"set,omitempty"`
	Snapshot teamportal.ArchivedItem `json:"snapshot,omitempty"`
	Stage    string                  `json:"stage"`
}

func (l *AionLive) journalPath() string {
	return filepath.Join(l.s.aionDataDir, "aion", "sync-pending.json")
}

func (l *AionLive) readJournal() (aionSyncJournal, bool) {
	b, err := os.ReadFile(l.journalPath())
	if err != nil {
		return aionSyncJournal{}, false
	}
	var j aionSyncJournal
	if json.Unmarshal(b, &j) != nil || j.Item == "" {
		return aionSyncJournal{}, false
	}
	return j, true
}

func (l *AionLive) writeJournal(j aionSyncJournal) error {
	path := l.journalPath()
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	b, err := json.MarshalIndent(j, "", "  ")
	if err != nil {
		return err
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, b, 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}

func (l *AionLive) clearJournal() { _ = os.Remove(l.journalPath()) }

func collaborativeKeys(set map[string]string) []string {
	var out []string
	for key := range set {
		key = strings.ToLower(strings.TrimSpace(key))
		if teamportal.PatchFields[key] || key == "done_on" {
			out = append(out, key)
		}
	}
	return out
}

func (l *AionLive) ownerUpdate(itemID string, set map[string]string, now time.Time) error {
	l.opMu.Lock()
	defer l.opMu.Unlock()
	team, actor := l.teamStore()
	if strings.HasPrefix(itemID, "team/") {
		if team == nil {
			return errors.New("Aion team store is not configured")
		}
		_, err := team.OwnerPatchTeamItem(actor, itemID, set, now)
		return err
	}
	j := aionSyncJournal{ID: "sync-" + now.Format("20060102T150405.000000000"), Kind: "update", Item: itemID, Set: set, Stage: "planned"}
	if err := l.writeJournal(j); err != nil {
		return err
	}
	if err := l.s.aion.UpdateItem(itemID, set, now); err != nil {
		l.clearJournal()
		return err
	}
	_ = l.refresh(true)
	j.Stage = "vault"
	_ = l.writeJournal(j)
	if team != nil {
		if err := team.OwnerResolve(actor, itemID, collaborativeKeys(set), now); err != nil {
			return err
		}
	}
	l.clearJournal()
	return nil
}

func (l *AionLive) ownerDecide(itemID, outcome string, now time.Time) error {
	set := map[string]string{"status": aion.StatusDecided, "outcome": strings.TrimSpace(outcome), "decided": now.Format("2006-01-02")}
	if strings.HasPrefix(itemID, "team/") {
		return l.ownerUpdate(itemID, set, now)
	}
	l.opMu.Lock()
	defer l.opMu.Unlock()
	team, actor := l.teamStore()
	j := aionSyncJournal{ID: "sync-" + now.Format("20060102T150405.000000000"), Kind: "decide", Item: itemID, Set: set, Stage: "planned"}
	if err := l.writeJournal(j); err != nil {
		return err
	}
	if err := l.s.aion.Decide(itemID, outcome, now); err != nil {
		l.clearJournal()
		return err
	}
	_ = l.refresh(true)
	j.Stage = "vault"
	_ = l.writeJournal(j)
	if team != nil {
		if err := team.OwnerResolve(actor, itemID, []string{"status", "outcome"}, now); err != nil {
			return err
		}
	}
	l.clearJournal()
	return nil
}

func archiveSnapshot(it AionEffectiveItem) teamportal.ArchivedItem {
	val := func(p *string) string {
		if p == nil {
			return ""
		}
		return *p
	}
	return teamportal.ArchivedItem{
		ID: it.ID, Kind: it.Kind, Title: it.Title, Owner: val(it.Owner), Captured: it.Captured,
		Rock: val(it.Rock), Due: val(it.Due), Status: val(it.Status), DoneOn: val(it.DoneOn),
		NeededBy: val(it.NeededBy), Decided: val(it.Decided), Outcome: val(it.Outcome),
		Team: it.Team, AddedBy: it.AddedBy,
	}
}

func (l *AionLive) ownerDelete(itemID string, now time.Time) error {
	l.opMu.Lock()
	defer l.opMu.Unlock()
	team, actor := l.teamStore()
	var found *AionEffectiveItem
	for _, it := range l.effectiveItems() {
		if it.ID == itemID {
			copy := it
			found = &copy
			break
		}
	}
	if found == nil {
		return errors.New("item not found")
	}
	snap := archiveSnapshot(*found)
	if strings.HasPrefix(itemID, "team/") {
		if team == nil {
			return errors.New("Aion team store is not configured")
		}
		return team.Archive(actor, snap, now)
	}
	if team == nil || !team.HasCollaboration(itemID) {
		err := l.s.aion.DeleteItem(itemID)
		if err == nil {
			_ = l.refresh(true)
		}
		return err
	}
	j := aionSyncJournal{ID: "sync-" + now.Format("20060102T150405.000000000"), Kind: "archive", Item: itemID, Snapshot: snap, Stage: "planned"}
	if err := l.writeJournal(j); err != nil {
		return err
	}
	if err := l.s.aion.DeleteItem(itemID); err != nil {
		l.clearJournal()
		return err
	}
	_ = l.refresh(true)
	j.Stage = "vault"
	_ = l.writeJournal(j)
	if err := team.Archive(actor, snap, now); err != nil {
		return err
	}
	l.clearJournal()
	return nil
}

func (l *AionLive) recoverJournal() error {
	l.opMu.Lock()
	defer l.opMu.Unlock()
	j, ok := l.readJournal()
	if !ok {
		return nil
	}
	team, actor := l.teamStore()
	if team == nil {
		return nil
	}
	now := time.Now()
	switch j.Kind {
	case "update":
		if j.Stage != "vault" {
			if err := l.s.aion.UpdateItem(j.Item, j.Set, now); err != nil {
				return err
			}
		}
		_ = l.refresh(true)
		if err := team.OwnerResolve(actor, j.Item, collaborativeKeys(j.Set), now); err != nil {
			return err
		}
	case "decide":
		if j.Stage != "vault" {
			if err := l.s.aion.Decide(j.Item, j.Set["outcome"], now); err != nil && !strings.Contains(err.Error(), "already decided") {
				return err
			}
		}
		_ = l.refresh(true)
		if err := team.OwnerResolve(actor, j.Item, []string{"status", "outcome"}, now); err != nil {
			return err
		}
	case "archive":
		if j.Stage != "vault" {
			if err := l.s.aion.DeleteItem(j.Item); err != nil && !strings.Contains(err.Error(), "not found") {
				return err
			}
		}
		_ = l.refresh(true)
		if err := team.Archive(actor, j.Snapshot, now); err != nil {
			return err
		}
	}
	l.clearJournal()
	return nil
}
