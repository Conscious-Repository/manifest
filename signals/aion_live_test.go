package signals

import (
	"testing"
	"time"
)

type fakeAionLive struct {
	stale  bool
	rev    string
	detail string
}

func (f *fakeAionLive) LiveSyncState() (bool, string, string) { return f.stale, f.rev, f.detail }

func TestAionLiveSignalAutoClears(t *testing.T) {
	src := &fakeAionLive{stale: true, rev: "good-1", detail: "bad date"}
	emitter := AionLiveSync(src)
	got, err := emitter.Emit(time.Now())
	if err != nil || len(got) != 1 || got[0].Kind != "aion-live-degraded" || got[0].Hash == "" {
		t.Fatalf("degraded signal = %+v err=%v", got, err)
	}
	src.stale = false
	got, err = emitter.Emit(time.Now())
	if err != nil || len(got) != 0 {
		t.Fatalf("healed sync did not auto-clear: %+v err=%v", got, err)
	}
}
