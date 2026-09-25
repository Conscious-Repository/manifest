# Conversation and workspace recovery from server records

The research browser/backend journey now forwards every chat-state slot to the
real Go server, alongside artifact reads and saves. Its conversation descriptors
use valid production key grammar instead of the earlier fixture-only short keys.

After selecting the recovered artifact for discussion, the phone writes an
unsent instruction and confirms draft/workspace synchronization. An initialization
script clears local storage before the reloaded application executes, preventing
pagehide or older browser recovery copies from supplying the result. The real
server records restore the instruction, exact artifact revision and workspace
tab. The subsequent two-browser editor conflict journey remains intact.

The Go wrapper additionally reads the persisted conversation draft and workspace
record after the browser exits. It verifies the instruction text, selected
artifact ID/hash and retained artifact tab, in addition to the prior immutable
history and editor conflict assertions.

The integrated test passes explicitly under `-race` with Playwright available;
the standalone fixture mode also passes. Full repository/build/live results are
recorded in the checkpoint. No production behavior change was required.

Limits: conversation messages and provider/inventory responses remain fixtures.
This proves recovery without local browser storage using an uninterrupted Go
server, not a process restart, full server outage or physical-device trial.
Synchronization is explicitly awaited to distinguish confirmed persistence from
an interrupted pending write. The Go test skips when Playwright is unavailable.
