package personalemail

import (
	"strings"
	"testing"

	"manifest/approvals"
	"manifest/gmailsync"
)

type mapResolver map[string]string

func (m mapResolver) PersonByEmail(e string) (string, bool) {
	n, ok := m[strings.ToLower(e)]
	return n, ok
}

// Everyone on the thread is named: people with a note are links, everyone
// else a plain name on its own "no contact note" line — never a guessed link;
// the mailbox owner and automated senders are not people.
func TestParticipantsNameEveryoneWithoutGuessing(t *testing.T) {
	r := mapResolver{"rj@aion.bio": "rj tevonian"}
	msgs := []gmailsync.Msg{
		{From: `"Jon Chu" <Jon.Chu@khoslaventures.com>`, To: "Ben <ben@aion.bio>", Cc: "RJ Tevonian <RJ@aion.bio>, Raquel Colom <raquel@khoslaventures.com>"},
		{From: "Raquel Colom <raquel@khoslaventures.com>", To: "ben@aion.bio, calendar-notification@google.com"},
		{From: "Zoom <no-reply@zoom.us>", To: "ben@aion.bio"},
	}
	got := participants(msgs, r, "ben@aion.bio")
	want := "[[rj tevonian]]\n" + approvals.UnlinkedPeoplePrefix + "Jon Chu · Raquel Colom"
	if got != want {
		t.Fatalf("participants =\n%q\nwant\n%q", got, want)
	}
	if got := participants(msgs[2:], r, "ben@aion.bio"); got != "" {
		t.Fatalf("an automated sender is nobody: %q", got)
	}
}
