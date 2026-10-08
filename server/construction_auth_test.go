package server

import (
	"encoding/json"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"regexp"
	"strings"
	"testing"

	"manifest/construction"
)

// The deployment boundary (construction_auth.go) is local host trust: the
// routes answer loopback connections with a loopback Host and no proxy
// headers. A caller the server can recognise as remote — a non-loopback
// peer that copies everything the owner's page sends (loopback Host,
// same-origin Origin and Fetch-Metadata, the real session nonce), or a
// request carrying proxy/tailnet headers or a non-loopback Host — is refused
// on every route. Headers cannot widen access, and an injected resolver is
// never consulted for a refused request. The loopback same-origin owner
// session keeps working. (A raw TCP forward onto loopback is not
// recognisable; TestConstructionRawTCPForwardIsIndistinguishable pins that.)
func TestConstructionRecognisedRemoteRefused(t *testing.T) {
	f := constructionFixture(t)
	// the loopback owner session: /session, create, read
	sess := f.do(t, "GET", fixtureBase+"/session", nil)
	if sess.Code != 200 {
		t.Fatalf("loopback session: %d %s", sess.Code, sess.Body)
	}
	nonce := sess.json(t)["nonce"].(string)
	if caps := sess.json(t)["capabilities"].(map[string]any); !strings.HasPrefix(sess.json(t)["boundary"].(string), "Manifest's private listener") ||
		caps["accessModel"] != "private-listener" || caps["remoteAccess"] != "tailnet" {
		t.Fatalf("the session reports the private-listener boundary: %s", sess.Body)
	}
	v := f.create(t, fixtureBase, "create-remote-01", "Loopback owner problem", nil)
	id := viewProblem(v)["id"].(string)
	sub := construction.SubjectRef{Kind: "property", ID: "fixture-ooda-house"}
	head := func() string {
		b, _ := os.ReadFile(filepath.Join(f.root, "projects", construction.ProjectKey(sub), "problems", id, "head.json"))
		return string(b)
	}
	before := head()
	cmd := func(req string) map[string]any {
		return map[string]any{"schemaVersion": 1, "requestId": req, "problemId": id, "expectedProblemRevision": viewRev(v, "problem"),
			"operations": []any{map[string]any{"op": "SetProblemText", "title": "changed remotely"}}}
	}
	routes := []struct {
		method, path string
		body         any
	}{
		{"GET", fixtureBase + "/session", nil},
		{"GET", fixtureBase + "/problems", nil},
		{"GET", fixtureBase + "/problems/" + id, nil},
		{"GET", fixtureBase + "/problems/" + id + "/history", nil},
		{"GET", fixtureBase + "/problems/" + id + "/export", nil},
		{"GET", fixtureBase + "/preflight", nil},
		{"GET", "/api/home/construction/problems", nil},
		{"POST", fixtureBase + "/problems", map[string]any{"schemaVersion": 1, "requestId": "create-remote-02", "title": "Forged"}},
		{"POST", fixtureBase + "/problems/" + id + "/commands", cmd("cmd-remote-001")},
	}
	refused := func(name string, r cResp) {
		t.Helper()
		if r.Code != http.StatusForbidden {
			t.Fatalf("%s: want 403, got %d %s", name, r.Code, r.Body)
		}
		var e struct {
			Kind     string   `json:"kind"`
			Error    string   `json:"error"`
			Problems []string `json:"problems"`
		}
		if json.Unmarshal(r.Body, &e) != nil || e.Kind != "remote-disabled" || !strings.Contains(e.Error, "tailnet") || len(e.Problems) != 1 {
			t.Fatalf("%s: want the remote-disabled explanation, got %s", name, r.Body)
		}
		if strings.Contains(string(r.Body), nonce) {
			t.Fatalf("%s: the refusal leaked the nonce", name)
		}
	}

	// forged remote requests: every header matches the owner's page and the
	// nonce is the real one; only the TCP peer is not this machine
	forged := func(peer string) func(*http.Request) {
		return func(r *http.Request) {
			r.RemoteAddr = peer
			r.Host = cHost
			r.Header.Set("Origin", "http://"+cHost)
			r.Header.Set("Sec-Fetch-Site", "same-origin")
			r.Header.Set("X-Construction-Nonce", nonce)
		}
	}
	for _, peer := range []string{"100.101.102.103:41234", "[fd7a:115c:a1e0::1]:41234", "192.168.1.20:52000", "[2001:db8::7]:443", "0.0.0.0:1", "", "not-an-address"} {
		for _, rt := range routes {
			refused("forged "+rt.method+" "+rt.path+" from "+peer, f.do(t, rt.method, rt.path, rt.body, forged(peer)))
		}
	}
	// headers cannot make a remote peer local
	refused("remote peer claiming loopback in headers", f.do(t, "GET", fixtureBase+"/problems", nil, forged("100.101.102.103:41234"), func(r *http.Request) {
		r.Header.Set("X-Forwarded-For", "127.0.0.1")
		r.Header.Set("X-Real-IP", "127.0.0.1")
	}))

	// the owner's tailnet path: `tailscale serve` relays onto the loopback
	// listener with its forwarding and identity headers — answered, as for
	// every other owner surface
	tailnet := "metis.tail1234.ts.net"
	viaTailnet := func(r *http.Request) {
		r.Host = tailnet
		r.Header.Set("Origin", "https://"+tailnet)
		r.Header.Set("X-Forwarded-For", "100.101.102.103")
		r.Header.Set("X-Forwarded-Proto", "https")
		r.Header.Set("Tailscale-User-Login", "owner@example.com")
	}
	if r := f.do(t, "GET", fixtureBase+"/session", nil, viaTailnet); r.Code != http.StatusOK {
		t.Fatalf("tailscale serve GET: %d %s", r.Code, r.Body)
	}
	if r := f.do(t, "POST", fixtureBase+"/problems", map[string]any{"schemaVersion": 1, "requestId": "create-tailnet-01", "title": "From the tailnet"}, viaTailnet, func(r *http.Request) {
		r.Header.Set("Sec-Fetch-Site", "same-origin")
		r.Header.Set("X-Construction-Nonce", nonce)
	}); r.Code != http.StatusOK {
		t.Fatalf("tailscale serve POST: %d %s", r.Code, r.Body)
	}
	// a public CDN/tunnel in front of the listener, and DNS-rebinding Hosts,
	// are refused
	relays := map[string]func(*http.Request){
		"Cloudflare tunnel":                func(r *http.Request) { r.Header.Set("Cf-Connecting-Ip", "203.0.113.9") },
		"Cloudflare ray":                   func(r *http.Request) { r.Header.Set("CF-Ray", "8a1b2c3d4e5f-ORD") },
		"Fastly":                           func(r *http.Request) { r.Header.Set("Fastly-Client-IP", "203.0.113.9") },
		"non-canonical header key":         func(r *http.Request) { r.Header["cf-connecting-ip"] = []string{"203.0.113.9"} },
		"public Host only (DNS rebinding)": func(r *http.Request) { r.Host = "attacker.example:7781" },
		"look-alike Host":                  func(r *http.Request) { r.Host = "127.0.0.1.attacker.example:7781" },
		"localhost subdomain of attacker":  func(r *http.Request) { r.Host = "localhost.attacker.example:7781" },
		"ts.net look-alike":                func(r *http.Request) { r.Host = "metis.ts.net.attacker.example" },
	}
	for name, mutate := range relays {
		refused(name+" GET", f.do(t, "GET", fixtureBase+"/session", nil, mutate))
		refused(name+" POST", f.do(t, "POST", fixtureBase+"/problems/"+id+"/commands", cmd("cmd-remote-002"), mutate))
	}

	// an injected resolver that would say "owner" to anyone is never asked
	// about a remote request, and it sees only the transport peer
	calls, seen := 0, ""
	f.srv.construction.principal = func(p constructionPeer) (construction.Actor, error) {
		calls++
		seen = p.RemoteAddr
		return construction.OwnerActor(), nil
	}
	refused("remote request with an owner-saying resolver", f.do(t, "GET", fixtureBase+"/problems", nil, forged("100.101.102.103:41234")))
	if calls != 0 {
		t.Fatalf("the resolver was consulted for a remote request (%d calls)", calls)
	}
	if r := f.do(t, "GET", fixtureBase+"/problems", nil); r.Code != 200 || calls != 1 || seen != cPeer {
		t.Fatalf("loopback with the resolver: %d calls=%d peer=%q", r.Code, calls, seen)
	}
	f.srv.construction.principal = nil
	pt := reflect.TypeOf(constructionPeer{})
	if pt.NumField() != 1 || pt.Field(0).Name != "RemoteAddr" || pt.Field(0).Type.Kind() != reflect.String {
		t.Fatalf("a resolver must see transport facts only, never headers/cookies/URL/body: %v", pt)
	}
	if ft := reflect.TypeOf(constructionPrincipal(nil)); ft.NumIn() != 1 || ft.In(0) != pt {
		t.Fatalf("the resolver's only input is the peer: %v", ft)
	}

	// nothing a refused request carried reached the store
	if head() != before {
		t.Fatal("a refused remote request changed the problem")
	}
	// two problems: the loopback owner's and the one created over the tailnet
	if list, err := f.srv.construction.store.List(sub); err != nil || len(list) != 2 {
		t.Fatalf("a refused remote create was stored: %+v %v", list, err)
	}

	// the loopback owner keeps every way of naming this machine, and can write
	for _, lo := range []struct{ peer, host string }{{"[::1]:50000", "localhost:7781"}, {"127.0.0.1:50000", "[::1]:7781"}, {"127.0.0.2:50000", "LOCALHOST"}} {
		r := f.do(t, "GET", fixtureBase+"/session", nil, func(r *http.Request) {
			r.RemoteAddr, r.Host = lo.peer, lo.host
			r.Header.Set("Origin", "http://"+lo.host)
		})
		if r.Code != 200 {
			t.Fatalf("loopback %s → %s: %d %s", lo.peer, lo.host, r.Code, r.Body)
		}
	}
	if r := f.do(t, "POST", fixtureBase+"/problems/"+id+"/commands", cmd("cmd-remote-003")); r.Code != 200 {
		t.Fatalf("the loopback owner's command: %d %s", r.Code, r.Body)
	}
	if head() == before {
		t.Fatal("the loopback owner's command did not commit")
	}
	f.assertSourcesUntouched(t)
}

