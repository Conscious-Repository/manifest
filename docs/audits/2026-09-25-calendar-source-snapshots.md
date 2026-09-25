# Calendar source identity and partial reads — September 25

Calendar reads now expose a snapshot with per-account/calendar issues and an
explicit partial flag. Events retain account, calendar and provider event IDs;
a stable composite key distinguishes equal provider IDs across sources. Missing
source identity produces no selectable key. Provider response timing no longer
changes calendar merge order: account order is retained and calendars are sorted.

The Calendar API includes these identities and completeness metadata. Calendar
shows an incomplete-read warning while rendering available events, including
when a separate account already requires reauthentication. Complete recovery
clears the warning. Total failure still reports an error. Failed later pages
remain explicitly partial; provider error bodies are not retained in issues.

The Day calendar source uses this result and does not replace its complete
offline mirror with partial data. Its calendar date interval uses the next local
date, preserving the day boundary across daylight-saving transitions. Legacy
Events callers retain best-effort event reads.

Validation: full calendar race tests pass with a local fake provider, forced
out-of-order responses, duplicate event IDs across calendars/accounts, failed
calendar discovery, failed calendar reads, failed later pages, stable repeat
keys, total failure, unqualified-key refusal and complete-cache preservation.
Chromium exercises the real calendar loading/warning code with available events,
partial warning, complete recovery and 320/390/1440px bounds; screenshot inspected.
Release build, syntax and diff checks pass. `make test` passed server (39.854s)
and other packages except the known unchanged Hermes source-hash canary failure;
final account-discovery assertions passed separately under the race detector.

Live verification checks service health and served code without fetching a real
calendar. Exact calendar-event selection in Records is still the next integration;
these contracts are prerequisites, not a claim that selection is complete. Other
unchecked workbench requirements remain open.
