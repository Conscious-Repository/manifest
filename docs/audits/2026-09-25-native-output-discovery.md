# Saved outputs in continued conversations' Files view

Conversation-scoped artifact listing previously used only the logical runtime
scope. A native conversation continued from a terminal has its own concrete
conversation identity, so native outputs saved under that identity were omitted
from its Files query even after the context-acceptance fix.

The list now includes `chat-output` records from that exact native conversation
alongside the original runtime scope. Other artifact kinds do not acquire this
additional scope. Filtering uses one registry snapshot, retaining the registry's
update-time ordering and all existing kind/ref/harness/task/run constraints before
content search. Ordinary and terminal conversation queries keep their behavior.

Race-enabled tests cover distinct logical/concrete identities, both expected
scopes, exclusion of unrelated outputs and other kinds in the extra scope,
ordering, content search and kind/ref filters. Existing artifact search, saved
output delivery and exact handoff scope tests pass. Full repository/build/live
results are recorded in the plan checkpoint.

This changes the data returned to Files; the existing refresh control remains
available for an already-open list. It does not add automatic list polling,
broaden team access or infer links from filenames.
