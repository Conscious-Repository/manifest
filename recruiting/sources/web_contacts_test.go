package sources

import (
	"context"
	"strings"
	"testing"
)

// contactsRoster is a lab people page with the shapes the matcher has to
// get right: a card with a visible mailto (Ellie) sharing a column with a
// card whose heading the crawl cannot read as a name (a pronoun suffix) but
// which shows its own address; a card with a literal address (Cory) — the
// two Berklands share a surname; a card whose only mailto is an empty anchor
// (Dana); a card that prints two people over one address (Lu Xu / Mack
// Yang); a mailto whose text prints the address with an anti-spam decoy in
// the host (Guy); a mailto whose text names a different mailbox than its
// href (Ram); one office address printed beside two people (Amit,
// Gretchen); a general inbox in a paragraph of its own; and two addresses
// in chrome.
const contactsRoster = `<html><head><title>Example Lab · People</title></head><body>
<nav><a href="/">Home</a> <a href="mailto:lab@example.test">Contact the lab</a></nav>
<main>
<h1>Lab Members</h1>
<div class="column">
  <div class="card"><h2>Ellie Berkland</h2><p>Undergraduate Research Assistant</p>
    <p><a href="mailto:ellie@example.test">ellie@example.test</a></p></div>
  <div class="card"><h2>Austin Kellogg (They/He), B.S.</h2><p>BME PhD Student</p>
    <p><a href="mailto:a%2ec.kellogg&#64;example.test">a.c.kellogg@<b>nospam.</b>example.test</a></p></div>
</div>
<div class="card"><h2>Cory Berkland, PhD</h2><p>Professor</p><p>Email: cory@example.test</p></div>
<div class="card"><h2>Dana Reyes</h2><p>Postdoctoral Fellow</p><p><a href="mailto:hidden@example.test"></a></p></div>
<div class="card"><h2>Lu Xu</h2><p>Graduate Student</p><h2>Mack Yang</h2><p>Graduate Student</p>
  <p><a href="mailto:shared@example.test">shared@example.test</a></p></div>
<div class="card"><h2>Guy Genin</h2><p>Professor</p>
  <p><span class="screen-reader-text">Email: </span><a href="mailto:genin&#64;example.test">genin@<b>nospam.</b>example.test</a></p></div>
<div class="card"><h2>Ram Dixit</h2><p>Professor of Biology</p>
  <p><a href="mailto:pi@example.test">ramdixit@example.test</a></p></div>
<div class="card"><h2>Amit Pathak</h2><p>Associate Professor</p><p><a href="mailto:office@example.test">office@example.test</a></p></div>
<div class="card"><h2>Gretchen Meyer</h2><p>Assistant Professor</p><p><a href="mailto:office@example.test">office@example.test</a></p></div>
<p>Berkland lab general inbox: berklandlab@example.test</p>
</main>
<footer>Contact: webmaster@example.test</footer>
</body></html>`

func contactsNet() *webNet {
	return newWebNet().site("lab.example", map[string]string{
		"/robots.txt": "User-agent: *\nDisallow: /private/\n",
		"/people/":    contactsRoster,
		"/people/dana/": `<html><body><nav><a href="/people/">People</a></nav><main><h1>Dana Reyes</h1>
<p>Postdoctoral Fellow, Example Imaging Group</p><div class="contact"><p>Email: dana@example.test</p></div></main></body></html>`,
		"/people/cory/": `<html><body><main><h1>Cory Berkland, PhD</h1><p>Professor</p>
<p>For inquiries contact Jane Admin: jadmin@example.test</p></main></body></html>`,
		"/people/ellie/": `<html><body><main><h1>Ellie Berkland</h1><p>Undergraduate Research Assistant</p>
<p>ellie@example.test</p><p>Advisor: cory@example.test</p></main></body></html>`,
		"/private/staff/": contactsRoster,
		"/challenge/":     `<html><body><p>Just a moment — checking your browser before accessing the site.</p></body></html>`,
	})
}

var contactsNames = []string{"Ellie Berkland", "Cory Berkland", "Dana Reyes", "Lu Xu", "Mack Yang", "Berkland",
	"Guy Genin", "Ram Dixit", "Amit Pathak", "Gretchen Meyer"}

func bindings(p ContactPage) map[string]string {
	out := map[string]string{}
	for _, b := range p.Bound {
		out[b.Name] = out[b.Name] + b.Address + ";"
	}
	return out
}

