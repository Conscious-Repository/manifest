package sources

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"net/url"
	"regexp"
	"strings"
	"time"
	"unicode"

	"golang.org/x/net/html"
)

// PUBLISHED ADDRESSES (owner ask 2026-09-21). A lab's own people page
// prints its members' institutional addresses. This reads ONE such page and
// answers, for a roster of names the caller already holds, which printed
// address the page's own markup binds to which printed name. It is the
// contact half of D15 done the way D15 allows: an address enters the system
// only as a citation (contact_published), quoted verbatim, from a page the
// owner can open — never as a field, never guessed.
//
// ⚠ NOTHING HERE IS SYNTHESISED. An address is accepted only when it
// appears in the fetched bytes — a `mailto:` href on a visible link, or a
// literal user@host token in page text. No first.last@host is ever built
// from a name. A page that publishes nothing yields nothing.
//
// ⚠ BINDING IS BY MARKUP, NOT BY SURNAME. An address binds to a name when
// the SMALLEST element enclosing the address also holds that name as a
// printed name line (its own heading, cell, caption or paragraph — the same
// "name line" the crawl built the draft from), holds no OTHER printed
// person name, holds exactly one roster name, and shows exactly one
// address. A surname shared by two people never matches anyone: names
// compare whole, under the web identity rule (WebPersonKey), and a
// one-word roster name is not looked for at all. An address printed beside
// two different people is nobody's. A mailto: whose visible text names a
// different mailbox is a conflicting publication and neither is taken. A
// page that names one person in its <h1> and publishes exactly one address
// outside its chrome is the single-candidate case, and binds only when the
// address sits alone on its line.
//
// Same guards as the crawl: webRefuse, the SSRF dialer, robots.txt, the
// honest User-Agent, the pause, the body cap. One page per call, no
// frontier.

// ContactPage is what one people page published, bound to roster names.
type ContactPage struct {
	// URL is where the bytes came from, after redirects.
	URL         string
	RetrievedAt time.Time
	// BodyLen and BodyHash identify the bytes, so a caller reading several
	// pages can see one boilerplate body served under several URLs.
	BodyLen  int
	BodyHash string
	// Named lists the roster names the page prints as a name line outside
	// its chrome — the callers' signal that the page is the roster it was
	// meant to be, rather than a challenge or an error page.
	Named []string
	// Addresses is how many distinct addresses the page publishes outside
	// its chrome, bound or not.
	Addresses int
	// Bound are the addresses the markup tied to a roster name, in document
	// order, each once.
	Bound []ContactBinding
}

// ContactBinding is one address the page bound to one roster name.
type ContactBinding struct {
	// Name is the roster entry, exactly as the caller gave it.
	Name string
	// Printed is the name line as the page prints it ("Dana Reyes, PhD").
	Printed string
	// Address is verbatim from the page: the mailto target, or the token.
	Address string
	// Mailto is set when the address came from a mailto: href.
	Mailto bool
}

// contactAddressRe is the shape a published address must have. Deliberately
// plain: a local part, one @, a dotted host with a lettered TLD.
var contactAddressRe = regexp.MustCompile(`^[A-Za-z0-9._%+\-]+@[A-Za-z0-9\-]+(\.[A-Za-z0-9\-]+)*\.[A-Za-z]{2,}$`)

// contactNotTLDs are "TLDs" that mark an asset name, not a mailbox
// ("hero@2x.png").
var contactNotTLDs = map[string]bool{
	"png": true, "jpg": true, "jpeg": true, "gif": true, "svg": true, "webp": true,
	"js": true, "css": true, "html": true, "htm": true, "php": true, "json": true,
}

// contactLabelMaxWords bounds what may share a line with a single-candidate
// page's address besides the person's own name: "Email:", "E-mail address".
const contactLabelMaxWords = 2

// contactAttempts and contactRetryBase bound the retry on a rate-limited
// host: 5 s, then 10 s, then the error stands. A pass is idempotent, so a
// page that stays throttled is simply read next time.
const (
	contactAttempts  = 3
	contactRetryBase = 5 * time.Second
)

