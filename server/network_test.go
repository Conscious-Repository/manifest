package server

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"manifest/contacts"
	"manifest/recruiting"
	"manifest/vaultindex"
)

// networkTestServer: a recruiting store + a real contacts layer over three
// person notes + an AION team roster.
func networkTestServer(t *testing.T) (*Server, string) {
	t.Helper()
	s, vw, vault, dataDir := testRecruitingServer(t)
	for path, body := range map[string]string{
		"Alice Ray.md":          "---\ncategories: [people]\n---\n",
		"Bob Stone.md":          "---\ncategories: [people]\n---\n",
		"Carol Tu.md":           "---\ncategories: [people]\n---\n",
		"system/aion/people.md": "---\n---\n- [initials:: BA] [name:: Benjamin Anderson] [role:: CEO]\n",
	} {
		abs := filepath.Join(vault, path)
		if err := os.MkdirAll(filepath.Dir(abs), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(abs, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	ix, err := vaultindex.Open(vaultindex.Config{VaultRoot: vault})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { ix.Close() })
	if _, err := ix.Rebuild(); err != nil {
		t.Fatal(err)
	}
	s.index = ix
	cs, err := contacts.NewStore(dataDir)
	if err != nil {
		t.Fatal(err)
	}
	s.contacts = contacts.New(ix, cs, vw, recruitingMeetingCalendar{}, nil)
	return s, vault
}

func networkGet(t *testing.T, s *Server) map[string]NetPerson {
	t.Helper()
	w := httptest.NewRecorder()
	s.handleNetwork(w, httptest.NewRequest(http.MethodGet, "/api/network", nil))
	if w.Code != http.StatusOK {
		t.Fatalf("%d %s", w.Code, w.Body.String())
	}
	var out struct{ People []NetPerson }
	if err := json.Unmarshal(w.Body.Bytes(), &out); err != nil {
		t.Fatal(err)
	}
	byName := map[string]NetPerson{}
	for _, p := range out.People {
		if prior, dup := byName[p.Name]; dup {
			byName[p.Name+"#2"] = p
			_ = prior
			continue
		}
		byName[p.Name] = p
	}
	return byName
}

// ⚠ ONE HUMAN, ONE ROW — ONLY BY EXPLICIT LINK (ARCHITECTURE §9). A kept row
// that refs a contact absorbs it; a kept row that merely shares a contact's
// NAME stays a second row; the team member arrives by initials.
func TestNetworkResolvesRegistriesByExplicitLinksOnly(t *testing.T) {
	s, _ := networkTestServer(t)
	// Alice: a kept row that EXPLICITLY refs her contact
	if err := s.recruiting.AddNetworkPerson(recruiting.NetworkPerson{Name: "Alice Ray", Ref: "alice ray", Type: "advisor"}); err != nil {
		t.Fatal(err)
	}
	// "Bob Stone": a kept row with the same NAME as a contact and no ref
	if err := s.recruiting.AddNetworkPerson(recruiting.NetworkPerson{Name: "Bob Stone", Type: "expert"}); err != nil {
		t.Fatal(err)
	}
	got := networkGet(t, s)

	alice := got["Alice Ray"]
	if !alice.Editable || alice.Kind != "advisor" || strings.Join(alice.Sources, ",") != "kept,contact" {
		t.Fatalf("a ref'd contact did not merge onto its row: %+v", alice)
	}
	if _, dup := got["Alice Ray#2"]; dup {
		t.Fatal("Alice drew twice despite the explicit link")
	}
	if _, two := got["Bob Stone#2"]; !two {
		t.Fatal("a same-name contact was merged by NAME — §9 forbids inference")
	}
	carol := got["Carol Tu"]
	if carol.Editable || carol.ID != "contact/carol tu" {
		t.Fatalf("an unlinked contact should be a read-only contact row: %+v", carol)
	}
	// the install seed's Ben row and the team roster's BA are two rows until
	// LINKED — the same §9 rule, and the reason the editor has a team link
	var team, seed NetPerson
	for _, k := range []string{"Benjamin Anderson", "Benjamin Anderson#2"} {
		if got[k].ID == "team/BA" {
			team = got[k]
		} else {
			seed = got[k]
		}
	}
	if team.ID != "team/BA" || team.Role != "CEO" || seed.ID != "aion-net/ben-anderson" {
		t.Fatalf("before linking: team=%+v seed=%+v", team, seed)
	}
	// the pair is OFFERED as the same person (both ways), never merged
	if len(seed.SameName) != 1 || seed.SameName[0].ID != "team/BA" || len(team.SameName) != 1 {
		t.Fatalf("same-name suggestion: seed=%+v team=%+v", seed.SameName, team.SameName)
	}
	// two KEPT rows with one name have no link that joins them — not offered
	if b := got["Bob Stone"]; len(b.SameName) != 1 || b.SameName[0].ID != "contact/bob stone" {
		t.Fatalf("kept row vs contact is joinable by ref: %+v", b.SameName)
	}
	w := recruitingPost(t, s, s.handleNetworkPerson, "/api/network/person/x", seed.ID, `{"set":{"team":"BA"}}`)
	if w.Code != http.StatusOK {
		t.Fatalf("%d %s", w.Code, w.Body.String())
	}
	got = networkGet(t, s)
	if _, dup := got["Benjamin Anderson#2"]; dup {
		t.Fatal("an explicit team link did not merge the seed row onto the team member")
	}
	if b := got["Benjamin Anderson"]; b.Role != "CEO" || strings.Join(b.Sources, ",") != "kept,team" || len(b.SameName) != 0 {
		t.Fatalf("linked: %+v", b)
	}
}

// The first edit of a contact CREATES its linked row (by the contact key) and
// never touches the vault note; a second edit reuses the row.
func TestNetworkEditingAContactAdoptsItWithoutWritingTheNote(t *testing.T) {
	s, vault := networkTestServer(t)
	noteBefore := readFile(t, filepath.Join(vault, "Carol Tu.md"))

	w := recruitingPost(t, s, s.handleNetworkPerson, "/api/network/person/contact/carol%20tu", "contact/carol tu",
		`{"set":{"tags":"fda-510k","kind":"expert","note":"regulatory, ran two 510(k)s"}}`)
	if w.Code != http.StatusOK {
		t.Fatalf("%d %s", w.Code, w.Body.String())
	}
	got := networkGet(t, s)
	carol := got["Carol Tu"]
	if !carol.Editable || carol.Kind != "expert" || strings.Join(carol.Tags, ",") != "fda-510k" ||
		strings.Join(carol.Sources, ",") != "kept,contact" {
		t.Fatalf("adoption: %+v", carol)
	}
	if _, dup := got["Carol Tu#2"]; dup {
		t.Fatal("adoption left a second Carol behind")
	}
	if readFile(t, filepath.Join(vault, "Carol Tu.md")) != noteBefore {
		t.Fatal("editing a contact in Network wrote their vault note")
	}
	rows := len(s.recruiting.Connectors())
	w = recruitingPost(t, s, s.handleNetworkPerson, "/api/network/person/x", carol.ID, `{"set":{"last_contact":"2026-09-27"}}`)
	if w.Code != http.StatusOK || len(s.recruiting.Connectors()) != rows {
		t.Fatalf("a second edit wrote a second row: %d %s", w.Code, w.Body.String())
	}
}

func networkGraphGet(t *testing.T, s *Server, query string) graphReply {
	t.Helper()
	w := httptest.NewRecorder()
	s.handleNetworkGraph(w, httptest.NewRequest(http.MethodGet, "/api/network/graph?"+query, nil))
	if w.Code != http.StatusOK {
		t.Fatalf("%d %s", w.Code, w.Body.String())
	}
	var out graphReply
	if err := json.Unmarshal(w.Body.Bytes(), &out); err != nil {
		t.Fatal(err)
	}
	return out
}

// ⚠ THE NETWORK GRAPH IS THE LIST'S PEOPLE: an investor whose contact a kept
// row refs is ONE node however an edge was filed (by contact key or by row),
// kinded by what the owner said they are; a same-name row is still two nodes.
func TestNetworkGraphDrawsOneNodePerLinkedHumanAndNeverMergesByName(t *testing.T) {
	s, _ := networkTestServer(t)
	fr := testFundraisingStore(t)
	s.fundraising = fr
	op, err := fr.Create("Acme Ventures")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := fr.Update(op.ID, map[string]any{"people": []map[string]string{{"key": "Alice Ray", "display": "Alice Ray"}}}); err != nil {
		t.Fatal(err)
	}
	if err := s.recruiting.AddNetworkPerson(recruiting.NetworkPerson{Name: "Alice Ray", Ref: "Alice Ray", Type: "advisor"}); err != nil {
		t.Fatal(err)
	}
	if err := s.recruiting.AddNetworkPerson(recruiting.NetworkPerson{Name: "Bob Stone", Type: "expert"}); err != nil {
		t.Fatal(err)
	}
	var aliceRow string
	for _, p := range s.recruiting.Connectors() {
		if p.Name == "Alice Ray" {
			aliceRow = p.ID
		}
	}
	s.recruiting.UseDerivedEdges(func() []recruiting.Edge {
		return []recruiting.Edge{
			{From: "contact/alice ray", To: "contact/carol tu", Kind: "same_meeting", Basis: "a call", Confidence: "0.70", Inferred: true, Source: "calendar"},
			{From: aliceRow, To: "contact/carol tu", Kind: "same_meeting", Basis: "a call", Confidence: "0.70", Inferred: true, Source: "calendar"},
			{From: "contact/bob stone", To: "contact/carol tu", Kind: "same_meeting", Basis: "a call", Confidence: "0.70", Inferred: true, Source: "calendar"},
		}
	})

	g := networkGraphGet(t, s, "mode=whole")
	byLabel := map[string][]graphNode{}
	for _, n := range g.Nodes {
		byLabel[n.Label] = append(byLabel[n.Label], n)
	}
	if a := byLabel["Alice Ray"]; len(a) != 1 || a[0].ID != aliceRow || a[0].Kind != "investor" {
		t.Fatalf("Alice should be ONE investor node on her row: %+v", a)
	}
	if b := byLabel["Bob Stone"]; len(b) != 2 {
		t.Fatalf("a same-name kept row and contact must stay two nodes: %+v", b)
	}
	if c := byLabel["Carol Tu"]; len(c) != 1 || c[0].Kind != "known" {
		t.Fatalf("an unkinded contact is known: %+v", c)
	}
	aliceToCarol := 0
	for _, e := range g.Edges {
		if (e.From == aliceRow || e.To == aliceRow) && (e.From == "contact/carol tu" || e.To == "contact/carol tu") {
			aliceToCarol++
		}
		if strings.HasPrefix(e.From, "contact/alice") || strings.HasPrefix(e.To, "contact/alice") {
			t.Fatalf("an edge still names the absorbed contact: %+v", e)
		}
	}
	if aliceToCarol != 1 {
		t.Fatalf("the two filings of one tie should fold to one edge, got %d", aliceToCarol)
	}
	// the team member draws with no edges at all — whole mode is the list
	if tm := byLabel["Benjamin Anderson"]; len(tm) == 0 {
		t.Fatal("an unconnected team member was not drawn")
	}
	// an unchecked kind is absent, not painted
	g = networkGraphGet(t, s, "mode=whole&status=team")
	for _, n := range g.Nodes {
		if n.Kind == "investor" || n.Kind == "known" {
			t.Fatalf("a hidden kind was drawn: %+v", n)
		}
	}
}

// The 240 ceiling holds through the Network lens too.
func TestNetworkGraphHoldsTheCeiling(t *testing.T) {
	s, _ := networkTestServer(t)
	for i := 0; i < graphMaxNodes+40; i++ {
		if err := s.recruiting.AddNetworkPerson(recruiting.NetworkPerson{Name: "Person " + strconv.Itoa(i), Type: "hire"}); err != nil {
			t.Fatal(err)
		}
	}
	g := networkGraphGet(t, s, "mode=whole")
	if len(g.Nodes) > graphMaxNodes || g.Omitted["whole"] == 0 {
		t.Fatalf("ceiling: %d nodes, omitted %+v", len(g.Nodes), g.Omitted)
	}
}

// A kept row's source topics are OFFERED (suggest), minus what the owner has
// already tagged; accepting one is an ordinary tag edit. Nothing tags itself.
func TestNetworkSuggestsSourceTopicsNotYetTagged(t *testing.T) {
	s, _ := networkTestServer(t)
	if err := s.recruiting.AddNetworkPerson(recruiting.NetworkPerson{Name: "Ada Coil", Type: "expert",
		Topics: []string{"MRI coils", "FDA"}, Tags: []string{"fda"}}); err != nil {
		t.Fatal(err)
	}
	ada := networkGet(t, s)["Ada Coil"]
	if strings.Join(ada.Suggest, "|") != "MRI coils" || strings.Join(ada.Tags, "|") != "fda" {
		t.Fatalf("suggest=%v tags=%v", ada.Suggest, ada.Tags)
	}
	w := recruitingPost(t, s, s.handleNetworkPerson, "/api/network/person/x", ada.ID, `{"set":{"tags":"fda, MRI coils"}}`)
	if w.Code != http.StatusOK {
		t.Fatalf("%d %s", w.Code, w.Body.String())
	}
	ada = networkGet(t, s)["Ada Coil"]
	if len(ada.Suggest) != 0 || len(ada.Tags) != 2 {
		t.Fatalf("an accepted suggestion should leave suggest: %+v", ada)
	}
	var row recruiting.NetworkPerson
	for _, p := range s.recruiting.Connectors() {
		if p.ID == ada.ID {
			row = p
		}
	}
	if len(row.Topics) != 2 {
		t.Fatalf("accepting a tag must not rewrite what the source said: %+v", row.Topics)
	}
}

// Contacts merged into Network: a contact row carries what Contacts computes
// (has-note, and the rest of the neglect lens), and the list answers even
// with the recruiting store unwired — contacts are not a recruiting feature.
func TestNetworkListsContactsWithTheirSignalsWithoutRecruiting(t *testing.T) {
	s, _ := networkTestServer(t)
	if carol := networkGet(t, s)["Carol Tu"]; !carol.HasNote || carol.NotePath == "" {
		t.Fatalf("contact signals missing: %+v", carol)
	}
	s.recruiting = nil
	got := networkGet(t, s)
	if carol := got["Carol Tu"]; carol.ID != "contact/carol tu" || carol.Editable {
		t.Fatalf("without recruiting, contacts still list read-only: %+v", carol)
	}
}

// A link is undone by clearing it: the row and the contact draw as two again,
// and nothing else (the note, the row's other fields) changes.
func TestNetworkUnlinkSplitsTheRowBackApart(t *testing.T) {
	s, vault := networkTestServer(t)
	noteBefore := readFile(t, filepath.Join(vault, "Alice Ray.md"))
	if err := s.recruiting.AddNetworkPerson(recruiting.NetworkPerson{Name: "Alice Ray", Ref: "alice ray", Type: "advisor"}); err != nil {
		t.Fatal(err)
	}
	alice := networkGet(t, s)["Alice Ray"]
	if w := recruitingPost(t, s, s.handleNetworkPerson, "/api/network/person/x", alice.ID, `{"set":{"ref":""}}`); w.Code != http.StatusOK {
		t.Fatalf("%d %s", w.Code, w.Body.String())
	}
	got := networkGet(t, s)
	if _, two := got["Alice Ray#2"]; !two {
		t.Fatal("clearing the link did not split the row from the contact")
	}
	if a := got["Alice Ray"]; a.Kind != "advisor" && got["Alice Ray#2"].Kind != "advisor" {
		t.Fatal("unlinking lost the row's own fields")
	}
	if readFile(t, filepath.Join(vault, "Alice Ray.md")) != noteBefore {
		t.Fatal("unlinking touched the vault note")
	}
}

// SAVED people (kept from a sweep, no contact yet) sit apart from your
// people, carry the source's topics as their experience, draw as "saved" in
// the graph — and graduate by getting a contact link.
func TestNetworkSavedPeopleSitApartUntilTheyBecomeContacts(t *testing.T) {
	s, _ := networkTestServer(t)
	if err := s.recruiting.AddNetworkPerson(recruiting.NetworkPerson{Name: "Ada Coil", Source: "openalex",
		SourceRef: "openalex:A1", Topics: []string{"RF coils"}}); err != nil {
		t.Fatal(err)
	}
	ada := networkGet(t, s)["Ada Coil"]
	if !ada.Saved || strings.Join(ada.Topics, ",") != "RF coils" {
		t.Fatalf("saved: %+v", ada)
	}
	var kind string
	for _, n := range networkGraphGet(t, s, "mode=whole").Nodes {
		if n.ID == ada.ID {
			kind = n.Kind
		}
	}
	if kind != "saved" {
		t.Fatalf("graph kind %q", kind)
	}
	w := httptest.NewRecorder()
	s.handleNetwork(w, httptest.NewRequest(http.MethodGet, "/api/network", nil))
	if !strings.Contains(w.Body.String(), `"tag":"RF coils","count":1`) {
		t.Fatalf("a source topic is not in the experience filter: %s", w.Body.String()[:200])
	}
	if w := recruitingPost(t, s, s.handleNetworkPerson, "/api/network/person/x", ada.ID, `{"set":{"ref":"carol tu"}}`); w.Code != http.StatusOK {
		t.Fatal(w.Body.String())
	}
	if got := networkGet(t, s); got["Ada Coil"].Saved {
		t.Fatal("a contact link should graduate a saved person")
	}
}
