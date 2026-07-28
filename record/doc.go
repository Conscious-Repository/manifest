// Package record is the manifest OS kernel: the one place that owns the
// mechanics every domain used to hand-roll — the [key:: value] inline-field
// grammar, checkbox-outline parsing with the fixpoint guarantee, slugs and
// explicit-id identity, frontmatter, sidecar conventions, archives, and the
// store lifecycle (ARCHITECTURE.md §3). A domain (goals, todos, real estate,
// aion, …) is a declaration over this kernel: record kinds, zone paths,
// section grammars, recognized fields, derived rollups, views — no parsing
// regexes and no serialization code of its own.
package record
