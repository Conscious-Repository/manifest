package server

import (
	"bytes"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"image"
	"image/color"
	"image/png"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"manifest/agentchat"
	"manifest/artifacts"
	"manifest/hermes"
	"manifest/ledger"
	"manifest/threads"
)

// Live Hermes trials — the Hermes equivalent of TestHerdrLive*
// (terminal_herdr_live_test.go). Owner decision D7 (2026-09-27): live trials
// are not authorized by default, so every test here SKIPS unless the owner
// authorizes a run through the environment. A run spends provider tokens and
// sends the probe text below to a real model; it never touches the owner's
// Hermes state, vault or checkout.
//
//	MANIFEST_HERMES_LIVE=authorized   the owner's go-ahead for this run (exact value)
//	MANIFEST_HERMES_LIVE_BIN          absolute path of the hermes CLI to invoke
//	MANIFEST_HERMES_LIVE_HOME         a scratch HERMES_HOME (config.yaml + .env for the
//	                                  provider); refused if it is the owner's ~/.hermes
//	MANIFEST_HERMES_LIVE_MODELS       optional comma list; one subtest per model
//	                                  ("" = the scratch home's configured default)
//	MANIFEST_HERMES_LIVE_PROFILE      optional -p profile inside the scratch home
//	MANIFEST_HERMES_LIVE_TOOLSETS     optional -t scope (default: production's read scope)
//	MANIFEST_HERMES_LIVE_EVIDENCE     optional evidence directory (default /tmp/manifest-hermes-live)
//
// One command (docs/hermes-live-trials.md):
//
//	MANIFEST_HERMES_LIVE=authorized MANIFEST_HERMES_LIVE_BIN=$(command -v hermes) \
//	MANIFEST_HERMES_LIVE_HOME=/tmp/hermes-live-home MANIFEST_HERMES_LIVE_MODELS=<m1>,<m2> \
//	go test ./server -run '^TestHermesLive' -count=1 -v -timeout 60m
//
// Each test writes one JSON evidence file per model: requested and reported
// model, Hermes session, delivery state, the reply, and which probe tokens
// came back. That file is the row-2 evidence the checklist cites.

// hermesLiveReadToolsets mirrors DefaultHermesReadToolsets (config.go, package
// main), the scope production passes native chat turns.
const hermesLiveReadToolsets = "web,session_search,memory,x_search,skills,clarify,context_engine,vision,mcp-manifest"

type hermesLiveEnv struct {
	bin, home, profile, toolsets, evidence string
	models                                 []string
}

func hermesLive(t *testing.T) hermesLiveEnv {
	t.Helper()
	if os.Getenv("MANIFEST_HERMES_LIVE") != "authorized" {
		t.Skip("live Hermes trial not authorized: set MANIFEST_HERMES_LIVE=authorized, MANIFEST_HERMES_LIVE_BIN and a scratch MANIFEST_HERMES_LIVE_HOME (docs/hermes-live-trials.md)")
	}
	env := hermesLiveEnv{bin: os.Getenv("MANIFEST_HERMES_LIVE_BIN"), home: os.Getenv("MANIFEST_HERMES_LIVE_HOME"),
		profile: os.Getenv("MANIFEST_HERMES_LIVE_PROFILE"), toolsets: os.Getenv("MANIFEST_HERMES_LIVE_TOOLSETS"),
		evidence: os.Getenv("MANIFEST_HERMES_LIVE_EVIDENCE")}
	if !filepath.IsAbs(env.bin) {
		t.Fatal("MANIFEST_HERMES_LIVE_BIN must be the absolute path of the hermes CLI (no $PATH lookup)")
	}
	userHome, _ := os.UserHomeDir()
	if env.home == "" || !filepath.IsAbs(env.home) || filepath.Clean(env.home) == filepath.Join(userHome, ".hermes") {
		t.Fatal("MANIFEST_HERMES_LIVE_HOME must be an absolute scratch HERMES_HOME, never the owner's ~/.hermes")
	}
	if _, err := os.Stat(filepath.Join(env.home, "config.yaml")); err != nil {
		t.Fatalf("scratch home has no config.yaml: %v", err)
	}
	if env.toolsets == "" {
		env.toolsets = hermesLiveReadToolsets
	}
	if env.evidence == "" {
		env.evidence = "/tmp/manifest-hermes-live"
	}
	if err := os.MkdirAll(env.evidence, 0o700); err != nil {
		t.Fatal(err)
	}
	for _, m := range strings.Split(os.Getenv("MANIFEST_HERMES_LIVE_MODELS"), ",") {
		env.models = append(env.models, strings.TrimSpace(m))
	}
	// the runner passes this process's environment to the CLI
	t.Setenv("HERMES_HOME", env.home)
	return env
}