// The conservative matcher: a shared surname binds nobody, a two-person
// card binds nobody, an invisible mailto is not published, chrome is not
// read, and a one-word roster name is never looked for.
func TestWebContactsBindByCardNotSurname(t *testing.T) {
	n := contactsNet()
	got, err := n.adapter().LookupContacts(context.Background(), "https://lab.example/people/", contactsNames)
	if err != nil {
		t.Fatal(err)
	}
	b := bindings(got)
	if b["Ellie Berkland"] != "ellie@example.test;" {
		t.Errorf("Ellie: %q", b["Ellie Berkland"])
	}
	if b["Cory Berkland"] != "cory@example.test;" {
		t.Errorf("Cory: %q", b["Cory Berkland"])
	}
	if b["Guy Genin"] != "genin@example.test;" {
		t.Errorf("Guy (decoy host in the link text, real address in the href): %q", b["Guy Genin"])
	}
	for _, name := range []string{"Dana Reyes", "Lu Xu", "Mack Yang", "Berkland", "Ram Dixit", "Amit Pathak", "Gretchen Meyer"} {
		if b[name] != "" {
			t.Errorf("%s bound %q — a surname, a shared card, an invisible link, a conflicting link or a shared address is not a binding", name, b[name])
		}
	}
	if len(got.Bound) != 3 {
		t.Errorf("bound %d, want 3: %+v", len(got.Bound), got.Bound)
	}
	for _, x := range got.Bound {
		for _, never := range []string{"berklandlab", "webmaster", "lab@", "hidden", "kellogg", "nospam", "pi@", "office"} {
			if strings.Contains(x.Address, never) {
				t.Errorf("address outside its own card was bound: %+v", x)
			}
		}
	}
	if got.Addresses != 7 {
		t.Errorf("addresses outside chrome = %d, want 7 (ellie, kellogg, cory, shared, genin, office, general)", got.Addresses)
	}
	if strings.Join(got.Named, ",") != "Ellie Berkland,Cory Berkland,Dana Reyes,Lu Xu,Mack Yang,Guy Genin,Ram Dixit,Amit Pathak,Gretchen Meyer" {
		t.Errorf("named: %v", got.Named)
	}
	if got.BodyHash == "" || got.BodyLen != len(contactsRoster) {
		t.Errorf("body identity: %d %q", got.BodyLen, got.BodyHash)
	}
	for _, r := range n.requests() {
		if !strings.HasPrefix(r, "https://lab.example/") {
			t.Errorf("request left the page's host: %s", r)
		}
	}
}

// The single-candidate page: an h1 naming one roster person and exactly one
// address alone on its line binds; a line introducing somebody else, or a
// second address on the page, does not.
func TestWebContactsSingleCandidatePage(t *testing.T) {
	n := contactsNet()
	w := n.adapter()
	got, err := w.LookupContacts(context.Background(), "https://lab.example/people/dana/", contactsNames)
	if err != nil {
		t.Fatal(err)
	}
	if b := bindings(got); b["Dana Reyes"] != "dana@example.test;" || len(got.Bound) != 1 {
		t.Errorf("dana's own page: %+v", got.Bound)
	}
	got, err = w.LookupContacts(context.Background(), "https://lab.example/people/cory/", contactsNames)
	if err != nil {
		t.Fatal(err)
	}
	if len(got.Bound) != 0 {
		t.Errorf("an inquiries line naming somebody else was bound: %+v", got.Bound)
	}
	got, err = w.LookupContacts(context.Background(), "https://lab.example/people/ellie/", contactsNames)
	if err != nil {
		t.Fatal(err)
	}
	if len(got.Bound) != 0 {
		t.Errorf("a page with two addresses bound one of them: %+v", got.Bound)
	}
}

// A challenge page names nobody: Named is empty, nothing is bound, and the
// caller has what it needs to call the page unavailable. The bytes identify
// it so the same body under another URL is recognisable.
func TestWebContactsBoilerplateNamesNobody(t *testing.T) {
	n := contactsNet()
	got, err := n.adapter().LookupContacts(context.Background(), "https://lab.example/challenge/", contactsNames)
	if err != nil {
		t.Fatal(err)
	}
	if len(got.Named) != 0 || len(got.Bound) != 0 || got.Addresses != 0 || got.BodyHash == "" {
		t.Errorf("challenge page: %+v", got)
	}
}

// Same guards as the crawl: robots.txt, the refuse list, the host policy.
func TestWebContactsKeepsTheCrawlGuards(t *testing.T) {
	n := contactsNet()
	w := n.adapter()
	if _, err := w.LookupContacts(context.Background(), "https://lab.example/private/staff/", contactsNames); err == nil || !strings.Contains(err.Error(), "robots") {
		t.Errorf("robots.txt not honoured: %v", err)
	}
	for _, p := range n.pages() {
		if strings.Contains(p, "/private/") {
			t.Errorf("disallowed path fetched: %s", p)
		}
	}
	for _, bad := range []string{"https://www.linkedin.com/in/someone", "http://127.0.0.1/people/", "ftp://lab.example/people/", "https://lab.example/login"} {
		if _, err := w.LookupContacts(context.Background(), bad, contactsNames); err == nil {
			t.Errorf("%s was not refused", bad)
		}
	}
	if len(n.leaks) != 0 {
		t.Errorf("requests leaked to unmapped hosts: %v", n.leaks)
	}
}

func TestContactMailtoAndTokens(t *testing.T) {
	for href, want := range map[string]string{
		"mailto:a.b@example.test":                 "a.b@example.test",
		"MAILTO:A@example.test?subject=hi":        "A@example.test",
		"mailto:a%40example.test":                 "a@example.test",
		"mailto:a@example.test,b@example.test":    "",
		"mailto:not-an-address":                   "",
		"https://example.test/mailto:a@b.example": "",
		"mailto:hero@2x.png":                      "",
	} {
		if got := contactMailto(href); got != want {
			t.Errorf("contactMailto(%q)=%q want %q", href, got, want)
		}
	}
	got := contactTokens("Write to dana@example.test, or (cory@example.test). Not @handle nor a@b or img@2x.png.")
	if strings.Join(got, " ") != "dana@example.test cory@example.test" {
		t.Errorf("tokens: %v", got)
	}
}