func contactTransient(err error) bool {
	msg := err.Error()
	return strings.Contains(msg, "HTTP 429") || strings.Contains(msg, "HTTP 503")
}

// LookupContacts reads one people page and binds its published addresses to
// the roster. The roster is every name the caller is triaging from that
// page (the run's queue), given verbatim; names of fewer than two words are
// ignored rather than matched loosely. A fetch that fails is an error; a
// page that reads but names nobody is a ContactPage with Named empty, which
// the caller treats as unavailable.
func (w Web) LookupContacts(ctx context.Context, rawURL string, roster []string) (ContactPage, error) {
	out := ContactPage{URL: strings.TrimSpace(rawURL)}
	u, err := url.Parse(out.URL)
	if err != nil || u.Host == "" || (u.Scheme != "http" && u.Scheme != "https") {
		return out, fmt.Errorf("web: %q is not an http(s) URL", rawURL)
	}
	if why := webRefuse(u); why != "" {
		return out, fmt.Errorf("web: %s", why)
	}
	if !w.allowedByRobots(ctx, map[string]*webRobots{}, u) {
		return out, fmt.Errorf("web: %s asks not to be fetched (robots.txt)", u.Host)
	}
	var body []byte
	var final *url.URL
	var doc *html.Node
	for attempt := 0; ; attempt++ {
		w.pause(ctx)
		body, final, doc, err = w.fetchDoc(ctx, u)
		// a host that says "too many requests" is asked again, later, a
		// bounded number of times — the same fetch, the same guards, not a
		// second fetcher; anything else is the page's answer
		if err == nil || attempt+1 >= contactAttempts || !contactTransient(err) {
			break
		}
		if werr := scholarlyWait(ctx, contactRetryBase<<attempt); werr != nil {
			return out, err
		}
	}
	if err != nil {
		return out, err
	}
	sum := sha256.Sum256(body)
	out.URL, out.RetrievedAt, out.BodyLen, out.BodyHash = final.String(), time.Now().UTC(), len(body), hex.EncodeToString(sum[:])

	keys := map[string]string{} // WebPersonKey → roster name as given
	for _, name := range roster {
		name = strings.TrimSpace(name)
		key := WebPersonKey(name)
		if len(strings.Fields(key)) < 2 {
			continue // a surname alone is never looked for
		}
		if _, dup := keys[key]; !dup {
			keys[key] = name
		}
	}
	page := &webPage{url: final}
	page.extract(doc)
	whole := contactSubjects(page.lines, keys)
	for _, s := range whole.subjects {
		out.Named = append(out.Named, s.name)
	}
	if len(keys) == 0 {
		return out, nil
	}

	occ := contactOccurrences(doc)
	distinct := map[string]bool{}
	// every element's distinct visible addresses, so a binding element that
	// holds two people's addresses — one of them under a heading the crawl
	// could not read as a name — is recognised as a shared container
	held := map[*html.Node]map[string]bool{}
	for _, o := range occ {
		addr := strings.ToLower(o.address)
		distinct[addr] = true
		for cur := o.node; cur != nil; cur = cur.Parent {
			if held[cur] == nil {
				held[cur] = map[string]bool{}
			}
			held[cur][addr] = true
		}
	}
	out.Addresses = len(distinct)

	cache := map[*html.Node]contactCard{}
	seenBinding := map[string]bool{}
	names := map[string]map[string]bool{} // address → roster keys it was bound to
	var bound []ContactBinding
	for _, o := range occ {
		b, ok := contactBind(o, page.url, keys, cache, held, whole, out.Addresses)
		if !ok {
			continue
		}
		addr := strings.ToLower(b.Address)
		k := WebPersonKey(b.Name) + "\x00" + addr
		if seenBinding[k] {
			continue
		}
		seenBinding[k] = true
		if names[addr] == nil {
			names[addr] = map[string]bool{}
		}
		names[addr][WebPersonKey(b.Name)] = true
		bound = append(bound, b)
	}
	// one address printed beside two different people is nobody's
	for _, b := range bound {
		if len(names[strings.ToLower(b.Address)]) == 1 {
			out.Bound = append(out.Bound, b)
		}
	}
	return out, nil
}

