package artifacts

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func store(t *testing.T) (*Store, string) {
	t.Helper()
	dir := t.TempDir()
	s, err := New(dir)
	if err != nil {
		t.Fatal(err)
	}
	return s, dir
}

func countBlobs(t *testing.T, dir string) int {
	t.Helper()
	n := 0
	filepath.Walk(filepath.Join(dir, "blobs"), func(p string, fi os.FileInfo, err error) error {
		if err == nil && fi != nil && !fi.IsDir() {
			n++
		}
		return nil
	})
	return n
}

// The point of content addressing: the same bytes cost one blob however many
// people send them — while each sender keeps THEIR filename. (realestate's
// older CAS gets the second half wrong; this is the regression guard.)
func TestIdenticalBytesStoreOnceButKeepEachName(t *testing.T) {
	s, dir := store(t)
	const body = "PROPOSAL\nWM Electric\n$38,500\n"
	a, err := s.Save(strings.NewReader(body), "wm-bid.pdf", "application/pdf")
	if err != nil {
		t.Fatal(err)
	}
	b, err := s.Save(strings.NewReader(body), "electrical quote FINAL.pdf", "application/pdf")
	if err != nil {
		t.Fatal(err)
	}
	if a.Hash != b.Hash {
		t.Fatalf("identical bytes hashed differently: %s vs %s", a.Hash, b.Hash)
	}
	if got := countBlobs(t, dir); got != 1 {
		t.Fatalf("want 1 stored blob, got %d", got)
	}
	if a.Name != "wm-bid.pdf" || b.Name != "electrical quote FINAL.pdf" {
		t.Fatalf("a sender's own name was lost: %q / %q", a.Name, b.Name)
	}
	if a.Size != int64(len(body)) || b.Size != a.Size {
		t.Fatalf("size wrong: %d / %d", a.Size, b.Size)
	}
	// and different bytes are a different blob
	if _, err := s.Save(strings.NewReader(body+"x"), "wm-bid.pdf", "application/pdf"); err != nil {
		t.Fatal(err)
	}
	if got := countBlobs(t, dir); got != 2 {
		t.Fatalf("want 2 blobs after a distinct file, got %d", got)
	}
}

