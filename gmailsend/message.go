package gmailsend

import (
	"crypto/rand"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"mime"
	"net/mail"
	"strings"
	"time"
)

// Message is one outbound text/plain email. Date and MessageID are filled
// when empty; a caller that sets both gets a byte-deterministic build,
// which is what the tests pin.
type Message struct {
	From       string
	FromName   string
	To         []string
	Cc         []string
	Subject    string
	Body       string
	InReplyTo  string
	References string
	Date       time.Time
	MessageID  string
}

// Build renders the RFC 5322 message: From, To, Cc, Subject, Date,
// Message-ID, MIME headers, In-Reply-To/References when threading, and a
// text/plain UTF-8 body with CRLF line endings. Addresses are validated
// before anything is rendered — a malformed recipient never reaches the
// wire.
func Build(m Message) ([]byte, error) {
	from := strings.TrimSpace(m.From)
	if from == "" {
		return nil, errors.New("a message needs a From address")
	}
	if _, err := mail.ParseAddress(from); err != nil {
		return nil, fmt.Errorf("bad From address %q", from)
	}
	to := cleanAddrs(m.To)
	if len(to) == 0 {
		return nil, errors.New("a message needs at least one recipient")
	}
	for _, a := range append(append([]string{}, to...), cleanAddrs(m.Cc)...) {
		if _, err := mail.ParseAddress(a); err != nil {
			return nil, fmt.Errorf("bad recipient address %q", a)
		}
	}
	if strings.TrimSpace(m.Subject) == "" {
		return nil, errors.New("a message needs a subject")
	}
	if strings.TrimSpace(m.Body) == "" {
		return nil, errors.New("a message needs a body")
	}
	date := m.Date
	if date.IsZero() {
		date = time.Now()
	}
	id := strings.TrimSpace(m.MessageID)
	if id == "" {
		id = NewMessageID(from)
	}

	var b strings.Builder
	line := func(k, v string) {
		b.WriteString(k)
		b.WriteString(": ")
		b.WriteString(v)
		b.WriteString("\r\n")
	}
	fromHdr := from
	if name := strings.TrimSpace(m.FromName); name != "" {
		fromHdr = (&mail.Address{Name: name, Address: from}).String()
	}
	line("From", fromHdr)
	line("To", strings.Join(to, ", "))
	if cc := cleanAddrs(m.Cc); len(cc) > 0 {
		line("Cc", strings.Join(cc, ", "))
	}
	line("Subject", mime.QEncoding.Encode("utf-8", strings.TrimSpace(m.Subject)))
	line("Date", date.UTC().Format(time.RFC1123Z))
	line("Message-ID", id)
	if r := strings.TrimSpace(m.InReplyTo); r != "" {
		line("In-Reply-To", r)
	}
	if r := strings.TrimSpace(m.References); r != "" {
		line("References", r)
	}
	line("MIME-Version", "1.0")
	line("Content-Type", "text/plain; charset=\"UTF-8\"")
	line("Content-Transfer-Encoding", "8bit")
	b.WriteString("\r\n")
	body := strings.ReplaceAll(strings.ReplaceAll(m.Body, "\r\n", "\n"), "\n", "\r\n")
	b.WriteString(body)
	if !strings.HasSuffix(body, "\r\n") {
		b.WriteString("\r\n")
	}
	return []byte(b.String()), nil
}

// EncodeRaw is the Gmail API's `raw` encoding: base64url without padding.
func EncodeRaw(raw []byte) string { return base64.RawURLEncoding.EncodeToString(raw) }

// DecodeRaw reverses EncodeRaw (tests read the wire back).
func DecodeRaw(s string) ([]byte, error) { return base64.RawURLEncoding.DecodeString(s) }

// NewMessageID mints an RFC 5322 Message-ID at the sender's domain.
func NewMessageID(from string) string {
	domain := "manifest.local"
	if i := strings.LastIndex(from, "@"); i >= 0 && i < len(from)-1 {
		domain = from[i+1:]
	}
	b := make([]byte, 12)
	_, _ = rand.Read(b)
	return "<" + hex.EncodeToString(b) + "@" + domain + ">"
}

func cleanAddrs(in []string) []string {
	var out []string
	for _, a := range in {
		if a = strings.TrimSpace(a); a != "" {
			out = append(out, a)
		}
	}
	return out
}
