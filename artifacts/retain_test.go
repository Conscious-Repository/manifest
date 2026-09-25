package artifacts

import (
	"errors"
	"sync"
	"testing"
)

func TestRetainPreservesEditedHead(t *testing.T) {
	pool, err := New(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	r, err := NewRegistry(pool)
	if err != nil {
		t.Fatal(err)
	}
	p := Put{Ref: "output.md", Content: []byte("captured"), Provenance: Provenance{Delivery: "request-one"}}
	first, err := r.Retain(p)
	if err != nil {
		t.Fatal(err)
	}
	if p.Provenance.IsZero() {
		t.Fatal("delivery-only provenance lost")
	}
	edited, err := r.Put(Put{ID: first.Artifact.ID, Ref: "renamed-output.md", Content: []byte("owner edit")})
	if err != nil {
		t.Fatal(err)
	}
	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			got, err := r.Retain(p)
			if err != nil || got.Revision.Hash != first.Revision.Hash || got.Artifact.Head != edited.Artifact.Head {
				t.Errorf("capture retry: %+v %v", got, err)
			}
		}()
	}
	wg.Wait()
	got, _ := r.Get(first.Artifact.ID)
	if len(got.Revisions) != 2 || got.Head != edited.Artifact.Head || got.Provenance.Delivery != "request-one" {
		t.Fatal(got)
	}
	p.Content = []byte("different capture")
	p.Ref = "renamed-output.md"
	if _, err := r.Retain(p); !errors.Is(err, ErrRevisionConflict) {
		t.Fatal("source identity reused", err)
	}
}
