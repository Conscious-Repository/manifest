# Marked organization context — September 25

Records now exposes the contacts store's explicit owner organization/firm
classifications. It retains their stable keys, searches profile aliases, and
previews classification plus the exact authored profile note where one exists.
Unlinked organizations explicitly have no profile. A person or a company-looking
name is not silently classified as an organization. Ambiguous profile names and
profiles outside authored knowledge are refused.

Explicit selection uses the existing reviewed-hash snapshot and private draft
flow. Organization provenance links to the existing contact source page when
available. Snapshots are preview-only and do not create contacts or change
classification. Linked note contents, recruiting records and fundraising
summaries are excluded.

Validation: focused server race tests cover alias lookup, body-free search, exact
profile bytes, owner classification, note-less state, stale-source refusal,
canonical source links, retained-version delivery after source edits, native
duplicate-request recovery, shared/public denial and ambiguous profile refusal.
Chromium uses the real Records inspector for keyboard selection, kind/view
restoration and source links; responsive checks pass and the organization
phone-width screenshot was inspected. Build and diff checks pass. Full-suite and
read-only live verification are recorded in the plan checkpoint.

Coverage is explicitly marked organizations in Contacts. This does not claim a
unified organization registry across every business adapter or infer new entities
from other records. Schedule context, remaining organization sources and other
unchecked workbench requirements remain open.
