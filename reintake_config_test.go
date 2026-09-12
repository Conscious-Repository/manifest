package main

import (
	"encoding/json"
	"testing"
)

func TestReIntakeConfigDefaultOff(t *testing.T) {
	var cfg Config
	if err := json.Unmarshal([]byte(`{}`), &cfg); err != nil {
		t.Fatal(err)
	}
	if cfg.ReIntake.ShadowEnabled {
		t.Fatal("shadow defaults on")
	}
	if err := json.Unmarshal([]byte(`{"reIntake":{"shadowEnabled":true}}`), &cfg); err != nil || !cfg.ReIntake.ShadowEnabled {
		t.Fatal("explicit shadow flag not decoded")
	}
}
