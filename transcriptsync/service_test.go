package transcriptsync

import (
	"context"
	"database/sql"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"manifest/approvals"
	_ "modernc.org/sqlite"
)

func fixtureService(t *testing.T, source string, h http.HandlerFunc) (*Service, *approvals.Store) {
	t.Helper()
	db, e := sql.Open("sqlite", ":memory:")
	if e != nil {
		t.Fatal(e)
	}
	db.SetMaxOpenConns(1)
	t.Cleanup(func() { db.Close() })
	for _, q := range []string{"CREATE TABLE notes(path TEXT,name TEXT,date TEXT,granola_id TEXT,pocket_id TEXT)", "CREATE TABLE entities(key TEXT,display TEXT,is_person INT)", "CREATE TABLE note_aliases(path TEXT,alias_lower TEXT)"} {
		if _, e := db.Exec(q); e != nil {
			t.Fatal(e)
		}
	}
	dir := t.TempDir()
	ap := approvals.NewStore(filepath.Join(dir, "artifacts"))
	cfg := Config{Granola: SourceConfig{Account: "fixture"}, Pocket: SourceConfig{Account: "fixture"}}
	s := New(dir, cfg, NewIndex(db), ap)
	s.now = func() time.Time { return time.Date(2026, 9, 16, 12, 0, 0, 0, time.UTC) }
	legacy := t.TempDir()
	path := filepath.Join(legacy, "vessel", "state", source, "watermark")
	os.MkdirAll(filepath.Dir(path), 0700)
	os.WriteFile(path, []byte("2026-09-10T00:00:00Z\n"), 0600)
	if _, e := s.Import(source, legacy, "", false); e != nil {
		t.Fatal(e)
	}
	if _, e := os.Stat(s.statePath(source)); !os.IsNotExist(e) {
		t.Fatal("preview wrote state")
	}
	ritual := filepath.Join(legacy, "spirits", "ea-coordinator", "rituals", source+"-sync.md")
	os.MkdirAll(filepath.Dir(ritual), 0700)
	os.WriteFile(ritual, []byte("---\nenabled: false\npaused_reason: fixture handoff\n---\n"), 0600)
	if _, e := s.Import(source, legacy, "fixture-key", true); e != nil {
		t.Fatal(e)
	}
	if source == "granola" {
		s.cfg.Granola.Enabled = true
	} else {
		s.cfg.Pocket.Enabled = true
	}
	server := httptest.NewServer(h)
	t.Cleanup(server.Close)
	s.granola = func(key string) *GranolaClient {
		return &GranolaClient{&transport{key: key, base: server.URL, client: server.Client()}}
	}
	s.pocket = func(key string) *PocketClient {
		return &PocketClient{&transport{key: key, base: server.URL, client: server.Client()}}
	}
	return s, ap
}
func granolaFixture(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path == "/notes" {
		w.Write([]byte(`{"notes":[{"id":"note_one","title":"A meeting","created_at":"2026-09-12T12:00:00Z"}],"hasMore":false}`))
		return
	}
	w.Write([]byte(`{"id":"note_one","title":"A meeting","created_at":"2026-09-12T12:00:00Z","transcript":[{"speaker":{"name":"Jane"},"text":"I will review the draft."}]}`))
}
func TestPollRecoveryKeepsDecisions(t *testing.T) {
	s, ap := fixtureService(t, "granola", granolaFixture)
	initial, _ := s.read("granola")
	st, e := s.Poll(context.Background(), "granola")
	if e != nil || st.Filed != 1 {
		t.Fatalf("first: %+v %v", st, e)
	}
	p := ap.List("pending")[0]
	if !strings.Contains(p.Proposed, "granola-id: note_one") {
		t.Fatal(p)
	}
	if e := ap.Reject(p.ID, "reviewed"); e != nil {
		t.Fatal(e)
	}
	// Simulate lost state after a successful publication/decision, not a replay of
	// the user action. Source identity must recover the original legacy ID.
	if e := s.save("granola", initial); e != nil {
		t.Fatal(e)
	}
	st, e = s.Poll(context.Background(), "granola")
	if e != nil || st.Filed != 0 || st.Items["note_one"].ProposalID != p.ID || len(ap.List("pending")) != 0 {
		t.Fatalf("recovery: %+v %v", st, e)
	}
}
func TestIncompletePaginationDoesNotPublishOrAdvance(t *testing.T) {
	for _, mode := range []string{"missing-cursor", "second-page"} {
		t.Run(mode, func(t *testing.T) {
			s, ap := fixtureService(t, "granola", func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Query().Get("cursor") != "" {
					w.WriteHeader(403)
					w.Write([]byte("PRIVATE RESPONSE"))
					return
				}
				cursor := ""
				if mode == "second-page" {
					cursor = "next"
				}
				json.NewEncoder(w).Encode(map[string]any{"notes": []any{map[string]string{"id": "note_one", "created_at": "2026-09-12T12:00:00Z"}}, "hasMore": true, "cursor": cursor})
			})
			old, _ := s.read("granola")
			st, e := s.Poll(context.Background(), "granola")
			if e == nil || st.Watermark != old.Watermark || len(ap.List("pending")) != 0 || strings.Contains(e.Error(), "PRIVATE") {
				t.Fatalf("incomplete: %+v %v", st, e)
			}
		})
	}
}
func TestPocketUnfinishedHoldsCheckpointAndLaterArrives(t *testing.T) {
	ready := false
	s, ap := fixtureService(t, "pocket", func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/public/recordings" {
			state := "pending"
			if ready {
				state = "completed"
			}
			json.NewEncoder(w).Encode(map[string]any{"success": true, "data": []any{map[string]string{"id": "rec_old", "title": "Old", "state": state, "recording_at": "2026-09-11T12:00:00Z"}, map[string]string{"id": "rec_new", "title": "New", "state": "completed", "recording_at": "2026-09-13T12:00:00Z"}}, "pagination": map[string]bool{"has_more": false}})
			return
		}
		id := strings.TrimPrefix(r.URL.Path, "/public/recordings/")
		json.NewEncoder(w).Encode(map[string]any{"success": true, "data": map[string]any{"id": id, "title": id, "transcript": map[string]any{"segments": []any{map[string]string{"speaker": "Jane", "text": "I will do the review."}}}}})
	})
	old, _ := s.read("pocket")
	st, e := s.Poll(context.Background(), "pocket")
	if e != nil || st.Waiting != 1 || st.Watermark != old.Watermark || st.Filed != 1 {
		t.Fatalf("waiting: %+v %v", st, e)
	}
	ready = true
	st, e = s.Poll(context.Background(), "pocket")
	if e != nil || st.Waiting != 0 || !st.Watermark.After(old.Watermark) || len(ap.List("pending")) != 2 {
		t.Fatalf("ready: %+v %v", st, e)
	}
}
func TestImportDryRunAccountAndCorruption(t *testing.T) {
	s, _ := fixtureService(t, "granola", granolaFixture)
	s.cfg.Granola.Account = "another-account"
	if _, e := s.Poll(context.Background(), "granola"); e == nil {
		t.Fatal("account drift accepted")
	}
	s.cfg.Granola.Account = "fixture"
	os.WriteFile(s.statePath("granola"), []byte("{broken"), 0600)
	if _, e := s.Poll(context.Background(), "granola"); e == nil {
		t.Fatal("corrupt state silently reset")
	}
}
func TestScheduleChicagoAndDST(t *testing.T) {
	for _, date := range []string{"2026-03-08", "2026-11-01"} {
		local, _ := time.ParseInLocation("2006-01-02 15:04", date+" 09:00", chicago)
		if !due("pocket", local, local.Add(-time.Minute)) || due("pocket", local.Add(time.Minute), local) {
			t.Fatal("slot repeat/miss", date)
		}
		if due("granola", local, local.Add(-time.Minute)) {
			t.Fatal("granola fired on pocket slot")
		}
	}
}

