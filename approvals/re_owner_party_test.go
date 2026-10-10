package approvals

import "testing"

// A bid's "owner to decide" names a role, not a person: it lands on the
// configured party, and a real name passes through untouched.
func TestReTaskOwnerParty(t *testing.T) {
	s := (&Store{}).WithReOwnerParty("olga-sobkiv")
	for in, want := range map[string]string{
		"owner": "olga-sobkiv", " Owner ": "olga-sobkiv", "homeowner": "olga-sobkiv",
		"twisted-brick": "twisted-brick", "": "",
	} {
		if got := s.reTaskOwner(in); got != want {
			t.Errorf("reTaskOwner(%q) = %q, want %q", in, got, want)
		}
	}
	if got := (&Store{}).reTaskOwner("owner"); got != "" {
		t.Errorf("unset party: got %q, want unassigned", got)
	}
}

// A milestone the extractor names by its slug reads as words, and keeps its
// node id; a written name is left alone.
func TestMilestoneTitle(t *testing.T) {
	for in, want := range map[string]string{
		"masonry-tuck-point-repair":   "Masonry tuck point repair",
		"Masonry & tuck-point repair": "Masonry & tuck-point repair",
		"electrical":                  "electrical",
	} {
		if got := milestoneTitle(in); got != want {
			t.Errorf("milestoneTitle(%q) = %q, want %q", in, got, want)
		}
	}
}
