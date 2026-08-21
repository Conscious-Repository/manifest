package server

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"manifest/artifacts"
	"manifest/gmailsync"
	"manifest/realestate"
	"manifest/record"
	"manifest/teamportal"
	"manifest/vaultindex"
	"manifest/vaultwriter"
)

// oodaPortalFixture stands up a real OODA portal over a real (tiny) vault, so
// the write tests exercise the SHARED layer exactly as production mounts it.
func oodaPortalFixture(t *testing.T) (http.Handler, *teamportal.Auth, *teamportal.Store, string) {
	f := oodaPortalFixtureFull(t)
	return f.h, f.auth, f.store, f.vault
}

// oodaPortalHandles carries every handle the email-lane tests need beyond
// the classic 4-tuple.
type oodaPortalHandles struct {
	h     http.Handler
	auth  *teamportal.Auth
	store *teamportal.Store
	vault string
	srv   *Server
	cands *gmailsync.Candidates
	live  *OodaLive
}

func oodaPortalFixtureFull(t *testing.T) *oodaPortalHandles {
	t.Helper()
	vault, dataDir, teamDir := t.TempDir(), t.TempDir(), t.TempDir()
	write := func(rel, content string) {
		full := filepath.Join(vault, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(full, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	write("system/realestate/properties/748-n-euclid.md", `---
categories: [property]
address: 748 N Euclid Ave, St. Louis
entity: garden-spe
status: construction
control: owned
---

## rocks
- [ ] Stabilize shell [work:: shell]
    - [ ] Roof [owner:: BPA] [work:: shell/roof]
`)
	write("system/realestate/people.md", `# Real Estate People
- [initials:: BA] [name:: benjamin anderson] [role:: partner] [email:: ben@ooda.group]
- [initials:: BPA] [name:: brian anderson] [role:: partner] [email:: bpabbassa@att.net]
- [initials:: BF] [name:: brian fromal] [role:: partner] [email:: brian@ooda.group]
- [initials:: OS] [name:: olga sobkiv] [role:: partner] [email:: me@olgasobkiv.com] [contractor:: olga-sobkiv]
`)
	// the REAL mapping (corrected 2026-08-21): brian@ooda.group = Brian FROMAL
	if err := os.WriteFile(filepath.Join(teamDir, "emails.json"),
		[]byte(`{"ben@ooda.group":"BA","me@benjaminbanderson.com":"BA","brian@ooda.group":"BF","bpabbassa@att.net":"BPA","me@olgasobkiv.com":"OS"}`),
		0o600); err != nil {
		t.Fatal(err)
	}

	ix, err := vaultindex.Open(vaultindex.Config{VaultRoot: vault})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { ix.Close() })
	if _, err := ix.Rebuild(); err != nil {
		t.Fatal(err)
	}
	store, err := teamportal.New(teamDir)
	if err != nil {
		t.Fatal(err)
	}
	pol := teamportal.Policy{
		Domain: "ooda.group", CookiePrefix: "ooda_portal",
		ClientFile: "ooda-portal-oauth.json", KeyFile: "ooda-portal-session.key",
		AllowExtra: func(email string) bool {
			_, ok := store.EmailOverrides()[email]
			return ok
		},
	}
	store.WithPolicy(pol)
	auth := teamportal.NewAuthPolicy(dataDir, pol)

	srv := &Server{index: ix, aionDataDir: dataDir, realestateRoot: "system/realestate"}
	srv.realestate = realestate.New(ix)
	live := srv.NewOodaLive()
	live.UseTeam(store, teamportal.Identity{Email: "ben@ooda.group", Name: "Benjamin"})

	// the email lane + artifact pool, wired the way main.go wires production
	arts, err := artifacts.New(filepath.Join(dataDir, "artifacts"))
	if err != nil {
		t.Fatal(err)
	}
	srv.UseArtifacts(arts)
	cands, err := gmailsync.NewCandidates(filepath.Join(teamDir, "email"))
	if err != nil {
		t.Fatal(err)
	}
	gtok, err := gmailsync.NewTokens(filepath.Join(dataDir, "portals", "ooda-gmail"))
	if err != nil {
		t.Fatal(err)
	}
	srv.UseOodaEmail(cands, gtok)

	h, err := PortalHandler(PortalOptions{
		Auth: auth, Store: store, Live: live, AdminEmail: "ben@ooda.group",
		WebRoot: "web/ooda", ReadRoutes: OodaReadRoutes(live),
	})
	if err != nil {
		t.Fatal(err)
	}
	return &oodaPortalHandles{h: h, auth: auth, store: store, vault: vault, srv: srv, cands: cands, live: live}
}

func oodaDo(t *testing.T, h http.Handler, c *http.Cookie, method, path, body string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(method, path, strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")
	if c != nil {
		req.AddCookie(c)
	}
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return rec
}

// THE DOCTRINE TEST (ARCHITECTURE §12): exercise every write a portal member
// can reach and prove NOT ONE BYTE under the vault changed. If this ever
// fails, a portal write has crossed into owner-authored territory.
func TestNoPortalWriteTouchesTheVault(t *testing.T) {
	h, auth, _, vault := oodaPortalFixture(t)
	partner, err := auth.SessionCookie("me@olgasobkiv.com", "Olga", false, time.Now())
	if err != nil {
		t.Fatal(err)
	}

	// fingerprint the whole vault: path + size + content hash
	before := vaultFingerprint(t, vault)

	// every member-reachable write, in one pass
	rec := oodaDo(t, h, partner, "POST", "/api/team/items", `{"kind":"task","title":"walk the roof"}`)
	if rec.Code != 200 {
		t.Fatalf("add item: %d %s", rec.Code, rec.Body)
	}
	var item struct {
		ID string `json:"id"`
	}
	_ = json.Unmarshal(rec.Body.Bytes(), &item)

	for _, w := range []struct{ method, path, body string }{
		{"POST", "/api/team/comment", `{"item":"` + item.ID + `","text":"masonry looks done"}`},
		{"PATCH", "/api/team/item/" + item.ID, `{"status":"done"}`},
		{"POST", "/api/team/proposals", `{"target":"brian@ooda.group","kind":"task","title":"pull the permit"}`},
		{"POST", "/api/ooda/bid", `{"property":"748-n-euclid","contractor":"Tree Court","amount":10131,"scope":"windows"}`},
	} {
		if rec := oodaDo(t, h, partner, w.method, w.path, w.body); rec.Code != 200 {
			t.Fatalf("%s %s: %d %s", w.method, w.path, rec.Code, rec.Body)
		}
	}

	after := vaultFingerprint(t, vault)
	if before != after {
		t.Fatal("A PORTAL WRITE REACHED THE VAULT. Team state is derived and must " +
			"never materialize into system/realestate — see ARCHITECTURE §12.")
	}
}

func vaultFingerprint(t *testing.T, root string) string {
	t.Helper()
	var b strings.Builder
	err := filepath.Walk(root, func(path string, info os.FileInfo, err error) error {
		if err != nil || info.IsDir() {
			return err
		}
		raw, rerr := os.ReadFile(path)
		if rerr != nil {
			return rerr
		}
		b.WriteString(path)
		b.WriteString(":")
		b.WriteString(string(raw))
		b.WriteString("\n")
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	return b.String()
}

// A bid is a PROPOSAL aimed at the owner, carrying its form as payload — never
// a contract record, and never committed money.
func TestOodaBidFilesAsAProposalForTheOwner(t *testing.T) {
	h, auth, store, _ := oodaPortalFixture(t)
	partner, err := auth.SessionCookie("me@olgasobkiv.com", "Olga", false, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	rec := oodaDo(t, h, partner, "POST", "/api/ooda/bid",
		`{"property":"748-n-euclid","workId":"shell/roof","contractor":"Bastin Roofing","amount":13906.17,"scope":"tear off + shingle","expires":"2026-09-30"}`)
	if rec.Code != 200 {
		t.Fatalf("bid: %d %s", rec.Code, rec.Body)
	}
	props := store.Ext().Proposals
	if len(props) != 1 {
		t.Fatalf("proposals = %+v", props)
	}
	p := props[0]
	if p.Kind != "bid" {
		t.Fatalf("kind = %q, want bid", p.Kind)
	}
	if p.Target != "ben@ooda.group" {
		t.Fatalf("target = %q — a bid must be aimed at the only person who can "+
			"materialize a contract", p.Target)
	}
	if p.Status != "pending" {
		t.Fatalf("status = %q, want pending", p.Status)
	}
	if p.Payload["contractor"] != "Bastin Roofing" || p.Payload["workId"] != "shell/roof" {
		t.Fatalf("payload lost the form: %+v", p.Payload)
	}
	if amt, _ := p.Payload["amount"].(float64); amt != 13906.17 {
		t.Fatalf("amount = %v", p.Payload["amount"])
	}
	// garbage is refused rather than filed
	for _, bad := range []string{
		`{"property":"","contractor":"X","amount":1}`,
		`{"property":"748-n-euclid","contractor":"","amount":1}`,
		`{"property":"748-n-euclid","contractor":"X","amount":0}`,
		`{"property":"no-such-property","contractor":"X","amount":1}`,
	} {
		if rec := oodaDo(t, h, partner, "POST", "/api/ooda/bid", bad); rec.Code == 200 {
			t.Fatalf("a malformed bid was accepted: %s", bad)
		}
	}
	if len(store.Ext().Proposals) != 1 {
		t.Fatal("a rejected bid still landed in the store")
	}
}

// The assignee lock reaches the OODA portal unchanged — including the absence
// of an admin override lane.
func TestOodaAssigneeLockHoldsForAdminToo(t *testing.T) {
	h, auth, store, _ := oodaPortalFixture(t)
	brian, err := auth.SessionCookie("bpabbassa@att.net", "Brian", false, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	admin, err := auth.SessionCookie("ben@ooda.group", "Benjamin", false, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	rec := oodaDo(t, h, brian, "POST", "/api/team/items", `{"kind":"task","title":"brian's item"}`)
	if rec.Code != 200 {
		t.Fatalf("add: %d %s", rec.Code, rec.Body)
	}
	var item struct {
		ID string `json:"id"`
	}
	_ = json.Unmarshal(rec.Body.Bytes(), &item)

	// the ADMIN cannot patch someone else's item — no override lane exists
	if rec := oodaDo(t, h, admin, "PATCH", "/api/team/item/"+item.ID, `{"status":"done"}`); rec.Code != 403 {
		t.Fatalf("admin patch of another's item = %d, want 403 (no override lane)", rec.Code)
	}
	before := store.Ext().Overrides
	if len(before) != 0 {
		t.Fatalf("a refused patch still wrote an override: %+v", before)
	}
	// the assignee can
	if rec := oodaDo(t, h, brian, "PATCH", "/api/team/item/"+item.ID, `{"status":"done"}`); rec.Code != 200 {
		t.Fatalf("assignee patch = %d %s", rec.Code, rec.Body)
	}
	// the same-person-two-addresses arm anchors on BA, who really has two:
	// the admin adds his own item, and his NON-admin personal address may
	// patch it — same initials, so the assignee lock recognizes one human.
	rec = oodaDo(t, h, admin, "POST", "/api/team/items", `{"kind":"task","title":"ben's item"}`)
	if rec.Code != 200 {
		t.Fatalf("admin add: %d %s", rec.Code, rec.Body)
	}
	var benItem struct {
		ID string `json:"id"`
	}
	_ = json.Unmarshal(rec.Body.Bytes(), &benItem)
	benAlt, err := auth.SessionCookie("me@benjaminbanderson.com", "Benjamin", false, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	if rec := oodaDo(t, h, benAlt, "PATCH", "/api/team/item/"+benItem.ID, `{"status":"done"}`); rec.Code != 200 {
		t.Fatalf("the same person's second address = %d, want 200", rec.Code)
	}
}

// THE FULL BID LOOP: a partner files a bid (no vault write), the owner accepts
// it in the COCKPIT, and only then does a contract record exist. This is the
// trust boundary the whole lane is built around.
func TestOodaBidBecomesAContractOnlyByTheOwnersHand(t *testing.T) {
	h, auth, store, vault := oodaPortalFixture(t)
	// the cockpit half needs a writer with the re-contracts capability
	srv := oodaCockpitFor(t, vault, store)

	partner, err := auth.SessionCookie("me@olgasobkiv.com", "Olga", false, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	if rec := oodaDo(t, h, partner, "POST", "/api/ooda/bid",
		`{"property":"748-n-euclid","workId":"shell/roof","contractor":"Bastin Roofing","amount":13906.17,"scope":"tear off and reshingle"}`); rec.Code != 200 {
		t.Fatalf("file bid: %d %s", rec.Code, rec.Body)
	}
	contractsDir := filepath.Join(vault, "system/realestate/contracts")
	if _, err := os.Stat(contractsDir); !os.IsNotExist(err) {
		t.Fatal("filing a bid created a contracts directory — the portal must not write the vault")
	}

	// the owner sees it pending in the cockpit
	code, listed := doJSON(t, srv.handleOodaBidsList, "GET", "/api/realestate/ooda-bids", "")
	if code != 200 {
		t.Fatalf("list: %d", code)
	}
	bids, _ := listed["bids"].([]any)
	if len(bids) != 1 {
		t.Fatalf("pending bids = %v", listed["bids"])
	}
	bidID, _ := bids[0].(map[string]any)["id"].(string)

	// accepting it — an OWNER action — mints the record
	req := httptest.NewRequest("POST", "/api/realestate/ooda-bids/"+bidID, strings.NewReader(`{"accept":true}`))
	req.SetPathValue("id", bidID)
	rec := httptest.NewRecorder()
	srv.handleOodaBidDecide(rec, req)
	if rec.Code != 200 {
		t.Fatalf("accept: %d %s", rec.Code, rec.Body)
	}
	var out struct {
		Contract string `json:"contract"`
	}
	_ = json.Unmarshal(rec.Body.Bytes(), &out)
	if out.Contract == "" {
		t.Fatalf("no contract slug: %s", rec.Body)
	}
	raw, err := os.ReadFile(filepath.Join(contractsDir, out.Contract+".md"))
	if err != nil {
		t.Fatalf("contract record: %v", err)
	}
	for _, want := range []string{"categories: [contract]", "total: 13906.17",
		"748-n-euclid | shell/roof | 13906.17", "status: proposed"} {
		if !strings.Contains(string(raw), want) {
			t.Fatalf("contract missing %q:\n%s", want, raw)
		}
	}
	// accepting the BID is not accepting the CONTRACT — money is not yet
	// committed until the owner accepts the contract itself
	if strings.Contains(string(raw), "status: accepted") {
		t.Fatal("a filed bid must not arrive pre-accepted — that would commit money on a partner's say-so")
	}
	// the proposal closed, so it cannot be double-materialized
	for _, p := range store.Ext().Proposals {
		if p.ID == bidID && p.Status != "approved" {
			t.Fatalf("proposal status = %q after accept", p.Status)
		}
	}
	rec2 := httptest.NewRecorder()
	req2 := httptest.NewRequest("POST", "/api/realestate/ooda-bids/"+bidID, strings.NewReader(`{"accept":true}`))
	req2.SetPathValue("id", bidID)
	srv.handleOodaBidDecide(rec2, req2)
	if rec2.Code == 200 {
		t.Fatal("the same bid materialized twice")
	}
}

// oodaCockpitFor builds the cockpit-side Server (vault writer + realestate +
// the OODA team store) over the same vault the portal fixture created.
func oodaCockpitFor(t *testing.T, vault string, store *teamportal.Store) *Server {
	t.Helper()
	ix, err := vaultindex.Open(vaultindex.Config{VaultRoot: vault})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { ix.Close() })
	if _, err := ix.Rebuild(); err != nil {
		t.Fatal(err)
	}
	vw := vaultwriter.New(vault).WithZoneRoots("system", "extrinsic").Grant(
		vaultwriter.Capability{Name: "re-contracts", Zone: record.ZoneSystem,
			Pattern: "system/realestate/contracts/**", Actor: vaultwriter.ActorUserAction},
		vaultwriter.Capability{Name: "realestate", Zone: record.ZoneSystem,
			Pattern: "system/realestate/**", Actor: vaultwriter.ActorUserAction},
	)
	srv := &Server{index: ix, realestateRoot: "system/realestate"}
	srv.UseVault(vw)
	srv.realestate = realestate.New(ix)
	srv.UseOodaBids(store, "ben@ooda.group")
	return srv
}