func TestPollConflictingDuplicateIdentity(t *testing.T) {
	for _, source := range []string{"granola", "pocket"} {
		t.Run(source, func(t *testing.T) {
			calls := 0
			s, ap := fixtureService(t, source, func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path == "/notes" {
					w.Write([]byte(`{"notes":[{"id":"same","title":"Meeting","created_at":"2026-09-12T12:00:00Z"},{"id":"same","title":"Meeting","created_at":"2026-09-12T12:00:00Z"}],"hasMore":false}`))
					return
				}
				if r.URL.Path == "/public/recordings" {
					w.Write([]byte(`{"success":true,"data":[{"id":"same","title":"Meeting","state":"completed","recording_at":"2026-09-12T12:00:00Z"},{"id":"same","title":"Meeting","state":"completed","recording_at":"2026-09-12T12:00:00Z"}],"pagination":{"has_more":false}}`))
					return
				}
				calls++
				text := "Original transcript"
				if calls > 1 {
					text = "Conflicting transcript"
				}
				if source == "granola" {
					json.NewEncoder(w).Encode(map[string]any{"id": "same", "transcript": []any{map[string]any{"text": text}}})
				} else {
					json.NewEncoder(w).Encode(map[string]any{"success": true, "data": map[string]any{"id": "same", "transcript": map[string]any{"segments": []any{map[string]string{"text": text}}}}})
				}
			})
			old, _ := s.read(source)
			st, err := s.Poll(context.Background(), source)
			if err == nil || len(ap.List("pending")) != 0 || !st.Watermark.Equal(old.Watermark) {
				t.Fatalf("conflicting duplicate silently accepted: %+v %v", st, err)
			}
		})
	}
}

