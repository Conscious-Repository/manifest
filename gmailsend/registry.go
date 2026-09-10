package gmailsend

import (
	"fmt"
	"net/mail"
	"path/filepath"
	"strings"
)

// Registry maps work domains to explicit send-only accounts. Recipient inference
// is allowed only for known, unambiguous domains; external recipients need the
// caller's work domain. There is no global recruiting fallback.
type Registry struct{ Aion, Ooda *Client }

func NewRegistry(data string) *Registry {
	return &Registry{New("ben@aion.bio", TokenPath(data)), New("ben@ooda.group", filepath.Join(data, "gmail-send", "ooda", "token.json"))}
}
func MailDomain(domain string) string {
	switch strings.ToLower(strings.TrimSpace(domain)) {
	case "aion", "aion.bio", "recruiting":
		return "aion"
	case "ooda", "ooda.group", "real-estate", "real estate":
		return "ooda"
	case "personal":
		return "personal"
	}
	return ""
}
func (r *Registry) Resolve(domain string, recipients []string) (*Client, error) {
	key := MailDomain(domain)
	if strings.TrimSpace(domain) != "" && key == "" {
		return nil, fmt.Errorf("no sender mapping for domain %q", domain)
	}
	if key == "" {
		for _, recipient := range recipients {
			addr, err := mail.ParseAddress(recipient)
			if err != nil {
				return nil, fmt.Errorf("invalid recipient")
			}
			parts := strings.Split(addr.Address, "@")
			candidate := MailDomain(parts[len(parts)-1])
			if candidate == "" || key != "" && candidate != key {
				return nil, fmt.Errorf("choose the correspondence domain; recipients do not identify one mapped sender")
			}
			key = candidate
		}
	}
	if key == "personal" {
		return nil, fmt.Errorf("personal Outlook sender is not connected; no email sent")
	}
	if r == nil {
		return nil, fmt.Errorf("mail senders are unavailable")
	}
	var c *Client
	if key == "aion" {
		c = r.Aion
	}
	if key == "ooda" {
		c = r.Ooda
	}
	if c == nil {
		return nil, fmt.Errorf("no sender mapped; choose AION or OODA correspondence")
	}
	expected := "ben@aion.bio"
	if key == "ooda" {
		expected = "ben@ooda.group"
	}
	if c.Sender() != expected {
		return nil, fmt.Errorf("configured sender does not match the domain mapping")
	}
	return c, nil
}