// Through a real net/http server the peer is what the accepted connection
// reports. A connection to the private handler that does not come from
// loopback (another machine reaching a listener directly) is refused even
// with the owner's Host, Origin, Fetch-Metadata and nonce; on a loopback
// connection the owner session works, including `tailscale serve`'s relayed
// shape (tailnet Host, forwarding headers).
func TestConstructionRemoteSocketRefused(t *testing.T) {
	f := constructionFixture(t)
	local := httptest.NewServer(f.srv.Handler())
	defer local.Close()
	remote := httptest.NewUnstartedServer(f.srv.Handler())
	remote.Listener = peerListener{remote.Listener, &net.TCPAddr{IP: net.ParseIP("100.101.102.103"), Port: 41234}}
	remote.Start()
	defer remote.Close()
	client := &http.Client{Transport: &http.Transport{}} // no proxy from the environment
	defer client.CloseIdleConnections()
	send := func(srv *httptest.Server, method, path, host string, hdr map[string]string, body string) (int, map[string]any) {
		t.Helper()
		req, err := http.NewRequest(method, srv.URL+path, strings.NewReader(body))
		if err != nil {
			t.Fatal(err)
		}
		req.Host = host
		for k, v := range hdr {
			req.Header.Set(k, v)
		}
		res, err := client.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		defer res.Body.Close()
		raw, _ := io.ReadAll(res.Body)
		var m map[string]any
		_ = json.Unmarshal(raw, &m)
		return res.StatusCode, m
	}
	localHost := strings.TrimPrefix(local.URL, "http://")
	code, sess := send(local, "GET", fixtureBase+"/session", localHost, map[string]string{"Sec-Fetch-Site": "same-origin"}, "")
	if code != 200 || sess["nonce"] == nil {
		t.Fatalf("loopback socket session: %d %v", code, sess)
	}
	nonce := sess["nonce"].(string)
	page := map[string]string{"Origin": "http://" + localHost, "Sec-Fetch-Site": "same-origin", "X-Construction-Nonce": nonce, "Content-Type": "application/json"}
	create := `{"schemaVersion":1,"requestId":"create-socket-01","title":"Socket owner problem"}`
	if code, _ := send(local, "POST", fixtureBase+"/problems", localHost, page, create); code != 200 {
		t.Fatalf("loopback socket create: %d", code)
	}
	// the "remote" listener serves the same handler; the forged request names
	// the loopback host exactly as the owner's page does
	for _, c := range []struct{ method, path, body string }{
		{"GET", fixtureBase + "/session", ""},
		{"GET", fixtureBase + "/problems", ""},
		{"POST", fixtureBase + "/problems", `{"schemaVersion":1,"requestId":"create-socket-02","title":"Forged"}`},
	} {
		code, e := send(remote, c.method, c.path, localHost, page, c.body)
		if code != 403 || e["kind"] != "remote-disabled" || e["nonce"] != nil {
			t.Fatalf("remote socket %s %s: %d %v", c.method, c.path, code, e)
		}
	}
	relayed := map[string]string{"Origin": "https://metis.tail1234.ts.net", "Sec-Fetch-Site": "same-origin", "X-Forwarded-For": "100.101.102.103", "Tailscale-User-Login": "owner@example.com"}
	if code, e := send(local, "GET", fixtureBase+"/session", "metis.tail1234.ts.net", relayed, ""); code != 200 || e["nonce"] == nil {
		t.Fatalf("tailscale serve onto loopback: %d %v", code, e)
	}
	list, err := f.srv.construction.store.List(construction.SubjectRef{Kind: "property", ID: "fixture-ooda-house"})
	if err != nil || len(list) != 1 || list[0].Title != "Socket owner problem" {
		t.Fatalf("only the loopback owner's problem exists: %+v %v", list, err)
	}
	f.assertSourcesUntouched(t)
}

