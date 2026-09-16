# Agents runtime audit — 2026-09-15

## Findings and changes

- AION email: primary `ben@aion.bio` read-only Gmail grant is revoked (`invalid_grant`). September 15's engine report says completed despite a failed `email.sync_check`, no scan, and zero writes. User must reconnect Gmail under Settings → Portals. Agents now projects current Gmail failure alongside the historical runtime result and offers the reconnect path. Removed references to the already-retired waiting-on digest from connection copy.
- OODA: separate Manifest mailbox loop, every ten minutes, with three connected accounts including the owner's `ben@ooda.group`. Token checks were current; 190 pending mailbox copies awaited review, with recent September 15 mail. Agents now shows the deduplicated pending conversation count. Pending mail remains subject to existing confirmation; nothing was automatically approved.
- OODA reliability: listing formerly stopped at 100 threads and advanced to the newest message. All result pages are now consumed. A failed thread fetch or failed candidate write retains the previous checkpoint while processing healthy threads. The loop also runs immediately on startup. Regression tests cover multiple pages, later-page failure, and a missing thread among successful reads.
- `warden/audit`: retired in both live markdown and Manifest launch policy. The last run admitted a 152,637-character truncated snapshot and unexamined sections while reporting completion. Historical files retained; original ritual definition backed up on Metis. Engine registry confirms paused.
- Granola and Pocket: September 15 runs fetched real source data and correctly skipped existing notes. Retained: removing the engine would break these connectors and on-demand extraction. Zero newly written notes is not a failure for an incremental sync; removed that false health warning for the three sync connectors.
- Alfred domain scout: latest September 15 run reports three feed writes after recent failed runs. Retained. Existing paused briefing, waiting-on, skill-cast, and re-intake jobs remain retired.

## Remaining dependency

The old engine is not fully removed: email, Granola, Pocket, and on-demand extractors still depend on its cast implementations and approval pipeline. This audit retires the ineffective weekly audit, not working ingestion paths. Full runtime migration needs equivalent replacement connectors before disabling that service.

## Validation

Gmail sync package and spirits package tests pass. Focused Agents, retirement, email-health privacy, and CSS tests pass. Full server run encountered pre-existing real-estate intake storage failures; isolated baseline reproduction checked separately. No outgoing email or pending email approval was performed.
