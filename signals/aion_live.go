package signals

import (
	"fmt"
	"time"
)

// AionLiveState is implemented by the server-owned live projection without
// introducing a package cycle.
type AionLiveState interface {
	LiveSyncState() (stale bool, servingRevision, detail string)
}

// AionLiveSync pages the FEED only while the portal is serving a last-known-
// good snapshot. The signal disappears automatically as soon as validation
// succeeds again.
func AionLiveSync(src AionLiveState) Emitter { return aionLiveEmitter{src} }

type aionLiveEmitter struct{ src AionLiveState }

func (e aionLiveEmitter) Emit(_ time.Time) ([]Signal, error) {
	if e.src == nil {
		return nil, nil
	}
	stale, rev, detail := e.src.LiveSyncState()
	if !stale {
		return nil, nil
	}
	label := "Aion live sync degraded · serving last good snapshot"
	if detail != "" {
		label += " · " + detail
	}
	return []Signal{{
		ID: "aion-live-degraded", Kind: "aion-live-degraded", Entity: "Aion live sync",
		Label: label, ActHref: "#/aion", Hash: fmt.Sprintf("%s|%s", rev, detail),
	}}, nil
}
