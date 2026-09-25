# Planning → coding → result backend journey

`TestPlanningCodingResultJourney` joins existing workbench contracts into one
regression for each task-backed coding adapter, Codex and Claude. No production
behavior changes were required by this journey.

The fixture creates a private Alfred planning conversation, an explicit canonical
link to one existing task, and a reviewed task-plan version. It then uses the
production assignment/launch path, with a fake herdr socket and temporary Git
checkout. The dispatched durable brief contains the reviewed plan. A later owner
plan edit leaves that brief unchanged, and the Chat HTTP response exposes no
completed coding result while the run is active.

The test writes a simulated result using the production result-file contract,
reopens the terminal registry and conversation store, and invokes normal result
ingestion. The Chat HTTP response returns one result with the correct adapter,
run, task and body. Capturing that result twice through the HTTP endpoint retains
one artifact revision with producing-run provenance and identical reference/hash;
its exact content resolves for subsequent discussion. Repeated ingestion leaves
one task result notice. The source transcript, later owner plan and assigned open
task remain unchanged; completion does not imply acceptance or task closure.

Validation:

- Focused race tests include this journey, stale result capture, immutable
  discussion context, and explicit-origin result isolation.
- Existing full-frontend Chromium thread-switching and receipt-polling regression
  passes, including draft/focus preservation and phone-width error navigation.
- Full repository test/build outcomes are recorded with the plan checkpoint.

Scope: the source link and reviewed plan are fixture setup, not browser gestures.
The provider result is simulated, not evidence of CLI execution, code correctness
or a push. Reopening two durable stores is not a full server/process restart.
The browser regression is separate from the backend journey; this does not certify
the complete browser planning journey, related-terminal-chat result return,
physical-phone operation, or concurrent live providers. Existing result tests
retain separate stale-hash and unrelated-origin refusal coverage.
