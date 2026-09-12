package server

import (
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"time"

	"manifest/hermes"
	"manifest/ledger"
	"manifest/signals"
)

// runMigratedHermesDuty is a dark entry point for the successor, deliberately
// not wired to any duty, chat or scheduler in Phase 1. Refusals go into the
// existing run ledger, so an eventual caller cannot fail silently. No rejected
// reply reaches materializeHermesBrief or approvals.Propose.
func (s *Server) runMigratedHermesDuty(ctx context.Context, duty, prompt string) (hermes.Result, error) {
	if s.ledgerStore == nil {
		return hermes.Result{}, &hermes.Refusal{Reason: "run evidence store unavailable"}
	}
	var res hermes.Result
	var err error
	if duty == "" {
		err = &hermes.Refusal{Reason: "missing duty authority"}
	} else if s.hermes == nil {
		err = &hermes.Refusal{Reason: "missing duty authority"}
	} else {
		res, err = s.hermes.runner.Run(ctx, hermes.Request{MigratedDuty: duty, Prompt: prompt})
	}
	return s.recordMigratedDutyResult(duty, res, err)
}

// Only the bounded runner can mint the verification receipt. This entry point
// returns verified text to a future proposal caller; it does not file proposals.
func (s *Server) recordMigratedDutyResult(duty string, res hermes.Result, err error) (hermes.Result, error) {
	if err == nil && !res.DutyVerified() {
		err = &hermes.Refusal{Reason: "missing verified successor result"}
	}
	kind, reason := "run.completed", "verified bounded successor completion"
	if err != nil {
		kind, reason = "run.refused", "refused: runner failure"
		var refusal *hermes.Refusal
		if errors.As(err, &refusal) {
			reason = snipRunes(refusal.Error(), 160)
		}
		res = hermes.Result{}
	}
	// Neither caller-supplied duty names nor provider-reported strings enter
	// the ledger. Hashing retains correlation without storing prompt/PII.
	evidenceID := fmt.Sprintf("duty-%x", sha256.Sum256([]byte(duty)))[:21]
	meta := map[string]any{"duty": evidenceID, "itemsWritten": 0}
	if s.hermes != nil {
		if a := s.hermes.runner.DutyEvidence(duty); a != nil {
			meta["authority"] = a
		}
	}
	if res.DutyVerified() {
		meta["model"], meta["spentUsd"] = res.Model, res.SpentUSD
		meta["cost_policy"], meta["cost_telemetry"], meta["provider_binding"], meta["usage"] = res.CostPolicy, res.CostTelemetry, res.ProviderBinding, res.Usage
	}
	entry := ledger.Entry{TS: time.Now(), Source: "run", Kind: kind, Actor: "agent:hermes", Harness: "hermes", Text: reason, Meta: meta}
	if writeErr := s.ledgerStore.Append(entry); writeErr != nil {
		return hermes.Result{}, errors.Join(err, writeErr)
	}
	return res, err
}

type hermesDutyEmitter struct{ s *Server }

func (s *Server) HermesDutyEmitter() signals.Emitter { return hermesDutyEmitter{s} }
func (e hermesDutyEmitter) Emit(now time.Time) ([]signals.Signal, error) {
	out := []signals.Signal{}
	if e.s.ledgerStore == nil {
		return out, nil
	}
	latest := map[string]ledger.Entry{}
	for _, day := range e.s.ledgerStore.Days() {
		if day < now.Add(-runFailureWindow-24*time.Hour).Format("2006-01-02") {
			continue
		}
		entries, err := e.s.ledgerStore.Day(day)
		if err != nil {
			return nil, err
		}
		for _, entry := range entries {
			if entry.Harness != "hermes" || entry.Source != "run" || now.Sub(entry.TS) > runFailureWindow || entry.TS.After(now) {
				continue
			}
			duty, _ := entry.Meta["duty"].(string)
			if duty == "" {
				continue
			}
			if prev, ok := latest[duty]; !ok || entry.TS.After(prev.TS) {
				latest[duty] = entry
			}
		}
	}
	for duty, entry := range latest {
		if entry.Kind != "run.refused" {
			continue
		}
		out = append(out, signals.Signal{ID: "agent-duty-refused:" + duty, Kind: "agent-duty-refused", Entity: duty, Label: duty + " · " + snipRunes(entry.Text, 160), ActHref: "#/agents/runs", Hash: entry.TS.Format(time.RFC3339Nano) + "|" + entry.Text})
	}
	return out, nil
}

// Shared read-only projection used by Schedule, Runs and Settings.
func (s *Server) dutyRefusals(now time.Time) []signals.Signal {
	out, _ := s.HermesDutyEmitter().Emit(now)
	if out == nil {
		return []signals.Signal{}
	}
	return out
}