// peerListener reports a fixed remote address for every accepted connection,
// as a connection from another machine would.
type peerListener struct {
	net.Listener
	peer net.Addr
}

func (l peerListener) Accept() (net.Conn, error) {
	c, err := l.Listener.Accept()
	if err != nil {
		return nil, err
	}
	return peerConn{c, l.peer}, nil
}

type peerConn struct {
	net.Conn
	peer net.Addr
}

func (c peerConn) RemoteAddr() net.Addr { return c.peer }

// The loopback check reads RemoteAddr, which is the accepted connection's
// address only while nothing in the private handler chain rewrites it from
// a header (as "real IP" middleware does). No non-test file of the server
// package, nor main.go, assigns it or installs such middleware.
func TestConstructionRemoteAddrNeverRewritten(t *testing.T) {
	files, err := filepath.Glob("*.go")
	if err != nil || len(files) == 0 {
		t.Fatalf("server sources: %v", err)
	}
	files = append(files, filepath.Join("..", "main.go"))
	banned := regexp.MustCompile(`\.RemoteAddr\s*=[^=]|RealIP\(|ProxyHeaders\(`)
	scanned := 0
	for _, name := range files {
		if strings.HasSuffix(name, "_test.go") {
			continue
		}
		src, err := os.ReadFile(name)
		if err != nil {
			t.Fatal(err)
		}
		scanned++
		if loc := banned.FindIndex(src); loc != nil {
			t.Fatalf("%s rewrites the request's peer address: %q", name, src[loc[0]:loc[1]])
		}
	}
	if scanned < 10 {
		t.Fatalf("scanned only %d files", scanned)
	}
}

