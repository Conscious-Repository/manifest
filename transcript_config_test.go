package main

import (
	"encoding/json"
	"testing"
)

func TestTranscriptAndExtractionRoutesDefaultOff(t *testing.T) {
	var c Config
	if e := json.Unmarshal([]byte(`{}`), &c); e != nil {
		t.Fatal(e)
	}
	if c.TranscriptSync.Granola.Enabled || c.TranscriptSync.Pocket.Enabled || c.DomainExtraction.Aion || c.DomainExtraction.RealEstate || c.DomainExtraction.OodaEmail {
		t.Fatal("migration enabled by default")
	}
	if e := json.Unmarshal([]byte(`{"transcriptSync":{"pocket":{"enabled":true,"account":"fixture"}},"domainExtraction":{"aion":true}}`), &c); e != nil {
		t.Fatal(e)
	}
	if !c.TranscriptSync.Pocket.Enabled || c.TranscriptSync.Pocket.Account != "fixture" || !c.DomainExtraction.Aion || c.DomainExtraction.RealEstate {
		t.Fatal("explicit route config not preserved")
	}
}
