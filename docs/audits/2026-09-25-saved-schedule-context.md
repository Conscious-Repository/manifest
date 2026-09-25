# Saved schedule context — September 25

Records now offers saved Day schedule slots. Search uses today's saved schedule;
entering YYYY-MM-DD selects another date. Each slot is identified by date and
time token, and preview contains its saved label and focus state. Selection retains
the reviewed version through the existing registry/draft machinery. Source links
open the matching Day date. Snapshot context does not reschedule anything.

The daily service exposes a read-only authored-schedule projection using its
existing locator and parser. It reads only the manifest region, never fetches
calendar events, fills default rows, creates daily notes, or adds journal/tasks
to context. Bounded source reads and path checks remain explicit; incomplete or
ambiguous regions and duplicate slot identities are refused.

Validation: daily tests use a calendar source that panics if called, verify saved
rows and no file mutation/creation, and reject malformed regions, duplicate
identities and invalid dates. Server race tests cover one exact historical slot,
excluded neighboring/journal context, reviewed-hash retention, stale refusal,
historical source links, frozen native delivery after source edits, duplicate
request recovery, and shared/public denial. Chromium uses the real Records picker
for keyboard selection, restore and source links; responsive checks pass and the
schedule phone-width screenshot was inspected. Build, syntax and diff checks pass.
Full-suite and live verification are recorded in the plan checkpoint.

This covers saved Day slots. Live calendar entries still need account/calendar-
qualified identity and truthful partial-read state before equivalent selection.
Other unchecked workbench requirements remain open.
