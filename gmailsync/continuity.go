package gmailsync

import (
	"context"
	"fmt"
	"net/url"
	"strconv"
)

// ThreadAnchor checks identity and an existing message boundary without fetching
// bodies, headers, or advancing Gmail history. It uses the canonical GET transport.
func (c *Client) ThreadAnchor(ctx context.Context, id, message string, internalMS int64) error {
	if c.mailbox == "" || id == "" || message == "" || internalMS <= 0 {
		return fmt.Errorf("explicit mailbox and anchor required")
	}
	u := "https://gmail.googleapis.com/gmail/v1/users/" + url.PathEscape(c.mailbox) + "/threads/" + url.PathEscape(id) + "?format=minimal&fields=id,messages(id,internalDate)"
	var tr struct {
		ID       string `json:"id"`
		Messages []struct {
			ID           string `json:"id"`
			InternalDate string `json:"internalDate"`
		} `json:"messages"`
	}
	if err := c.get(ctx, u, &tr); err != nil {
		return fmt.Errorf("Gmail continuity read failed")
	}
	if tr.ID != id {
		return fmt.Errorf("Gmail thread identity mismatch")
	}
	for _, m := range tr.Messages {
		if m.ID == message {
			n, err := strconv.ParseInt(m.InternalDate, 10, 64)
			if err == nil && n == internalMS {
				return nil
			}
			return fmt.Errorf("Gmail message boundary mismatch")
		}
	}
	return fmt.Errorf("Gmail message boundary missing")
}
