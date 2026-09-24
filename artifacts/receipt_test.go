package artifacts

import (
	"errors"
	"sync"
	"testing"
)

func TestConditionalSaveReceipts(t *testing.T) {
	r, _ := registry(t)
	first, err := r.Put(Put{Ref: "file.txt", Content: []byte("first")})
	if err != nil {
		t.Fatal(err)
	}
	req := Put{ID: first.Artifact.ID, ExpectedHead: first.Artifact.Head, Content: []byte("second"), Actor: "owner", Note: "Edited in chat", RequestID: "request-12345678"}
	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			res, err := r.Put(req)
			if err != nil || res.Revision.N != 2 {
				t.Errorf("retry: %+v %v", res, err)
			}
		}()
	}
	wg.Wait()
	a, _ := r.Get(first.Artifact.ID)
	if len(a.Revisions) != 2 || len(a.Receipts) != 1 {
		t.Fatal(a)
	}
	newer, err := r.Put(Put{ID: a.ID, ExpectedHead: a.Head, Content: []byte("third")})
	if err != nil {
		t.Fatal(err)
	}
	// New registry instance reads the atomic receipt from disk.
	reopened, err := NewRegistry(r.pool)
	if err != nil {
		t.Fatal(err)
	}
	replay, err := reopened.Put(req)
	if err != nil || replay.Changed || replay.Revision.N != 2 || replay.Artifact.Head != newer.Artifact.Head {
		t.Fatalf("replay: %+v %v", replay, err)
	}
	for _, change := range []func(*Put){func(p *Put) { p.Content = []byte("different") }, func(p *Put) { p.ExpectedHead = newer.Artifact.Head }, func(p *Put) { p.Actor = "other" }, func(p *Put) { p.Note = "different" }} {
		altered := req
		change(&altered)
		if _, err := reopened.Put(altered); !errors.Is(err, ErrRevisionConflict) {
			t.Fatalf("altered identity: %v", err)
		}
	}
	// A no-op gets a receipt but no synthetic revision, and remains replayable.
	same := Put{ID: a.ID, ExpectedHead: newer.Artifact.Head, Content: []byte("third"), RequestID: "unchanged-123456"}
	noop, err := reopened.Put(same)
	if err != nil || noop.Changed || len(noop.Artifact.Revisions) != 3 {
		t.Fatal(noop, err)
	}
	last, err := reopened.Put(Put{ID: a.ID, Content: []byte("fourth")})
	if err != nil {
		t.Fatal(err)
	}
	noop, err = reopened.Put(same)
	if err != nil || noop.Changed || noop.Revision.N != 3 || noop.Artifact.Head != last.Artifact.Head {
		t.Fatal(noop, err)
	}
	stale := req
	stale.RequestID = "new-request-123"
	if _, err := reopened.Put(stale); !errors.Is(err, ErrRevisionConflict) {
		t.Fatal(err)
	}
}