func TestSaveGuards(t *testing.T) {
	s, _ := store(t)
	if _, err := s.Save(strings.NewReader(""), "empty.txt", "text/plain"); err == nil {
		t.Error("empty file accepted")
	}
	big := strings.Repeat("x", MaxBlobSize+1)
	if _, err := s.Save(strings.NewReader(big), "big.bin", ""); err == nil {
		t.Error("oversize accepted")
	}
	// a hostile filename never becomes a path
	r, err := s.Save(strings.NewReader("hi"), "../../etc/passwd", "text/plain")
	if err != nil {
		t.Fatal(err)
	}
	if strings.ContainsAny(SafeExt("../../etc/passwd"), `/\`) {
		t.Error("extension carried a path fragment")
	}
	p := s.BlobPath(r.Hash)
	if p == "" || strings.Contains(p, "..") {
		t.Fatalf("blob path unsafe or missing: %q", p)
	}
}

func TestBlobPathRejectsJunkHashes(t *testing.T) {
	s, _ := store(t)
	for _, h := range []string{"", "xyz", "../../etc/passwd", strings.Repeat("g", 64), strings.Repeat("a", 63)} {
		if got := s.BlobPath(h); got != "" {
			t.Errorf("BlobPath(%q) = %q, want empty", h, got)
		}
	}
}

// One pool, two businesses: a hash absent from a domain's index does not exist
// for that domain, even though the bytes are right there.
func TestDomainIndexIsTheAccessList(t *testing.T) {
	s, _ := store(t)
	ref, err := s.Save(strings.NewReader("aion confidential"), "cap-table.xlsx", "")
	if err != nil {
		t.Fatal(err)
	}
	if err := s.Add("aion", Entry{Ref: ref, By: "Ben", ByEmail: "ben@aion.bio", At: time.Now(), Thread: "th1"}); err != nil {
		t.Fatal(err)
	}
	if !s.Owns("aion", ref.Hash) {
		t.Fatal("aion should own what it uploaded")
	}
	if s.Owns("ooda", ref.Hash) {
		t.Fatal("ooda must NOT resolve an aion hash — one pool leaked across businesses")
	}
	// bad domains and hashes never pass
	for _, d := range []string{"", "../aion", "AION", strings.Repeat("a", 40)} {
		if s.Owns(d, ref.Hash) {
			t.Errorf("Owns(%q) passed", d)
		}
	}
	if err := s.Add("../escape", Entry{Ref: ref, At: time.Now()}); err == nil {
		t.Error("a traversing domain name was accepted")
	}
}

func TestAddIsIdempotentPerThreadAndSender(t *testing.T) {
	s, _ := store(t)
	ref, _ := s.Save(strings.NewReader("plan.pdf bytes"), "plan.pdf", "application/pdf")
	e := Entry{Ref: ref, By: "Ben", ByEmail: "ben@aion.bio", At: time.Now(), Thread: "th1"}
	for i := 0; i < 3; i++ {
		if err := s.Add("aion", e); err != nil {
			t.Fatal(err)
		}
	}
	if got := len(s.List("aion")); got != 1 {
		t.Fatalf("re-attaching the same file to one thread grew the index to %d", got)
	}
	// the same file in another thread IS a separate artifact
	e2 := e
	e2.Thread = "th2"
	if err := s.Add("aion", e2); err != nil {
		t.Fatal(err)
	}
	if got := len(s.List("aion")); got != 2 {
		t.Fatalf("want 2 artifacts across two threads, got %d", got)
	}
	// as is the same file from a different person
	e3 := e
	e3.ByEmail = "heye@aion.bio"
	s.Add("aion", e3)
	if got := len(s.List("aion")); got != 3 {
		t.Fatalf("want 3, got %d", got)
	}
}

func TestExtractsAreCachedPerBlob(t *testing.T) {
	s, _ := store(t)
	ref, _ := s.Save(strings.NewReader("some pdf bytes"), "bid.pdf", "application/pdf")
	if _, ok := s.Extract(ref.Hash); ok {
		t.Fatal("extract present before it was written")
	}
	if err := s.PutExtract(ref.Hash, "PROPOSAL\n$38,500"); err != nil {
		t.Fatal(err)
	}
	got, ok := s.Extract(ref.Hash)
	if !ok || !strings.Contains(got, "38,500") {
		t.Fatalf("extract not cached: %q ok=%v", got, ok)
	}
	// a second upload of the same bytes finds the extract already there —
	// re-uploading a document never re-extracts it
	again, _ := s.Save(strings.NewReader("some pdf bytes"), "copy.pdf", "application/pdf")
	if _, ok := s.Extract(again.Hash); !ok {
		t.Fatal("dedup did not reuse the cached extract")
	}
	if s.ExtractPath("nonsense") != "" {
		t.Error("ExtractPath accepted a bad hash")
	}
}

func TestListIsNewestFirst(t *testing.T) {
	s, _ := store(t)
	now := time.Now()
	for i, name := range []string{"old.pdf", "mid.pdf", "new.pdf"} {
		ref, _ := s.Save(strings.NewReader(name), name, "")
		s.Add("aion", Entry{Ref: ref, At: now.Add(time.Duration(i) * time.Hour), Thread: "t"})
	}
	list := s.List("aion")
	if len(list) != 3 || list[0].Name != "new.pdf" || list[2].Name != "old.pdf" {
		t.Fatalf("wrong order: %+v", list)
	}
	if got := s.List("ooda"); len(got) != 0 {
		t.Fatalf("empty domain returned %d", len(got))
	}
}
