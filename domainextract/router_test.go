package domainextract

import (
	"testing"
	"time"
)

type legacyFixture struct{ calls int }

func (l *legacyFixture) SpoolRunNow(string, string, string, string) error { l.calls++; return nil }
func (l *legacyFixture) EngineAlive() (bool, time.Time)                   { return false, time.Time{} }
func TestRoutingNeverFallsBack(t *testing.T) {
	legacy := &legacyFixture{}
	r := &Router{Config: Config{Aion: true}, Vault: t.TempDir(), Legacy: legacy}
	if e := r.SpoolRunNow("extractor", "aion", "- log/missing.md", ""); e == nil || legacy.calls != 0 {
		t.Fatal("enabled route fell back")
	}
	if e := r.SpoolRunNow("extractor", "real-estate", "fixture", ""); e != nil || legacy.calls != 1 {
		t.Fatal("disabled route changed")
	}
	if alive, _ := r.For("aion").EngineAlive(); alive {
		t.Fatal("unattached successor ready")
	}
	if alive, _ := r.For("real-estate").EngineAlive(); alive {
		t.Fatal("legacy health ignored")
	}
}
