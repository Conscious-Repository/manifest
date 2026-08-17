package record

import "testing"

func TestOwnerIsMe(t *testing.T) {
	cases := []struct {
		owner string
		want  bool
	}{
		{"me", true},
		{"Me", true},
		{" me ", true},
		// "ME" is Matthias Estermann in the aion people registry, not the
		// sentinel — a case-insensitive match here routed his tasks to the
		// vault owner's list.
		{"ME", false},
		{"BA", false},
		{"HZ", false},
		{"BA/RT", false},
		{"agent:hermes", false},
		{"Mel", false},
		{"", false},
	}
	for _, c := range cases {
		if got := OwnerIsMe(c.owner); got != c.want {
			t.Errorf("OwnerIsMe(%q) = %v, want %v", c.owner, got, c.want)
		}
	}
}

func TestIsInitials(t *testing.T) {
	for _, tok := range []string{"BA", "ME", "HZ", "RJT"} {
		if !IsInitials(tok) {
			t.Errorf("IsInitials(%q) = false, want true", tok)
		}
	}
	for _, tok := range []string{"me", "Me", "Hannah", "agent:hermes", "", "MATT"} {
		if IsInitials(tok) {
			t.Errorf("IsInitials(%q) = true, want false", tok)
		}
	}
}
