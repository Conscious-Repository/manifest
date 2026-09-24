# Proactive tracked-reply notices — September 24

Verified reply polls now update one durable notice per sent operation in the
same operation-record save as the watch result. The latest reply watermark
survives dismissal and expiry, preventing repeated or older provider results
from resurfacing old mail. A later reply rearms the notice. Dismissal identifies
the operation and exact reply; an old card cannot dismiss a newer arrival.

The existing notices lane and badge project these records without executing
operations or reading a mailbox. Cards show subject, sender account and reply
author, and expire 14 days after the reply timestamp. The existing dismissal
endpoint routes their IDs to the operation store. Stopping monitoring retains
already received notices, including the automatic stop-after-reply outcome.

The card's `view replies` action fetches a private, read-only confirmed-email
receipt and renders the canonical email card inline. It does not run operation
reconciliation, approve or send. Notice list payloads contain no reply bodies.
Receipt previews remain literal text. Existing stopped watches are not restarted;
existing enabled watches begin producing notices on their next successful poll.
No scheduler, attention kind, approval lane or additional record store is added.

Validation:

- Manifest MCP race tests: first notice, repeat dismissal, restart/re-poll,
  later reply rearming, stale-dismiss refusal, older-result watermark retention,
  14-day expiry, manual-stop retention and unchanged fake send count.
- Sourcing journey race test: verified reply after canonical delivery, notice
  and badge projection even after automatic monitoring stop, exact receipt
  read, pending-receipt refusal and dismissal through the existing handler,
  without another send. Public portal rejects receipt, tracking and dismissal
  routes.
- Chromium uses the real notice renderer, shared card shell/actions, canonical
  email card and repository CSS. It covers notice→receipt, literal reply text,
  failed-read retry, refused-dismissal restoration, successful dismissal and
  320/390/1440px bounds. Phone-width screenshot inspected.
- Release build, JS syntax and diff checks passed. `make test` passed server
  (39.569s) and other packages except the known unchanged Hermes source-hash
  canary re-audit failure. Added privacy/pending-read assertions passed separately
  under the race detector.

Live verification is read-only: service/binary health and served assets. No
production monitoring, dismissals or mail sends are needed for verification.
This provides Feed notices, not operating-system push notifications. Provider
lost-ack reconciliation, personal sender support, actual mailbox/phone acceptance
and other unchecked workbench requirements remain open.
