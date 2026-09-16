# OODA partner email capture

Owner confirmed: each email thread must be confirmed first. Relevant mail remains private to its source member and the admin until confirmed. Extraction only produces proposals; it does not automatically approve tasks, decisions, or financial records.

Live mailbox syncs already cover ben@ooda.group, brian@ooda.group, and stephen@ooda.group. Found four historical confirmed conversations whose engine spools were consumed in August while the engine could not load the ooda-email ritual; no extraction receipts existed. Requests were also truncated to 4,000 bytes before the email note.

Changes:
- Targeted engine request limit for extractor/ooda-email now matches Manifest's 60,000-byte budget; regression verifies a long inline note survives dispatch. Engine source committed separately in the harness repository; previous binary backed up before deployment.
- A minute-based reconciler checks confirmed artifact hashes against durable run receipts and recovers missing handoffs serially. It never selects pending/dismissed threads or blindly replays an existing failure receipt.
- Confirmation remains explicit. Portal copy says “confirm & extract” and identifies tasks/decisions as reviewable suggestions.
- Feed shows the member's last successful mailbox scan or sync trouble; Archive distinguishes awaiting a receipt from a recorded extraction outcome.
- Tests cover recovery, queue deduplication, preservation of unconfirmed mail, failure-receipt no-replay, existing privacy boundaries, and conversation deduplication.

Live validation (2026-09-15):
- Manifest deployed at 3bb4c10; engine request fix committed as 6db8489.
- All four missing confirmed-thread extractions completed: 20260915-212407-8ab1, 20260915-212607-ecd4, 20260915-212707-d6a7, and 20260915-212807-a8a9.
- Four suggestions were written to pending approvals; two runs correctly produced no new items. Live authenticated Feed shows four proposed tasks/decisions with separate confirm/reject actions. No proposals were approved during verification.
- Live Feed displays successful sync time and explicit confirm & extract controls; Archive displays durable extraction outcomes. No pending email was confirmed during verification.
- Focused OODA, Gmail sync, and email-health Go tests passed; scheduler regression passed.