// hermesLiveServer is agentChatFixture over the real CLI.
func hermesLiveServer(t *testing.T, env hermesLiveEnv) (*Server, *agentchat.Store) {
	t.Helper()
	s := New(nil, nil, nil)
	s.UseHermes(hermes.NewRunner(hermes.Config{Enabled: true, Bin: env.bin, TimeoutSeconds: 600}), env.toolsets)
	var hosts HostsInfo
	hosts.Hermes.Enabled, hosts.Hermes.Bin = true, env.bin
	s.UseHosts(hosts)
	st := agentchat.New(filepath.Join(t.TempDir(), "chats"))
	s.UseAgentChat(st)
	led, err := ledger.New(t.TempDir(), time.UTC)
	if err != nil {
		t.Fatal(err)
	}
	s.UseLedger(led)
	s.UseChatState(t.TempDir())
	return s, st
}

func hermesLiveToken(kind string) string {
	b := make([]byte, 6)
	rand.Read(b)
	return kind + "-" + hex.EncodeToString(b)
}

// hermesLiveSettle waits for a live turn (minutes, not the stub's seconds).
func hermesLiveSettle(t *testing.T, st *agentchat.Store, id string) (agentchat.Session, string) {
	t.Helper()
	deadline := time.Now().Add(12 * time.Minute)
	for time.Now().Before(deadline) {
		sess, body, _, ok := st.Get("alfred", id)
		if ok && sess.Status == agentchat.StatusIdle && !st.InFlight("alfred", id) {
			return sess, body
		}
		time.Sleep(2 * time.Second)
	}
	t.Fatal("the live turn did not settle within 12 minutes")
	return agentchat.Session{}, ""
}

func hermesLiveEvidence(t *testing.T, env hermesLiveEnv, name string, v any) {
	t.Helper()
	b, _ := json.MarshalIndent(v, "", "  ")
	path := filepath.Join(env.evidence, time.Now().UTC().Format("20060102T150405Z")+"-"+name+".json")
	if err := os.WriteFile(path, b, 0o600); err != nil {
		t.Fatal(err)
	}
	t.Logf("evidence: %s", path)
}

