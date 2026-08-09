package aion

// SeedFiles are the write-once starter corpora (spec §7: valid of shape,
// empty of content — except people.md, which migrates the owner-authored
// roster). Every seed MUST be a fixpoint of its own parser (seed_test.go).
var SeedFiles = map[string]string{
	"people.md": `- [initials:: BA] [name:: Benjamin Anderson] [role:: CEO]
- [initials:: RT] [name:: RJ Tevonian] [role:: CSO]
- [initials:: HZ] [name:: Hannah Zmuda] [role:: Founding Scientist]
- [initials:: HG] [name:: Heye Groß] [role:: Founding Engineer]
- [initials:: EL] [name:: Ellie Lumen] [role:: Study Director]
- [initials:: NM] [name:: Nirosha Murugan] [role:: External lead, Murugan Lab]
- [initials:: YS] [name:: Yashiro]
- [initials:: MM] [name:: Morgan Miller]
- [initials:: JR] [name:: Jack Ruhl]
- [initials:: ME] [name:: Matthias Estermann]
`,

	"vto.md": `# AION — V/TO

## 01 core values

## 02 core focus
- [purpose:: ]
- [niche:: ]

## 03 10-year target
- A medbed in every home

## 04 marketing strategy
- [target:: ]

## 05 3-year picture
- [date:: ]

## 06 1-year plan
- [date:: ]

## 07 quarter
- [start:: ] [end:: ]

## 08 issues
issues live in the backlog — open decisions are the issues list
`,

	"backlog.md": `# AION — backlog

## Tasks

## Decisions
`,

	"heuristics.md": `# AION — heuristics

## retired
`,

	"hiring.md": `# AION — hiring
`,

	"references.md": `# AION — references
`,

	"finances.md": `---
capital:
monthly_burn:
as_of:
currency: USD
source: manual
note:
---

body is private — never rendered, never exported.
`,
}
