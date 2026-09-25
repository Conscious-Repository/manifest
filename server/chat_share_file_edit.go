package server

import (
	"strings"

	"manifest/artifacts"
)

// Authorized generic team-file editing — the mechanical half only.
//
// The plan's artifact-review row leaves "generic team-file editing where
// explicitly authorized" open because it needs a per-file edit consent in
// share review, and the consent wording (and whether it defaults on or off)
// is the owner's decision. This file does NOT invent that consent. It supplies
// what is independent of the wording:
//
//   - teamFileEditEligibility, a pure per-file predicate over the exact bytes
//     the review already read: could this version be offered for editing?
//   - its propagation into chatShareFile.Edit, inside the reviewed envelope,
//     so the existing staged-bytes rule (Revision = hash of the review; the
//     publish refuses any other revision; recovery decodes the stored bytes)
//     covers it without a second fingerprint.
//
// Behind Server.shareTeamFileEdit (config shareTeamFileEditEligibility),
// default off. Off, the field is omitted and every review is byte-identical
// to before. On, nothing is granted and no portal edit route exists; a
// future consent control can only offer files whose Edit.Eligible is true.

type chatShareFileEdit struct {
	Eligible bool   `json:"eligible"`
	Reason   string `json:"reason"`
	// Base is the exact revision a permitted edit would be checked against
	// (stale-write rejection, as for shared task-plan edits).
	Base string `json:"base,omitempty"`
}

// UseShareTeamFileEditEligibility sets the default-off switch.
func (s *Server) UseShareTeamFileEditEligibility(on bool) { s.shareTeamFileEdit = on }

// teamFileEditEligibility decides from the file entry and the exact reviewed
// bytes alone (data is nil when the review did not read registered bytes).
// Order matters: privacy exclusions are checked before capability.
func teamFileEditEligibility(file chatShareFile, data []byte) chatShareFileEdit {
	no := func(reason string) chatShareFileEdit { return chatShareFileEdit{Reason: reason} }
	switch {
	case file.Record != "":
		return no("private record snapshot (" + file.Record + "); the team can read the reviewed copy but never edit a private record")
	case file.ArtifactID == "":
		return no("uploaded file without a registered artifact; there is no version history to check an edit against")
	}
	for _, ref := range file.References {
		if strings.HasPrefix(ref, "terminal-plan:") {
			return no("task plan version; shared plan edits keep the original plan's own authority")
		}
	}
	if data == nil || artifacts.Hash(data) != file.Hash {
		return no("reviewed bytes unavailable; eligibility cannot be established")
	}
	if p := describeArtifactPreview(file.Hash, data); p.Kind != "text" {
		reason := p.Reason
		if reason == "" {
			reason = p.MediaType + " is previewed, not edited"
		}
		return no("not editable text: " + reason)
	}
	return chatShareFileEdit{Eligible: true, Base: file.Hash, Reason: "registered text artifact; an edit would save a new version checked against this exact revision"}
}