// contactOccurrence is one address as it appears in the document: the
// element to start climbing from, and the text of the block line it sits in.
type contactOccurrence struct {
	node    *html.Node
	address string
	mailto  bool
}

// contactOccurrences collects every published address outside chrome and
// outside skipped tags: a visible mailto: link (an anchor with no text is a
// CMS leftover nobody can click, and is not published to a reader), or a
// literal token in a text node.
func contactOccurrences(doc *html.Node) []contactOccurrence {
	var out []contactOccurrence
	var walk func(n *html.Node, chrome bool)
	walk = func(n *html.Node, chrome bool) {
		switch n.Type {
		case html.TextNode:
			if chrome {
				return
			}
			for _, tok := range contactTokens(n.Data) {
				if n.Parent != nil {
					out = append(out, contactOccurrence{node: n.Parent, address: tok})
				}
			}
			return
		case html.ElementNode:
		default:
			for c := n.FirstChild; c != nil; c = c.NextSibling {
				walk(c, chrome)
			}
			return
		}
		if webSkipTags[n.Data] {
			return
		}
		if !chrome && webChromeNode(n) {
			chrome = true
		}
		if n.Data == "a" && !chrome {
			if addr := contactMailto(webAttr(n, "href")); addr != "" {
				text := strings.TrimSpace(webText(n))
				if text == "" {
					return // an anchor nobody can see is not published to a reader
				}
				// the link's own text may print the address (often with an
				// anti-spam decoy in the host: "a.b@nospam.example"); a text
				// that names a DIFFERENT mailbox is a conflicting publication
				// and neither is taken
				for _, shown := range contactTokens(text) {
					if !strings.EqualFold(contactLocal(shown), contactLocal(addr)) {
						return
					}
				}
				out = append(out, contactOccurrence{node: n, address: addr, mailto: true})
				return // the text, where it repeats the address, is the same occurrence
			}
		}
		for c := n.FirstChild; c != nil; c = c.NextSibling {
			walk(c, chrome)
		}
	}
	walk(doc, false)
	return out
}

// contactMailto returns the single address a mailto: href names, or "".
// A list of recipients is nobody's address in particular.
func contactMailto(href string) string {
	href = strings.TrimSpace(href)
	if len(href) < len("mailto:") || !strings.EqualFold(href[:len("mailto:")], "mailto:") {
		return ""
	}
	rest := href[len("mailto:"):]
	if i := strings.IndexByte(rest, '?'); i >= 0 {
		rest = rest[:i]
	}
	if un, err := url.PathUnescape(rest); err == nil {
		rest = un
	}
	rest = strings.TrimSpace(rest)
	if strings.Contains(rest, ",") || strings.Contains(rest, ";") || !contactAddress(rest) {
		return ""
	}
	return rest
}

// contactTokens returns the address-shaped tokens in a run of text.
func contactTokens(text string) []string {
	if !strings.Contains(text, "@") {
		return nil
	}
	var out []string
	for _, tok := range strings.FieldsFunc(text, func(r rune) bool {
		return unicode.IsSpace(r) || strings.ContainsRune("<>()[]{},;:\"'|", r)
	}) {
		tok = strings.TrimRight(tok, ".")
		if contactAddress(tok) {
			out = append(out, tok)
		}
	}
	return out
}

// contactLocal is the mailbox part before the @.
func contactLocal(addr string) string {
	if i := strings.IndexByte(addr, '@'); i >= 0 {
		return addr[:i]
	}
	return addr
}

func contactAddress(s string) bool {
	if !contactAddressRe.MatchString(s) {
		return false
	}
	tld := strings.ToLower(s[strings.LastIndexByte(s, '.')+1:])
	return !contactNotTLDs[tld]
}

// contactSubject is one roster name printed as a name line.
type contactSubject struct {
	key, name, printed string
	level              int
}

