package server

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	"manifest/aion"
	"manifest/teamportal"
)

func liveFixture(t *testing.T) (*Server, *AionLive, *teamportal.Store, string) {
	t.Helper()
	vault, data := t.TempDir(), t.TempDir()
	root := filepath.Join(vault, "system", "aion")
	if err := os.MkdirAll(root, 0o755); err != nil {
		t.Fatal(err)
	}
	for name, seed := range aion.SeedFiles {
		if err := os.WriteFile(filepath.Join(root, name), []byte(seed), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	store := aion.NewStore(vault, "system/aion", func(path string, body []byte) error { return os.WriteFile(path, body, 0o644) })
	base := &aion.BacklogItem{Kind: aion.KindTask, Text: "Base task", Owner: "BA", Status: aion.StatusOpen}
	if err := store.AddItem(base); err != nil {
		t.Fatal(err)
	}
	srv := &Server{}
	srv.UseAion(store, "", "", "", data)
	team, err := teamportal.New(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	srv.UseTeamPortal(nil, team, "owner@aion.bio")
	return srv, srv.AionLive(), team, base.ID
}

func TestAionLiveProjectionAndOwnerResolution(t *testing.T) {
	_, live, team, baseID := liveFixture(t)
	now := time.Date(2026, 8, 17, 12, 0, 0, 0, time.UTC)
	member := teamportal.Identity{Email: "member@aion.bio", Name: "Member"}
	if _, err := team.Patch(member, baseID, map[string]string{"due": "2026-09-01", "status": "in_progress"}, now); err != nil {
		t.Fatal(err)
	}
	if _, err := team.AddComment(member, baseID, "working it", now); err != nil {
		t.Fatal(err)
	}
	added, err := team.AddItem(member, "MM", "task", "Team task", "", "", now)
	if err != nil {
		t.Fatal(err)
	}
	items := live.EffectiveItems()
	if len(items) != 2 {
		t.Fatalf("effective items = %+v", items)
	}
	var base, teamItem *AionEffectiveItem
	for i := range items {
		if items[i].ID == baseID {
			base = &items[i]
		}
		if items[i].ID == added.ID {
			teamItem = &items[i]
		}
	}
	if base == nil || base.Due == nil || *base.Due != "2026-09-01" || base.OverrideBy != member.Email || base.CommentCount != 1 {
		t.Fatalf("base projection = %+v", base)
	}
	if teamItem == nil || teamItem.SourceType != "team" || !teamItem.Team {
		t.Fatalf("team projection = %+v", teamItem)
	}
	if err := live.ownerUpdate(baseID, map[string]string{"due": "2026-10-01"}, now.Add(time.Minute)); err != nil {
		t.Fatal(err)
	}
	ov := team.Ext().Overrides[baseID]
	if _, ok := ov.Fields["due"]; ok || ov.Fields["status"] != "in_progress" {
		t.Fatalf("owner edit cleared wrong overlay keys: %+v", ov.Fields)
	}
	if got := live.ManifestItems(); len(got) != len(live.TeamState().EffectiveItems) {
		t.Fatalf("listeners do not share effective item count: %d vs %d", len(got), len(live.TeamState().EffectiveItems))
	}
	if err := live.ownerDelete(baseID, now.Add(2*time.Minute)); err != nil {
		t.Fatal(err)
	}
	for _, it := range live.EffectiveItems() {
		if it.ID == baseID {
			t.Fatal("archived collaborative item remained active")
		}
	}
	if len(team.Ext().Archives) != 1 || len(team.Ext().Comments[baseID]) != 1 {
		t.Fatalf("archive did not preserve snapshot/thread: %+v", team.Ext())
	}
}

func TestAionLiveInvalidSourceKeepsLastGoodAndSelfHeals(t *testing.T) {
	srv, live, _, baseID := liveFixture(t)
	good, rev, err := live.File("/data/backlog.json")
	if err != nil || len(good) == 0 {
		t.Fatalf("initial live file: rev=%q err=%v", rev, err)
	}
	if err := srv.aion.UpdateItem(baseID, map[string]string{"rock": "aion/missing-rock"}, time.Now()); err != nil {
		t.Fatal(err)
	}
	if err := live.refresh(true); err == nil || !live.Status().Stale {
		t.Fatalf("invalid source did not degrade: %v %+v", err, live.Status())
	}
	stale, staleRev, err := live.File("/data/backlog.json")
	if err != nil || staleRev != rev || string(stale) != string(good) {
		t.Fatal("last-known-good contract was not retained")
	}
	restarted := &Server{}
	restarted.UseAion(srv.aion, "", "", "", srv.aionDataDir)
	restartBody, restartRev, restartErr := restarted.AionLive().File("/data/backlog.json")
	if restartErr != nil || !restarted.AionLive().Status().Stale || restartRev != rev || string(restartBody) != string(good) {
		t.Fatalf("restart did not retain last good: rev=%q err=%v status=%+v", restartRev, restartErr, restarted.AionLive().Status())
	}
	if err := srv.aion.UpdateItem(baseID, map[string]string{"rock": ""}, time.Now()); err != nil {
		t.Fatal(err)
	}
	if err := live.refresh(true); err != nil || live.Status().Stale {
		t.Fatalf("live sync did not self-heal: %v %+v", err, live.Status())
	}
	if _, err := os.Stat(filepath.Join(srv.aionDataDir, "aion", "live-contract.json")); err != nil {
		t.Fatalf("last-good cache missing: %v", err)
	}
}

func TestAionCoordinatorReplaysJournalIdempotently(t *testing.T) {
	srv, live, team, baseID := liveFixture(t)
	now := time.Date(2026, 8, 17, 15, 0, 0, 0, time.UTC)
	member := teamportal.Identity{Email: "member@aion.bio", Name: "Member"}
	if _, err := team.Patch(member, baseID, map[string]string{"due": "2026-09-01"}, now); err != nil {
		t.Fatal(err)
	}
	intent := map[string]string{"due": "2026-10-01"}
	if err := srv.aion.UpdateItem(baseID, intent, now.Add(time.Minute)); err != nil {
		t.Fatal(err)
	}
	if err := live.writeJournal(aionSyncJournal{ID: "test-op", Kind: "update", Item: baseID, Set: intent, Stage: "vault"}); err != nil {
		t.Fatal(err)
	}
	if err := live.recoverJournal(); err != nil {
		t.Fatal(err)
	}
	if _, ok := team.Ext().Overrides[baseID]; ok {
		t.Fatal("recovery did not reconcile the overlay")
	}
	count := 0
	for _, e := range team.Activity(time.Time{}) {
		if e.Action == teamportal.ActOwnerResolve && e.Payload["item"] == baseID {
			count++
		}
	}
	if err := live.recoverJournal(); err != nil {
		t.Fatal(err)
	}
	countAfter := 0
	for _, e := range team.Activity(time.Time{}) {
		if e.Action == teamportal.ActOwnerResolve && e.Payload["item"] == baseID {
			countAfter++
		}
	}
	if count != 1 || countAfter != 1 {
		t.Fatalf("replay duplicated owner activity: before=%d after=%d", count, countAfter)
	}
}

func TestPortalLiveContractAndConditionalRevision(t *testing.T) {
	_, live, team, _ := liveFixture(t)
	h, err := PortalHandler(PortalOptions{Store: team, Live: live})
	if err != nil {
		t.Fatal(err)
	}
	metaReq := httptest.NewRequest(http.MethodGet, "/data/meta.json", nil)
	metaRec := httptest.NewRecorder()
	h.ServeHTTP(metaRec, metaReq)
	var meta map[string]any
	if metaRec.Code != http.StatusOK || json.Unmarshal(metaRec.Body.Bytes(), &meta) != nil || meta["contract"] != "2" {
		t.Fatalf("live meta = %d %s", metaRec.Code, metaRec.Body.String())
	}
	req := httptest.NewRequest(http.MethodGet, "/api/live/revision", nil)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	etag := rec.Header().Get("ETag")
	if rec.Code != http.StatusOK || etag == "" {
		t.Fatalf("revision = %d etag=%q", rec.Code, etag)
	}
	req2 := httptest.NewRequest(http.MethodGet, "/api/live/revision", nil)
	req2.Header.Set("If-None-Match", etag)
	rec2 := httptest.NewRecorder()
	h.ServeHTTP(rec2, req2)
	if rec2.Code != http.StatusNotModified {
		t.Fatalf("conditional revision = %d", rec2.Code)
	}
}