// Row 2: a selected endpoint/model receives a substantial instruction and the
// exact attachments and artifact context, or returns a clear error. Four
// tokens exist only in their own channel — the tail of a multi-KB
// instruction, an explicit artifact revision, a thread attachment (inlined)
// and an owned file (handed over as a path) — and the model is asked to
// return all four. A missing token names the channel that did not arrive.
func TestHermesLiveNativeChatExactContext(t *testing.T) {
	env := hermesLive(t)
	for _, model := range env.models {
		name := model
		if name == "" {
			name = "configured-default"
		}
		t.Run(name, func(t *testing.T) {
			s, st := hermesLiveServer(t, env)
			pool, err := artifacts.New(t.TempDir())
			if err != nil {
				t.Fatal(err)
			}
			reg, err := artifacts.NewRegistry(pool)
			if err != nil {
				t.Fatal(err)
			}
			s.UseArtifactRegistry(reg)
			private, err := threads.New(t.TempDir())
			if err != nil {
				t.Fatal(err)
			}
			s.UseThreads(private, nil, nil, nil, "owner@example.test")
			tokens := map[string]string{"INSTRUCTION": hermesLiveToken("tail"), "ARTIFACT": hermesLiveToken("artifact"), "THREADFILE": hermesLiveToken("thread"), "OWNEDFILE": hermesLiveToken("owned")}
			art, err := reg.Put(artifacts.Put{Ref: "live-probe.txt", Content: []byte("Probe artifact. Its token is " + tokens["ARTIFACT"] + ".\n")})
			if err != nil {
				t.Fatal(err)
			}
			blob, err := private.SaveBlob(strings.NewReader("Probe thread file. Its token is "+tokens["THREADFILE"]+".\n"), "probe-thread.txt", "text/plain")
			if err != nil {
				t.Fatal(err)
			}
			id, err := st.Create("alfred", env.profile, "Live exact-context probe", model)
			if err != nil {
				t.Fatal(err)
			}
			owned := uploadOwned(t, s, "agent:alfred/"+id, "probe-owned.txt", []byte("Probe owned file. Its token is "+tokens["OWNEDFILE"]+".\n"))
			var b strings.Builder
			b.WriteString("This is an isolated protocol probe from Manifest's test harness. Do not use web search, write anything, or change anything.\n\n")
			for i := 0; b.Len() < 8000; i++ {
				fmt.Fprintf(&b, "Background line %04d: this paragraph pads the instruction so its tail proves a multi-kilobyte message arrived whole.\n", i)
			}
			b.WriteString("\nReply with exactly four lines and nothing else:\nINSTRUCTION=<the token at the very end of this message>\nARTIFACT=<the token in the attached artifact>\nTHREADFILE=<the token in probe-thread.txt>\nOWNEDFILE=<the token in probe-owned.txt>\nIf you cannot open one of them, write CANNOT_OPEN after its = instead of guessing.\n")
			b.WriteString("[context-file:: " + owned.ID + "]\nThe token at the end of this message is " + tokens["INSTRUCTION"])
			body := map[string]any{"text": b.String(), "requestId": "live-exact-" + name, "explicitArtifacts": true,
				"artifacts": []artifactContextRef{{ID: art.Artifact.ID, Revision: art.Revision.Hash}},
				"files":     []map[string]string{{"hash": blob.Hash, "name": blob.Name}}}
			started := time.Now().UTC()
			code, out := agentChatJSON(t, s, "POST", "/api/agents/chat/alfred/sessions/"+id+"/messages", body)
			if code != 200 {
				t.Fatalf("send refused: %d %v", code, out)
			}
			sess, transcript := hermesLiveSettle(t, st, id)
			found := map[string]bool{}
			for k, v := range tokens {
				found[k] = strings.Contains(transcript, k+"="+v)
			}
			var d agentchat.Delivery
			if len(sess.Deliveries) > 0 {
				d = sess.Deliveries[len(sess.Deliveries)-1]
			}
			ev := map[string]any{"test": t.Name(), "started": started, "finished": time.Now().UTC(), "requestedModel": model, "profile": env.profile,
				"toolsets": env.toolsets, "delivery": d, "tokens": tokens, "returned": found, "instructionBytes": b.Len(), "transcript": transcript}
			hermesLiveEvidence(t, env, "exact-context-"+name, ev)
			if d.State != agentchat.DeliveryCompleted {
				t.Fatalf("delivery %s: %s (a clear error is acceptable evidence; it is recorded above)", d.State, d.Error)
			}
			for k, ok := range found {
				if !ok {
					t.Errorf("%s token did not come back: that channel did not reach %s (or the model could not open it)", k, name)
				}
			}
		})
	}
}

