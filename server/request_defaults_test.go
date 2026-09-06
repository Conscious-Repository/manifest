package server

import (
	"encoding/json"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestTaskAddNamedDomain(t *testing.T) {
	for _, tc := range []struct{ name, body, prefix, wantError string }{
		{"Personal", `"container":{"kind":"domain","name":"Personal"}`, "personal/", ""},
		{"Aion", `"container":{"kind":"domain","name":"Aion"}`, "aion/", ""},
		{"Home", `"container":{"kind":"domain","name":"Home"}`, "home/", ""},
		{"Real Estate", `"container":{"kind":"domain","name":"Real Estate"}`, "real-estate/", ""},
		{"case and whitespace", `"container":{"kind":"domain","name":" personal "}`, "personal/", ""},
		{"matching selectors", `"domain":"Personal","container":{"kind":"domain","name":"personal"}`, "personal/", ""},
		{"legacy domain", `"domain":"Personal"`, "personal/", ""},
		{"legacy domain creation", `"domain":"New Domain"`, "new-domain/", ""},
		{"legacy inbox", `"domain":""`, "inbox/", ""},
		{"legacy container", `"domain":"Home","container":{"kind":"domain"}`, "home/", ""},
		{"unknown", `"container":{"kind":"domain","name":"bogus-xyz"}`, "", "unknown domain: bogus-xyz"},
		{"conflict", `"domain":"Home","container":{"kind":"domain","name":"Personal"}`, "", "domain and container.name must match"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			srv, vault := unifiedHarness(t)
			doc, err := srv.tasksStore.Load()
			if err != nil {
				t.Fatal(err)
			}
			for _, name := range []string{"Personal", "Aion", "Home"} {
				doc.EnsureDomain(name)
			}
			if err := srv.tasksStore.Save(doc); err != nil {
				t.Fatal(err)
			}
			path := filepath.Join(vault, "to do.md")
			before, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}
			rec := httptest.NewRecorder()
			srv.handleTaskAdd(rec, httptest.NewRequest("POST", "/api/tasks/item", strings.NewReader(`{"text":"routing probe",`+tc.body+`}`)))
			after, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}
			if tc.wantError != "" {
				if rec.Code != 400 || !strings.Contains(rec.Body.String(), tc.wantError) {
					t.Fatalf("got %d %s", rec.Code, rec.Body.String())
				}
				if string(before) != string(after) {
					t.Fatal("rejected request changed task file")
				}
				return
			}
			if rec.Code != 200 {
				t.Fatalf("got %d %s", rec.Code, rec.Body.String())
			}
			var response struct {
				Created string `json:"created"`
			}
			if err := json.Unmarshal(rec.Body.Bytes(), &response); err != nil {
				t.Fatal(err)
			}
			if !strings.HasPrefix(response.Created, tc.prefix) {
				t.Fatalf("created %q, want prefix %q", response.Created, tc.prefix)
			}
			fresh, err := srv.tasksStore.Load()
			if err != nil {
				t.Fatal(err)
			}
			count := 0
			for _, dom := range fresh.Domains {
				for _, task := range dom.Tasks {
					if task.Text == "routing probe" {
						count++
						if task.ID != response.Created {
							t.Fatalf("written task %q differs from response %q", task.ID, response.Created)
						}
					}
				}
			}
			if count != 1 {
				t.Fatalf("wrote %d copies", count)
			}
		})
	}
}

func TestTermCreateKind(t *testing.T) {
	for _, kind := range []string{"shell", "claude", "codex", "bogus-xyz", "", "Shell", " shell "} {
		t.Run(kind, func(t *testing.T) {
			cfg := &termCfg{regPath: filepath.Join(t.TempDir(), "terminals.json")}
			srv := &Server{terminal: cfg}
			body, err := json.Marshal(map[string]string{"kind": kind})
			if err != nil {
				t.Fatal(err)
			}
			rec := httptest.NewRecorder()
			srv.handleTermCreate(rec, httptest.NewRequest("POST", "/api/terminal/session", strings.NewReader(string(body))))
			valid := kind == "shell" || kind == "claude" || kind == "codex"
			if !valid {
				if rec.Code != 400 || !strings.Contains(rec.Body.String(), "kind must be one of shell|claude|codex") {
					t.Fatalf("got %d %s", rec.Code, rec.Body.String())
				}
				if _, err := os.Stat(cfg.regPath); !os.IsNotExist(err) {
					t.Fatalf("rejected request touched registry: %v", err)
				}
				return
			}
			if rec.Code != 200 {
				t.Fatalf("got %d %s", rec.Code, rec.Body.String())
			}
			var response termSession
			if err := json.Unmarshal(rec.Body.Bytes(), &response); err != nil {
				t.Fatal(err)
			}
			rows := cfg.load()
			if response.Kind != kind || response.ID == "" || len(rows) != 1 || rows[0].Kind != kind || rows[0].ID != response.ID {
				t.Fatalf("wrong session: response=%+v registry=%+v", response, rows)
			}
		})
	}
}
