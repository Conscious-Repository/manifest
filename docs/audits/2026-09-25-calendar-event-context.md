# Calendar event context — September 25

Records now searches calendar events by date, title, connected account or calendar.
The source query uses the local calendar day, including daylight-saving boundaries.
Selection requires a complete source read and qualified account/calendar/event
identity. Equal provider IDs across accounts remain separate. Preview contains
available title, time, all-day/declined state and reported non-self participants;
descriptions, attachments, linked notes and other events are excluded.

Reviewed-hash selection re-reads the source before retaining exact bytes in the
existing artifact/draft flow. Partial reads and changed versions are refused.
Already retained versions remain deliverable after source changes. Source links
open the recorded calendar month; building a link does not fetch events. Context
does not send invitations or modify calendars. Snapshots are preview-only and
private; no separate calendar record store is introduced.

Validation: server race tests cover distinct source identities, a 23-hour local
day, exact participant scope, source links without reads, partial preview/retain
refusal, stale versions, frozen native delivery, duplicate request recovery,
unqualified-source refusal and shared/public denial. Chromium uses the actual
Records picker for keyboard selection, restored kind, literal previews and source
links. A failed calendar preview removes the earlier selection action and allows
retry. Responsive checks pass and the phone-width screenshot was inspected.
Build, syntax and diff checks pass; full-suite and live evidence are recorded in
the plan checkpoint.

Production verification is limited to the deployed code and assets, without a
real calendar fetch or event/context write. This does not certify live-provider
or physical-phone acceptance, provider descriptions/attachments, or the remaining
unchecked workbench requirements.
