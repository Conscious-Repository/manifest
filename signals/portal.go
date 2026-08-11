package signals

// DegradedPortal — the deepseek-local health state as a FEED card (big-change
// Phase 7 / Phase 5 verify). The portal test handler writes its last result to
// a state file (there are no background pings, by doctrine — state is only as
// fresh as the last explicit test); a degraded result pages the feed until a
// later successful test overwrites it. The card is the "tell RJ" channel.

import (
	"encoding/json"
	"os"
	"time"
)

// PortalState is the test handler's record of the last explicit test.
type PortalState struct {
	Portal string    `json:"portal"`
	State  string    `json:"state"` // open | degraded
	Err    string    `json:"err,omitempty"`
	At     time.Time `json:"at"`
}

// DegradedPortal emits one card while the state file records a degraded test.
func DegradedPortal(stateFile string) Emitter { return portalEmitter{stateFile} }

type portalEmitter struct{ path string }

func (e portalEmitter) Emit(now time.Time) ([]Signal, error) {
	b, err := os.ReadFile(e.path)
	if err != nil {
		return nil, nil // never tested / cleared — nothing to page
	}
	var st PortalState
	if json.Unmarshal(b, &st) != nil || st.State != "degraded" {
		return nil, nil
	}
	age := int(now.Sub(st.At).Hours() / 24)
	if age < 0 {
		age = 0
	}
	return []Signal{{
		ID:      "portal-degraded:" + st.Portal,
		Kind:    "portal-degraded",
		Entity:  st.Portal,
		Label:   "portal degraded · " + st.Portal + " — tell RJ (don't restart the serving stack)",
		Age:     age,
		ActHref: "#/spirits",
		Hash:    st.At.Format(time.RFC3339),
	}}, nil
}
