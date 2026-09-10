# Shared original-plan editing

An exact task-plan artifact included in the reviewed conversation share grants
teammates access to the original vault plan section. Both AION and OODA expose
GET/POST `/api/chat/threads/{thread}/plans/{plan}` through their authenticated,
fixed-domain portal callbacks. Conversation JSON lists eligible plans.

The editor opens beside chat on desktop and has a return-to-conversation action
on phones. Save updates the original; restore is another save that appends a
version. Discuss selects the exact displayed immutable bytes for the existing
message composer. None of these plan operations launches a runtime or sends a
message. Only the subsequent explicit message send does that.

Writes compare `expectedRevision` against the live canonical section. Stale
writes return 409; missing grants, archived threads and changed artifact identity
are denied. Record-boundary headings cannot be inserted through the plan editor.
Only the current head, explicitly reviewed versions and edits attributed to this
shared conversation are listed. Private task IDs, paths, titles, descriptions and
other registry metadata are omitted. Generic uploads are not editable task plans.
Repeated reads grant attachment access idempotently per domain/thread/hash.

## Recovery limits

Vault replacement and artifact snapshotting are separate locked operations, not
a cross-file transaction. Old bytes are retained before replacement. A crash
between replacement and snapshot can leave the saved vault content without the
member-attributed revision receipt; a later read observes the actual vault bytes.
An external edit between operations is retained and returned as current rather
than overwritten. A successful no-op save need not append a duplicate version.

There is no request-ID replay protocol for plan saves. The browser persists its
draft and expected hash before submitting, marks uncertain requests for review,
and never automatically retries. Reload current shows authoritative bytes while
preserving the draft. After comparison, Use current as save base explicitly
permits another save. Draft storage is per portal origin, member, thread and plan;
it is local to that browser. Storage failures are surfaced so text can be copied.
Already authorized in-flight operations can finish as consent changes; subsequent
access checks deny archived or mismatched conversations. This is not retroactive
revocation of bytes already shared.

## Validation (Metis, September 10, 2026)

- `go build ./...` and `go test ./server ./teamportal ./artifacts ./agentchat
  ./chatthreads ./manifestmcp ./gmailsend` passed; no empty test selections.
- Shared-plan fixtures use the real vault writer, reviewed consent, both portal
  callbacks and fixture-only signed cookies. They cover original owner reads,
  actor/note attribution, restore, old bytes, conflicts, external edits, private
  history/description filtering, reviewed uploads, unrelated tasks, wrong domain,
  archive/identity rechecks and idempotent attachment grants. They check terminal
  receipts/registry, messages and private-agent queue state for unwanted changes.
- `NODE_PATH=/tmp/manifest-browser-qa/node_modules PLAYWRIGHT_CHANNEL=chromium
  node server/testdata/shared-chat.cjs` covers phone editing, restore, conflicts,
  uncertain-save/reload recovery, thread isolation, exact Discuss selection and
  sends, desktop panel bounds, plus existing terminal/file/delivery fixtures.
  Both shared hook copies and portal JSX views compile with Babel standalone
  7.29.0. Playwright and Chromium were installed outside the repository.
- Linux fixture portability fixes pass scripts to Node on stdin and explicitly
  advance a same-size transcript rewrite's mtime.

No real team message, email or terminal input was used for QA. Live signed-in and
physical-phone acceptance are still unverified. Broader decision permissions,
generic artifact editing and email monitoring remain outside this feature.
