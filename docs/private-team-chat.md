# Private Kairos and Zeck conversations

The cockpit's new-chat paths use the named Hermes profiles `kairos-private` and
`zeck-private`. Existing `kairos` / `zeck` routes remain team portal histories.
These are private counterparts with separate memory, not resumed team engines.
The roster distinguishes private and team entries. New-chat navigation never
falls back to team creation if a private profile is unavailable.

The private profiles use the ordinary owner-only agentchat store, attachment
pool, durable delivery receipts, and artifact scope. They do not spool into
Kairos/Zeck team harnesses. The old cockpit team creation endpoint requires an
explicit `audience: "team"`; portal-native creation remains a team action.

## Runtime provisioning

Profiles live under the host owner's `~/.hermes/profiles/`. Production provisioned
both on September 10, 2026 with the existing default model connection, no gateway
or alias, and no imported conversations. Do not change the sticky default profile.

The installed CLI supports `hermes profile create NAME --clone-from default
--no-alias`, but **this version also copies memory** despite its help summary.
For a newly created profile only, move inherited MEMORY.md and USER.md into an
owner-only setup backup and initialize empty active memory. Never repeat this
reset on a profile that already contains conversations. Preserve credentials in
host configuration, not this repository. Do not clone team history or start a
messaging gateway.

Replace inherited SOUL.md before use with the private agent's identity and these
rules:

- Assist Benjamin with AION (Kairos) or OODA real estate (Zeck).
- Separate memory and execution from the team agent; never imply shared runtime
  continuity or access to undisclosed conversation memory.
- Use explicit conversation context and selected artifact revisions. Ask for
  missing facts instead of inventing them.
- Keep drafts, notes, memory, and outputs in owner-only locations; do not write
  them into team reports, shared memory, or portal history as an answering side
  effect.
- Linking a task or workstream does not publish anything. Sharing requires an
  explicit owner action selecting its audience.
- External sending requires approval of sender, recipients, content and files.
- Plan edits remain versioned proposals; executing them is a separate request.

A no-tools QA turn for each production profile returned the exact requested
marker, with a completed private receipt, and was absent from the team's list.
The disposable QA chats were removed. This verifies the normal execution and
storage path, not a filesystem sandbox against an agent deliberately writing
outside its workspace.

## Sharing still outstanding

The owner chose to make the **whole conversation, including future messages**
team-visible on explicit sharing. That transition is not implemented by private
creation. It must reconcile the source transcript, retained attachments and
artifact versions, pending deliveries, and shared decision identity atomically
or through recoverable receipts. Do not offer a button that only toggles a label
or copies a one-time snapshot. Existing private conversations stay private until
that transition is implemented and explicitly invoked.
