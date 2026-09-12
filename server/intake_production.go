package server

import (
	"encoding/json"
	"errors"
	"net/http"
	"strings"

	"manifest/approvals"
	"manifest/extract"
	"manifest/hermes"
	"manifest/realestate"
	"manifest/reintake"
)

// UseReIntake only configures the canonical owner cockpit route. No worker,
// poller, public portal route, or alternate uploader is registered here.
func (s *Server) UseReIntake(cfg reintake.Config, dataDir string, authority hermes.DutyAuthority) {
	s.reIntakeConfig, s.reIntakeDataDir, s.reIntakeAuthority = cfg, dataDir, authority
	s.reIntakeRun = reintake.RunStaged
}

func (s *Server) reserveREIntake() error {
	if err := reintake.ValidateProductionAccess(s.reIntakeConfig, s.reIntakeAuthority); err != nil {
		return err
	}
	if s.approvals == nil || s.vault == nil || s.reIntakeRun == nil || s.realestateRootOr() != "system/realestate" {
		return errors.New("production dependencies unavailable")
	}
	return reintake.ReserveUpload(s.reIntakeDataDir)
}

func (s *Server) stopREIntake(w http.ResponseWriter, _ error) {
	// HTTP response is the synchronous owner page; never send provider diagnostics
	// or invoke a notification connector. The reservation remains burnt on errors.
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusServiceUnavailable)
	_ = json.NewEncoder(w).Encode(map[string]any{"error": reintake.ProductionStop, "pageOwnerRequired": true, "pilotStatus": reintake.PilotStatus(s.reIntakeDataDir), "spooled": false})
}

func (s *Server) finishREIntake(w http.ResponseWriter, r *http.Request, name string, ref realestate.CASRef, extractRel string, res extract.Result) {
	stop := func(err error) { s.stopREIntake(w, err) }
	if !res.HasText || res.Via == "none" {
		stop(errors.New("no extract"))
		return
	}
	context, err := s.productionIntakeContext(name, ref.Ref, extractRel)
	if err != nil {
		stop(err)
		return
	}
	// Existing canonical records and pending cards participate in duplicate checks.
	for _, c := range s.realestate.Contracts() {
		if c.Doc == ref.Ref {
			stop(errors.New("source already contracted"))
			return
		}
	}
	for _, p := range s.approvals.List("pending") {
		if payload, ok := approvals.ParseReContractPayload(p.Body); ok && payload.Doc == ref.Ref {
			stop(errors.New("source already pending"))
			return
		}
	}
	textSource, err := reintake.StageExtract(s.reIntakeDataDir, res.Text)
	if err != nil {
		stop(err)
		return
	}
	target := "system/realestate/contracts/intake-" + strings.TrimPrefix(ref.Ref, "sha256:") + ".md"
	c := reintake.ProductionContract{Owner: "owner", Actor: "extractor", Source: ref.Ref, Documents: []string{ref.Ref}, TextSource: textSource, Context: context, Target: target, ApplyPath: target}
	if err := reintake.ValidateProductionRoute(s.reIntakeConfig, s.reIntakeAuthority, c); err != nil {
		stop(err)
		return
	}
	candidate, receipt, err := s.reIntakeRun(r.Context(), s.reIntakeDataDir, s.reIntakeConfig, s.reIntakeAuthority, c)
	if err != nil {
		stop(err)
		return
	}
	if r.Context().Err() != nil {
		stop(r.Context().Err())
		return
	}
	fresh, err := s.productionIntakeContext(name, ref.Ref, extractRel)
	if err != nil || fresh != context {
		stop(errors.New("domain context changed"))
		return
	}
	if err := reintake.CheckCandidateReceipt(s.reIntakeDataDir, c, candidate, receipt); err != nil {
		stop(err)
		return
	}
	// This is the normal pending inbox. Confirm/apply remains an owner action on
	// its existing card; no completion/ledger/spool/connector path is called.
	proposal, err := s.approvals.Propose(candidate)
	if err != nil {
		stop(err)
		return
	}
	writeJSON(w, map[string]any{"ref": ref.Ref, "name": ref.Name, "existed": ref.Existed, "spooled": false, "proposalId": proposal.ID, "status": "pending", "pilotStatus": reintake.PilotStatus(s.reIntakeDataDir)})
}

// Reuse the existing contractor/property/contract request context, and supply
// concrete node IDs because the bounded successor cannot use vault.read tools.
// Reject oversized context instead of truncating away matching/dedupe evidence.
func (s *Server) productionIntakeContext(name, ref, extractRel string) (string, error) {
	props, err := s.realestate.Properties()
	if err != nil {
		return "", err
	}
	var b strings.Builder
	b.WriteString(s.intakeRequest(name, ref, extractRel))
	b.WriteString("\nThe extract text is supplied inline; do not read vault paths. Existing work nodes (property — id — text):\n")
	for _, p := range props {
		if p.Hidden || (p.Control != "owned" && p.Entity == "") {
			continue
		}
		var walk func([]*realestate.WorkNode)
		walk = func(nodes []*realestate.WorkNode) {
			for _, n := range nodes {
				if n == nil {
					continue
				}
				text := ""
				if n.Task != nil {
					text = n.Task.Text
				}
				b.WriteString(p.Slug + " — " + n.ID + " — " + text + "\n")
				walk(n.Children)
			}
		}
		for _, rock := range p.Work {
			b.WriteString(p.Slug + " — " + rock.ID + " — " + rock.Text + "\n")
			walk(rock.Tasks)
		}
	}
	if b.Len() > 32000 {
		return "", errors.New("domain context exceeds pilot bound")
	}
	return b.String(), nil
}
