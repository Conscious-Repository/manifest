# Discuss a saved output in its source conversation

The initial Save output implementation exposed the artifact inspector's Discuss
action, but standalone message acceptance recognized only runtime-change
snapshots as conversation-owned context. A saved native output therefore needed
an unrelated explicit-context path or a task before it could be sent back.

Context validation now recognizes `chat-output` artifacts belonging to the exact
source conversation. Native dispatch and native-source handoff validation also
provide the concrete conversation key, since a continued native conversation may
have a different logical runtime scope. Additional keys apply only to saved
native outputs; runtime-change scope rules remain unchanged. Terminal origins do
not acquire fabricated Hermes identities. Unrelated conversations still require
an explicit private selection or a validated exact-version handoff.

The race-enabled HTTP journey captures a completed native reply without a task,
edits its artifact head, rejects an unrelated conversation, then sends the retained
original version to the source's fake runner. It checks exact reference/hash,
selected bytes, absence of the later edit, empty task context, completed receipt
and idempotent retry. Scope tests cover continued native source identity and
handoff refusal for an unselected later revision. Existing onward-handoff tests
and full-frontend output capture/discussion checks pass.

The Context-selection Node fixture passes from its expected `server` working
directory. Full repository/build/live results are recorded in the plan checkpoint.
This change does not grant team access, automatically attach saved replies, or
run a live provider during verification.
