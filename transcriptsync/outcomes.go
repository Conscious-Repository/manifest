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
