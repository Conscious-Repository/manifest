package transcriptsync

import (
	"fmt"

	"manifest/approvals"
)

// checkOutcome never interprets a missing card/note as permission to replay an
// effect. Decisions can legitimately change since the connector saved its row.
func (s *Service) checkOutcome(source, id string, prior *Outcome, inv approvals.ConnectorInventory) error {
	var card *approvals.ConnectorApproval
	for _, p := range inv.Items {
		if p.Source == source && p.SourceID == id {
			copy := p
			card = &copy
		}
	}
	var paths []string
	var err error
	if source == "granola" {
		paths, err = s.idx.PathsByGranolaID(id)
	} else {
		paths, err = s.idx.PathsByPocketID(id)
	}
	if err != nil || len(paths) > 1 {
		return fmt.Errorf("source identity index unavailable or conflicting")
	}
	if card != nil && (card.Status == "approved") != (len(paths) == 1) {
		return fmt.Errorf("uncertain approval/vault outcome; owner reconciliation required")
	}
	if prior == nil {
		return nil
	}
	if prior.ProposalID != "" {
		if card == nil || card.ID != prior.ProposalID {
			return fmt.Errorf("persisted outcome lost canonical approval; replay refused")
		}
	} else if prior.Disposition != "existing-note" || len(paths) != 1 {
		return fmt.Errorf("persisted outcome lost source note; replay refused")
	}
	return nil
}

// A historical uncertainty is terminal only with the exact durable owner receipt.
func (s *Service) checkReconciledOutcome(source, id string, prior *Outcome, inv approvals.ConnectorInventory, owner *approvals.OwnerReconciliation) error {
	if prior != nil && prior.Disposition == approvals.ReconciledUncertain {
		if prior.Replay || owner == nil || owner.Source != source || owner.SourceID != id || owner.Replay || owner.Disposition != prior.Disposition || approvals.ValidateOwnerReconciliation(*owner, inv) != nil {
			return fmt.Errorf("invalid no-replay reconciliation")
		}
		var paths []string
		var err error
		if source == "granola" {
			paths, err = s.idx.PathsByGranolaID(id)
		} else {
			paths, err = s.idx.PathsByPocketID(id)
		}
		if err != nil || len(paths) != 0 || len(owner.Artifacts) != 1 || owner.Artifacts[0].ID != prior.ProposalID {
			return fmt.Errorf("reconciled uncertainty changed; owner review required")
		}
		return nil
	}
	return s.checkOutcome(source, id, prior, inv)
}
