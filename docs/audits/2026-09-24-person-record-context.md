# Person record context — September 24

Records now includes People from the existing contacts service. Search keeps its
canonical contact keys and explicit CRM keys, with profile paths and aliases for
disambiguation. It does not create contacts, bind aliases, infer candidate records
or join a business record to a personal note by matching a display name.

The reviewed snapshot includes the exact profile note (including frontmatter,
relationship variables and specializations), identity, linked addresses, role and
location; other entity references; email-matched past meetings; separately labeled
confirmed versus possible upcoming matches; dated/undated note and transcript
references; and open meeting follow-ups. Linked note/transcript bodies, recruiting
records, fundraising summaries and the separate task-board waiting projection are
not included. The Contacts source link retains the exact canonical key. This is an
observed projection of the existing contact/calendar caches, not a new live-calendar
contract or a transaction over every source.

A missing, unreadable, oversized or out-of-vault profile fails preview. The source
path is checked before loading the contact page, and a changed canonical key/path
is rejected. When multiple indexed notes have the same profile name, the picker
refuses to select whichever path won the name-based entity index; it directs the
owner to the existing path-specific knowledge-note selector. Existing contact-index
ambiguities are not silently rewritten or merged.

Explicit selection uses the existing exact-version artifact retention, source
identity, synced composer draft and private delivery receipt. Search/preview do not
retain or send context. Snapshot edits remain disabled; source edits belong to the
existing Contacts/Notes surfaces. The current single-artifact selection is replaced
only after successful explicit retention. Source changes reject an older review.
Shared inputs and public portal routes gain no access to these private records.

Evidence:

- `TestPersonContextIdentityEvidenceAndRetention`: equal display names with separate
  personal/CRM keys; profile alias search; exact source bytes and relationship
  fields; distinction between last-met and last-mentioned, confirmed and possible
  meetings; exclusion of transcript/candidate/business contents; no source mutation
  or browse-side retention; deduplicated retention, source route and stale-profile
  rejection; source-person identity and exact retained context.
- `TestPersonContextUnavailableProfilesAndSharedBoundary`: fake native delivery of
  exact bytes with no duplicate execution on retry; shared-input and portal denial;
  missing/outside-vault note rejection and explicit index-read failure.
- `TestPersonContextRejectsAmbiguousProfileNames`: duplicate note names fail person
  selection while both exact paths remain available as knowledge notes.
- Chromium `server/testdata/chat-note-context.cjs`: People kind, keyboard selection,
  exact CRM key payload, kind/preview restoration, source route, stale-kind result
  suppression and shared hiding. Existing both-theme 320/390/1440px checks and the
  workspace fixture pass. `/tmp/manifest-person-context-phone.png` inspected.
- Focused Go tests, release build, JS syntax and diff checks pass. `make test` passes
  server and other packages except the known source-hash re-audit failure in
  `cmd/re-intake-canary/TestCanarySourceCallGraphIsolation` for unchanged Hermes files.

Provider evidence uses isolated fixtures. Structured organization/candidate
selection, schedule items, capability/skill context, multiple selections and the
remaining integrated/live-provider/physical-device acceptance are still open.