// A transparent TCP forward that terminates on loopback delivers a client's
// bytes from a loopback peer: like any process on the private listener's
// side of the boundary, it is answered as the owner. The session and the
// docs state the boundary as the private listener, not as detection.
func TestConstructionRawTCPForwardIsIndistinguishable(t *testing.T) {
	f := constructionFixture(t)
	srv := httptest.NewServer(f.srv.Handler())
	defer srv.Close()
	upstream := strings.TrimPrefix(srv.URL, "http://")
	relay, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer relay.Close()
	go func() {
		for {
			c, err := relay.Accept()
			if err != nil {
				return
			}
			go func(c net.Conn) {
				defer c.Close()
				up, err := net.Dial("tcp", upstream)
				if err != nil {
					return
				}
				defer up.Close()
				go io.Copy(up, c)
				io.Copy(c, up)
			}(c)
		}
	}()
	client := &http.Client{Transport: &http.Transport{}}
	defer client.CloseIdleConnections()
	req, err := http.NewRequest("GET", "http://"+relay.Addr().String()+fixtureBase+"/session", nil)
	if err != nil {
		t.Fatal(err)
	}
	req.Host = upstream // the client behind the forward names loopback
	res, err := client.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	var sess map[string]any
	_ = json.NewDecoder(res.Body).Decode(&sess)
	if res.StatusCode != 200 || sess["nonce"] == nil {
		t.Fatalf("a request through a raw forward looks local and is answered (the documented limit): %d %v", res.StatusCode, sess)
	}
	caps, _ := sess["capabilities"].(map[string]any)
	if b, _ := sess["boundary"].(string); !strings.Contains(b, "private listener") || caps["accessModel"] != "private-listener" || caps["remoteAccess"] != "tailnet" {
		t.Fatalf("session boundary %q capabilities %v", sess["boundary"], caps)
	}
	doc, err := os.ReadFile(filepath.Join("..", "docs", "construction-intelligence.md"))
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"tailscale serve", "public CDN or tunnel", "DNS-rebinding"} {
		if !strings.Contains(string(doc), want) {
			t.Fatalf("docs/construction-intelligence.md must state %q", want)
		}
	}
}