func TestPollInputEdgesAndRetry(t *testing.T) {
	for _, mode := range []string{"empty", "missing-title", "duplicate", "malformed", "disabled"} {
		t.Run(mode, func(t *testing.T) {
			calls := 0
			s, ap := fixtureService(t, "granola", func(w http.ResponseWriter, r *http.Request) {
				calls++
				if mode == "malformed" {
					w.Write([]byte(`{"notes":`))
					return
				}
				if r.URL.Path == "/notes" {
					item := map[string]string{"id": "edge", "created_at": "2026-09-12T12:00:00Z"}
					items := []any{item}
					if mode == "duplicate" {
						items = append(items, item)
					}
					json.NewEncoder(w).Encode(map[string]any{"notes": items, "hasMore": false})
					return
				}
				text := "Body retained"
				if mode == "empty" {
					text = " \n "
				}
				json.NewEncoder(w).Encode(map[string]any{"id": "edge", "transcript": []any{map[string]any{"text": text, "speaker": map[string]string{"name": "Jane"}}}})
			})
			if mode == "disabled" {
				s.cfg.Granola.Enabled = false
			}
			before, _ := s.read("granola")
			st, err := s.Poll(context.Background(), "granola")
			switch mode {
			case "disabled":
				if err == nil || calls != 0 {
					t.Fatal("disabled poll called source", calls, err)
				}
			case "malformed":
				if err == nil || !st.Watermark.Equal(before.Watermark) {
					t.Fatal(st, err)
				}
			case "empty":
				if err != nil || st.Waiting != 1 || !st.Watermark.Equal(before.Watermark) {
					t.Fatal(st, err)
				}
			default:
				if err != nil || st.Filed != 1 {
					t.Fatal(st, err)
				}
				pending := ap.List("pending")
				if len(pending) != 1 || pending[0].ApplyPath != "2026-09-12 untitled.md" || !strings.Contains(pending[0].Proposed, "[[Jane]]") || !strings.Contains(pending[0].Proposed, "Body retained") {
					t.Fatal(pending)
				}
				if st, err = s.Poll(context.Background(), "granola"); err != nil || st.Filed != 0 || len(ap.List("pending")) != 1 {
					t.Fatal(st, err)
				}
				return
			}
			if len(ap.List("pending")) != 0 {
				t.Fatal("invalid/disabled input filed proposal")
			}
		})
	}
}

func TestImportRefusesUnpausedLegacyWithoutWriting(t *testing.T) {
	for _, source := range []string{"granola", "pocket"} {
		for _, metadata := range []string{"", "---\nenabled: true\n---\n", "---\nenabled: false\n---\n"} {
			t.Run(source+metadata, func(t *testing.T) {
				dir, legacy := t.TempDir(), t.TempDir()
				svc := New(dir, Config{Granola: SourceConfig{Account: "fixture"}, Pocket: SourceConfig{Account: "fixture"}}, nil, nil)
				watermark := filepath.Join(legacy, "vessel", "state", source, "watermark")
				os.MkdirAll(filepath.Dir(watermark), 0700)
				original := []byte("2026-09-10T00:00:00Z\n")
				os.WriteFile(watermark, original, 0600)
				if metadata != "" {
					ritual := filepath.Join(legacy, "spirits", "ea-coordinator", "rituals", source+"-sync.md")
					os.MkdirAll(filepath.Dir(ritual), 0700)
					os.WriteFile(ritual, []byte(metadata), 0600)
				}
				if _, err := svc.Import(source, legacy, "", false); err != nil {
					t.Fatal("preview refused", err)
				}
				if _, err := svc.Import(source, legacy, "fixture-key", true); err == nil {
					t.Fatal("unsafe import accepted")
				}
				entries, err := os.ReadDir(dir)
				if err != nil || len(entries) != 0 {
					t.Fatal("refusal wrote successor state or key")
				}
				after, _ := os.ReadFile(watermark)
				if string(after) != string(original) {
					t.Fatal("legacy checkpoint changed")
				}
			})
		}
	}
}
