package teamportal

import (
	"net/url"
	"strings"
	"testing"
)

func TestOodaNoticeThreadLinks(t *testing.T) {
	b := &Bridge{name: "ooda-portal", link: "https://portal.ooda.group/#work"}
	for _, id := range []string{"5ea3a00d", "prop/748-n-euclid#shell/roof", "team/an item?x=1"} {
		got := b.noticeURL(Entry{Payload: map[string]any{"item": id}})
		encoded := strings.TrimPrefix(got, "https://portal.ooda.group/#/thread/")
		decoded, err := url.PathUnescape(encoded)
		if err != nil || decoded != id {
			t.Fatalf("link failed for %q: %s", id, got)
		}
	}
	if got := b.noticeURL(Entry{}); got != b.link {
		t.Fatal(got)
	}
	b.name = "aion-portal"
	if got := b.noticeURL(Entry{Payload: map[string]any{"item": "x"}}); got != b.link {
		t.Fatal("changed AION routing")
	}
}
