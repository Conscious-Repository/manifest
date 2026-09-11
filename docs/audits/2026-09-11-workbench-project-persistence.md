# Workbench project persistence

Project names and membership were stored alongside disposable inbox preferences.
They now use the existing record kernel and vaultwriter in one private
`system/workbench/projects.md` registry. Project context documents and normalized
run/review projections remain subsequent work; this is not full plan completion.

The compatibility endpoint remains `/api/chat/state/inbox/workstreams`. It
returns `record_version` alongside the existing numeric revision. A write checks
both against the complete source before editing the typed block. Unknown fields
and surrounding owner Markdown remain intact. Old clients must refresh before
writing. Startup migrates once and leaves the old JSON cache as a backup. An
existing record always wins; corruption never silently falls back to stale data.

The capability permits exactly this record. No portal route is introduced.
Archive, stop, sending, approvals and provider histories are unchanged.

Regression coverage verifies migration, cache loss, restart, hand-edit conflicts,
missing preconditions, unknown-field preservation, malformed source protection,
and private route isolation. Browser model choice and lost-send recovery checks
exercise compatibility with the existing chat frontend.
