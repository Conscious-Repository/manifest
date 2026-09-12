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
	if cfg.ReIntake.ShadowEnabled || cfg.ReIntake.ProductionEnabled {
		t.Fatal("shadow defaults on")
	}
	if err := json.Unmarshal([]byte(`{"reIntake":{"shadowEnabled":true}}`), &cfg); err != nil || !cfg.ReIntake.ShadowEnabled {
		t.Fatal("explicit shadow flag not decoded")
	}
}

func TestReIntakePrimaryAuthorityConfig(t *testing.T) {
	var cfg Config
	if err := json.Unmarshal([]byte(`{"hermes":{"duties":{"extractor/re-intake":{"costPolicy":"local-zero-marginal","endpoint":"http://192.168.87.11:8000/v1","providerBinding":"fixed-local-endpoint","provider":"deepseek-local","model":"deepseek-v4.1-flash","tools":["none"],"mcp":"no_mcp","timeoutSeconds":120,"maxSteps":1,"ceilingUsd":0}}}}`), &cfg); err != nil {
		t.Fatal(err)
	}
	a := cfg.Hermes.Duties["extractor/re-intake"]
	if err := a.Validate(); err != nil {
		t.Fatal(err)
	}
	if cfg.ReIntake.ShadowEnabled || a.Provider != "deepseek-local" || a.Model != "deepseek-v4.1-flash" || a.TimeoutSeconds != 120 || a.MaxSteps != 1 || *a.CeilingUSD != 0 || len(a.Tools) != 1 || a.Tools[0] != "none" || a.MCP != "no_mcp" {
		t.Fatal("config changed explicit primary or enabled shadow")
	}
}

func TestReIntakeProductionFlagExplicitOnly(t *testing.T) {
	var cfg Config
	if err := json.Unmarshal([]byte(`{"reIntake":{"productionEnabled":true}}`), &cfg); err != nil || !cfg.ReIntake.ProductionEnabled || cfg.ReIntake.ShadowEnabled {
		t.Fatal("production flag decode changed shadow or failed")
	}
}

func TestReIntakeOwnerBoundaryIsExplicitConfiguration(t *testing.T) {
	var cfg Config
	if err := json.Unmarshal([]byte(`{"reIntake":{"productionEnabled":true,"ownerBoundary":"private-tailnet-owner"}}`), &cfg); err != nil {
		t.Fatal(err)
	}
	if cfg.ReIntake.OwnerBoundary != "private-tailnet-owner" || !cfg.ReIntake.ProductionEnabled {
		t.Fatal(cfg.ReIntake)
	}
	var absent Config
	if err := json.Unmarshal([]byte(`{}`), &absent); err != nil {
		t.Fatal(err)
	}
	if absent.ReIntake.OwnerBoundary != "" || absent.ReIntake.ProductionEnabled {
		t.Fatal("implicit owner authority")
	}
}
