# Email recovery projection refresh — September 25

Email recovery previously replaced only the clicked card and notified recruiting.
Cached Chat, task and Feed projections could still show the old uncertain receipt
when rendered again. Recovery now uses the shared refresh function also used by
approval decisions and reply-watch changes: refresh the open Chat and task,
reload todos, announce the canonical approval ID and refresh Feed. Recruiting's
existing listener reloads the candidate's operation set and its outcome actions.
These are refreshes of existing canonical records, not copied receipts.

The actual shared-card Chromium fixture asserts that a failed recovery invokes
none of these refreshes; successful recovery invokes the Chat/task/todo/event/Feed
paths with the correct identities. The recruiting browser journey now starts
from a partial operation, confirms no outcome action is available, checks delivery,
and observes the enclosing Record sent outcome action appear. Recording then
completes using the same operation, without another approval/send request. Existing
reply-watch, approval preparation/retry and candidate isolation checks still pass.
320/390/1440px bounds pass and the resulting phone screenshot was inspected.

Syntax, diff and release-build checks pass. Full-suite and read-only deployment
results are recorded in the workbench plan checkpoint. This closes a measured
projection gap; it does not establish cross-device push delivery, live Gmail
lost-ack acceptance, or completion of other unchecked workbench requirements.
