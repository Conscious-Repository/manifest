# Chat attention and priority checkpoint

The inbox now filters running/queued work, input needed, pending artifact review or revision, and failures. Execution and artifact review remain separate: a running conversation can have an earlier output awaiting review. Terminal observations cannot prove a completed result; stopped processes override stale working observations. Durable planning delivery receipts can establish completion or failure.

Session summaries expose canonical conversation descriptors. The private review summary counts only current artifact heads, keyed by exact scope or explicit task linkage. No title matching, automatic acceptance, or external-action approval is introduced. Unscoped task artifacts are counted once per linked task.

Manual low/normal/high/urgent priorities use the existing vault project record and record revision checks. They affect ordering after pins and before recency. Concurrent unrelated writes are merged; a changed priority for the same thread requires a fresh choice. Unknown fields and project context are retained.

Validation: full server suite and Go build passed; focused Node tests cover advisory runtime states, stopped/working conflicts, durable receipts, independent review counts, task deduplication, priority compare-and-swap retry and conflict preservation. Tests are wired into the server suite. The existing pin fixture now provides its browser event-listener dependency.

Limits: this is not native run-event normalization or the complete workbench journey. Real provider execution, comprehensive sidebar layout/interaction audit, unread tracking, durable inspector restoration and release validation remain outstanding. Not deployed.