// contactCard is what one element's subtree prints: the roster names it
// holds as name lines, and how many OTHER person-shaped name lines it holds.
type contactCard struct {
	subjects []contactSubject
	foreign  int
	lines    []webLine
}

// contactSubjects scans block lines for name lines (webNameLine — the same
// rule the crawl attributed a title with) and sorts them into roster
// subjects and strangers. Chrome lines are ignored. Each subject once.
func contactSubjects(lines []webLine, keys map[string]string) contactCard {
	card := contactCard{lines: lines}
	seen := map[string]bool{}
	for _, l := range lines {
		if l.chrome {
			continue
		}
		name, _ := webNameLine(l.text)
		if name == "" {
			continue
		}
		key := WebPersonKey(name)
		if roster, ok := keys[key]; ok {
			if !seen[key] {
				seen[key] = true
				card.subjects = append(card.subjects, contactSubject{key: key, name: roster, printed: name, level: l.level})
			}
			continue
		}
		card.foreign++
	}
	return card
}

// contactBind climbs from an address to the smallest element whose subtree
// prints a person as a name line, and binds when that element prints
// exactly one roster name and no stranger. The climb stops — unbound — at
// the first element that prints anyone at all when that anyone is two
// people, a stranger, or ambiguous. Reaching the document itself is the
// single-candidate case and carries the stricter rules described above.
func contactBind(o contactOccurrence, base *url.URL, keys map[string]string, cache map[*html.Node]contactCard, held map[*html.Node]map[string]bool, whole contactCard, pageAddresses int) (ContactBinding, bool) {
	for cur := o.node; cur != nil; cur = cur.Parent {
		if cur.Type != html.ElementNode && cur.Type != html.DocumentNode {
			continue
		}
		top := cur.Type == html.DocumentNode || cur.Data == "html" || cur.Data == "body"
		var card contactCard
		if top {
			card = whole
		} else {
			var ok bool
			if card, ok = cache[cur]; !ok {
				sub := &webPage{url: base}
				sub.extract(cur)
				card = contactSubjects(sub.lines, keys)
				cache[cur] = card
			}
		}
		if len(card.subjects) == 0 && card.foreign == 0 {
			continue
		}
		// exactly one roster person, no stranger's name line, and exactly
		// one visible address: a card that shows two mailboxes is two
		// people's, whatever the crawl made of the second heading
		if len(card.subjects) != 1 || card.foreign != 0 || len(held[cur]) != 1 {
			return ContactBinding{}, false
		}
		s := card.subjects[0]
		if top || s.level == 1 {
			// the single-candidate case — the binding element is the page,
			// or the name is the page's own h1 (a page about one person,
			// however it is wrapped): one address on the whole page, alone
			// on its line. A card headed h2 or lower is a roster entry and
			// takes the card rule above.
			if s.level != 1 || pageAddresses != 1 || !contactAlone(o, base, s) {
				return ContactBinding{}, false
			}
		}
		return ContactBinding{Name: s.name, Printed: s.printed, Address: o.address, Mailto: o.mailto}, true
	}
	return ContactBinding{}, false
}

// contactAlone reports whether the address's own block line carries
// nothing but the address, the subject's printed name, and at most a short
// label ("Email:"). A line that introduces somebody else — "for inquiries
// contact Jane Admin: …" — fails it.
func contactAlone(o contactOccurrence, base *url.URL, s contactSubject) bool {
	block := o.node
	for block != nil && block.Type == html.ElementNode && !webBlockTags[block.Data] {
		block = block.Parent
	}
	if block == nil {
		return false
	}
	sub := &webPage{url: base}
	sub.extract(block)
	var text []string
	for _, l := range sub.lines {
		text = append(text, l.text)
	}
	rest := strings.Join(text, " ")
	rest = strings.ReplaceAll(rest, o.address, " ")
	rest = strings.ReplaceAll(rest, s.printed, " ")
	rest = strings.Map(func(r rune) rune {
		if unicode.IsLetter(r) || unicode.IsDigit(r) {
			return r
		}
		return ' '
	}, rest)
	return len(strings.Fields(rest)) <= contactLabelMaxWords
}
