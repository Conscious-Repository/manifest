package mdfm

import (
	"reflect"
	"testing"
)

func TestSplitAndBody(t *testing.T) {
	fm, body := Split("---\nid: abc\ntype: paper\ntags: [a, b]\n---\n\nhello world\n")
	if fm["id"] != "abc" || fm["type"] != "paper" {
		t.Fatalf("frontmatter parse: %+v", fm)
	}
	if got := List(fm["tags"]); !reflect.DeepEqual(got, []string{"a", "b"}) {
		t.Fatalf("tags: %v", got)
	}
	if body != "hello world\n" {
		t.Fatalf("body: %q", body)
	}
}

func TestSplitNoFrontmatter(t *testing.T) {
	fm, body := Split("just a body")
	if len(fm) != 0 || body != "just a body" {
		t.Fatalf("expected empty fm + verbatim body, got %+v / %q", fm, body)
	}
}

func TestWriterRoundTrip(t *testing.T) {
	doc := (&Writer{}).
		Set("id", "x1").
		Set("type", "person").
		Set("empty", ""). // skipped
		SetList("tags", []string{"bio", "aging"}).
		String("the body")
	fm, body := Split(doc)
	if fm["id"] != "x1" || fm["type"] != "person" {
		t.Fatalf("round-trip fm: %+v", fm)
	}
	if _, ok := fm["empty"]; ok {
		t.Fatal("empty field should have been skipped")
	}
	if got := List(fm["tags"]); !reflect.DeepEqual(got, []string{"bio", "aging"}) {
		t.Fatalf("tags round-trip: %v", got)
	}
	if body != "the body\n" {
		t.Fatalf("body round-trip: %q", body)
	}
}

func TestWriterEmptyBody(t *testing.T) {
	doc := (&Writer{}).Set("id", "x").String("")
	if doc != "---\nid: x\n---\n" {
		t.Fatalf("empty-body doc: %q", doc)
	}
}