// Row 2/D2 evidence, never an assertion about vision: a solid red PNG is sent
// through the native path and the reply recorded. A model that names the
// colour is evidence toward a declared image capability; one that says it
// cannot open the image confirms the "vision support unknown" label. The test
// fails only if the delivery itself fails or the label is missing.
func TestHermesLiveImageProbe(t *testing.T) {
	env := hermesLive(t)
	for _, model := range env.models {
		name := model
		if name == "" {
			name = "configured-default"
		}
		t.Run(name, func(t *testing.T) {
			s, st := hermesLiveServer(t, env)
			private, err := threads.New(t.TempDir())
			if err != nil {
				t.Fatal(err)
			}
			s.UseThreads(private, nil, nil, nil, "owner@example.test")
			img := image.NewRGBA(image.Rect(0, 0, 64, 64))
			for x := 0; x < 64; x++ {
				for y := 0; y < 64; y++ {
					img.Set(x, y, color.RGBA{220, 0, 0, 255})
				}
			}
			var buf bytes.Buffer
			png.Encode(&buf, img)
			blob, err := private.SaveBlob(&buf, "probe-colour.png", "image/png")
			if err != nil {
				t.Fatal(err)
			}
			if !strings.Contains(s.agentChatAttachments(agentchat.Turn{Who: "user", Text: "[file:: " + blob.Hash + " probe-colour.png]"}), "vision support unknown") {
				t.Fatal("the image hand-off lost its vision-support-unknown label")
			}
			id, err := st.Create("alfred", env.profile, "Live image probe", model)
			if err != nil {
				t.Fatal(err)
			}
			body := map[string]any{"text": "Isolated protocol probe; do not search or write anything. Reply with one line: COLOUR=<the single colour filling the attached image>, or COLOUR=CANNOT_OPEN if you cannot view it.",
				"requestId": "live-image-" + name, "files": []map[string]string{{"hash": blob.Hash, "name": blob.Name}}}
			if code, out := agentChatJSON(t, s, "POST", "/api/agents/chat/alfred/sessions/"+id+"/messages", body); code != 200 {
				t.Fatalf("send refused: %d %v", code, out)
			}
			sess, transcript := hermesLiveSettle(t, st, id)
			var d agentchat.Delivery
			if len(sess.Deliveries) > 0 {
				d = sess.Deliveries[len(sess.Deliveries)-1]
			}
			lower := strings.ToLower(transcript)
			hermesLiveEvidence(t, env, "image-"+name, map[string]any{"test": t.Name(), "requestedModel": model, "delivery": d,
				"namedRed": strings.Contains(lower, "colour=red") || strings.Contains(lower, "color=red"), "saidCannotOpen": strings.Contains(transcript, "CANNOT_OPEN"), "transcript": transcript})
			if d.State != agentchat.DeliveryCompleted {
				t.Fatalf("delivery %s: %s", d.State, d.Error)
			}
		})
	}
}

// Row 13 (task side): one live task-thread Ask through postAndDispatch. D4:
// exactly one hand-off, one reply, no re-dispatch.
func TestHermesLiveTaskThreadAsk(t *testing.T) {
	env := hermesLive(t)
	srv := loopFixture(t)
	srv.UseHermes(hermes.NewRunner(hermes.Config{Enabled: true, Bin: env.bin, TimeoutSeconds: 600}), env.toolsets)
	id := "inbox/research-zoning"
	if _, ok := srv.pinTaskID(id); !ok {
		t.Fatal("pin")
	}
	if err := srv.setPlanAssignee(id, "agent:hermes"); err != nil {
		t.Fatal(err)
	}
	token := hermesLiveToken("ask")
	if _, err := srv.postAndDispatch(id, "ask", "", nil, nil, "Isolated protocol probe; do not search or write anything. Reply with exactly: ASK_OK "+token); err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(12 * time.Minute)
	for privateCount(srv, id, actTurnClosed) == 0 {
		if time.Now().After(deadline) {
			t.Fatal("the live Ask did not close within 12 minutes")
		}
		time.Sleep(2 * time.Second)
	}
	posts := agentPosts(srv, id)
	hermesLiveEvidence(t, env, "task-ask", map[string]any{"test": t.Name(), "posts": posts, "dispatches": privateCount(srv, id, actTurnDispatched), "redispatches": privateCount(srv, id, actTurnRedispatch)})
	if n := privateCount(srv, id, actTurnDispatched); n != 1 || privateCount(srv, id, actTurnRedispatch) != 0 {
		t.Fatalf("want one hand-off and no re-dispatch, got %d hand-offs", n)
	}
	if len(posts) != 1 || !strings.Contains(posts[0].Text, "ASK_OK "+token) {
		t.Fatalf("want one reply carrying the token: %+v", posts)
	}
}
