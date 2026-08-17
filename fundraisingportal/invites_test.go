package fundraisingportal

import "testing"

func TestInviteStoreReplaceAndImmediateRevocation(t *testing.T) {
	store, err := NewInviteStore(t.TempDir(), "owner@aion.bio")
	if err != nil {
		t.Fatal(err)
	}
	if !store.Allowed("OWNER@AION.BIO") {
		t.Fatal("admin must always be allowed")
	}
	if err := store.Replace([]string{" Advisor@Example.com ", "advisor@example.com"}); err != nil {
		t.Fatal(err)
	}
	if got := store.List(); len(got) != 1 || got[0] != "advisor@example.com" || !store.Allowed(got[0]) {
		t.Fatalf("invites = %#v", got)
	}
	if err := store.Replace(nil); err != nil {
		t.Fatal(err)
	}
	if store.Allowed("advisor@example.com") {
		t.Fatal("revoked invite remained authorized")
	}
}

func TestInviteStoreRejectsInvalidEmail(t *testing.T) {
	store, _ := NewInviteStore(t.TempDir(), "owner@aion.bio")
	if err := store.Replace([]string{"not-an-email"}); err == nil {
		t.Fatal("expected invalid email error")
	}
}
