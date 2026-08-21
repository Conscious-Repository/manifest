package realestate

import "strings"

// Where a property sits relative to its OWN closing.
//
// The signal is STATUS, not `control`. `control: owned` means "this is ours to
// manage", which the vault sets the day a deal is signed — 28 of the 32 Garden
// SPE parcels carry `control: owned` while still `status: under_contract`.
// Reading control as "the purchase happened" put every one of those purchase
// prices into the portfolio's SPENT figure: $558,000 of money that has not
// left any bank account.
//
// `from:` (the seller field EntityHoldings was documented to key on) is empty
// on all 63 property records, so it cannot carry this distinction either.
type AcqState int

const (
	// AcqNone — a parcel we research or negotiate for but have not committed
	// to buy. No acquisition money is planned, committed, or spent.
	AcqNone AcqState = iota
	// AcqUnderContract — signed and obligated to close. The purchase price is
	// COMMITTED (money we must bring to closing) and nothing more.
	AcqUnderContract
	// AcqClosed — the purchase happened. The acquisition plan is recognized as
	// spent even before a closing-statement row lands in the ledger.
	AcqClosed
)

// String is the word every surface shows a partner. It must never say "owned"
// about a deal that has not closed.
func (s AcqState) String() string {
	switch s {
	case AcqClosed:
		return "owned"
	case AcqUnderContract:
		return "under-contract"
	default:
		return "pipeline"
	}
}

// closedStatuses are the statuses a property can only reach after closing.
var closedStatuses = map[string]bool{
	"pre_development": true,
	"construction":    true,
	"completed":       true,
	"leased":          true,
	"listed":          true,
	"sold":            true,
}

// AcqStateOf reads a property's acquisition state from control + status.
// An unrecognized status reads as AcqNone: an unknown state must never be
// assumed to have spent money.
func AcqStateOf(control, status string) AcqState {
	if !strings.EqualFold(strings.TrimSpace(control), "owned") {
		return AcqNone // tracked research — someone else's parcel
	}
	s := strings.ToLower(strings.TrimSpace(status))
	switch {
	case closedStatuses[s]:
		return AcqClosed
	case s == "under_contract":
		return AcqUnderContract
	default:
		return AcqNone
	}
}
