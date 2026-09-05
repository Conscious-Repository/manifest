package recruiting

import "strings"

// SeedFiles are the write-once starter records: valid of SHAPE, empty of
// content. Every seed MUST be a fixpoint of its own parser (recruiting_test).
//
// What is seeded and what is not is a decision, not an oversight:
//   - the four role records exist because D2 fixes the four lanes;
//   - the MRI role's `## criteria` are the ones the plan itself writes out
//     (§4.4). The other three roles get only what the plan establishes for
//     every AION role — on-site Saint Louis, and full-remote as the
//     disqualifier — because translating a posting into must/nice is
//     Benjamin's judgment, not a default this file gets to invent;
//   - seeds.md carries Benjamin and RJ and nothing else (D11): every other
//     seed is an owner action, and the specific labs, companies, papers and
//     repos are Q5, still open;
//   - network/edges.md is empty. An edge is a claim, and we have not been
//     told one yet.
var SeedFiles = map[string]string{
	"roles/mri-engineer.md": roleSeed("role/mri-engineer", "MRI Engineer", true, `## criteria
- [criterion:: low-field MRI hardware] [class:: must] [weight:: 3]
- [criterion:: pulse sequence or coil design] [class:: must] [weight:: 3]
- [criterion:: on-site Saint Louis] [class:: must] [weight:: 2]
- [criterion:: rapid prototyping] [class:: nice] [weight:: 1]
- [criterion:: requires full remote] [class:: disqualifier]

## sourcing
- [term:: low-field MRI] [term:: coil design] [term:: quantitative MRI]
`),

	"roles/mechanical-engineer.md": roleSeed("role/mechanical-engineer", "Mechanical Engineer", true, ownerCriteria),
	"roles/biomedical-engineer.md": roleSeed("role/biomedical-engineer", "Biomedical Engineer", false, ownerCriteria),
	"roles/scientist-microscopy.md": roleSeed("role/scientist-microscopy",
		"Scientist: Microscopy", false, ownerCriteria),

	"seeds.md": `# AION recruiting — seeds

The high-signal set a source run is scoped FROM: target labs and PIs, target
companies, people Benjamin and RJ know, and key papers and repos. Twenty to
fifty entries, entered by hand. A seed is not a candidate.

- [id:: seed/person-ben-anderson] [class:: person] [name:: Benjamin Anderson] [org:: AION Biosciences] [source:: owner] [consent:: owner]
- [id:: seed/person-rj-tevonian] [class:: person] [name:: RJ Tevonian] [org:: AION Biosciences] [source:: owner] [consent:: owner]
`,

	"network/people.md": `# AION recruiting — network

- [id:: aion-net/ben-anderson] [name:: Benjamin Anderson] [type:: founder] [email:: ben@aion.bio] [org:: AION Biosciences] [source:: owner] [consent:: owner]
- [id:: aion-net/rj-tevonian] [name:: RJ Tevonian] [type:: founder] [org:: AION Biosciences] [source:: owner] [consent:: owner]
`,

	"network/edges.md": `# AION recruiting — edges

Relationship CLAIMS, never assumed truth. Every row carries the source that
supports it, the basis in prose, and whether it was inferred.
`,

	"passed.md": `# AION recruiting — passed

People already looked at and declined, so a second sweep of the same place
does not ask the same question twice. Each row is a TOMBSTONE, not a record:
the key a search matches on, the name, the reason if one was given, and the
date. Delete a row to let that person be offered again.
`,
}

// ownerCriteria is the criteria section a role ships with when its
// must/nice translation has not been written yet: only what holds for every
// AION role, plus a note saying whose call the rest is.
const ownerCriteria = `## criteria
<!-- the domain musts for this role are Benjamin's translation of the posting -->
- [criterion:: on-site Saint Louis] [class:: must] [weight:: 2]
- [criterion:: requires full remote] [class:: disqualifier]

## sourcing
`

// roleSeed renders one roles/<slug>.md starter. Ashby ids and handoff_mode
// stay blank: D8 gives the handoff no invented default, and a blank key is
// how the record says "the UI asks".
func roleSeed(id, title string, pinned bool, body string) string {
	var b strings.Builder
	b.WriteString("---\n")
	b.WriteString("id: " + id + "\n")
	b.WriteString("title: " + title + "\n")
	b.WriteString("status: open\n")
	b.WriteString("location: Saint Louis, MO\n")
	b.WriteString("employment: full-time on-site\n")
	b.WriteString("ashby_job_id:\n")
	b.WriteString("ashby_posting_id:\n")
	b.WriteString("ashby_project_id:\n")
	b.WriteString("handoff_mode:\n")
	b.WriteString("pinned: " + emitBool(pinned) + "\n")
	b.WriteString("source: owner\n")
	b.WriteString("synced:\n")
	b.WriteString("---\n\n")
	b.WriteString(body)
	b.WriteString("\n## posting\n")
	return b.String()
}

// SeedOrder is the deterministic write order for Ensure — roles first, then
// the seed set, then the network. A map iteration would seed in a different
// order on every boot and make the audit log unreadable.
var SeedOrder = []string{
	"roles/mri-engineer.md",
	"roles/mechanical-engineer.md",
	"roles/biomedical-engineer.md",
	"roles/scientist-microscopy.md",
	"seeds.md",
	"network/people.md",
	"network/edges.md",
	"passed.md",
}
