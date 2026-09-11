# Chat UX acceptance record

Scope: the owner's Codex reference screenshots and requested Manifest affordances, on desktop and phone. This is a working acceptance record, not a claim that Manifest reproduces every Codex capability.

| User path | Current behavior | Evidence / remaining verification |
| --- | --- | --- |
| Find and organize chats | Project/workstream headings, standalone Recent, search, pin, rename, archive, Trash and restore | Lifecycle, pin and live sidebar checks. Folder grouping is inferred from exact host/path; group expansion lasts for this page. |
| Start a chat | Explicit agent chooser, recent coding folders, first send creates the session | Live desktop/phone navigation and landing/delivery fixtures. No model send during UI QA. |
| Read long conversations | Quiet activity disclosures, preserved reading position, floating latest action | Conversation-layout, reading-anchor and latest-browser fixtures; live scroll checks. |
| Compose and resume | Full-width input, recipient/model footer, draft recovery, bounded pending-send notices | Delivery/device/focus/upload race fixtures. Physical-phone keyboard remains unverified. |
| Change agent | Explicit recipient choice with history/selected-file context, existing work continues | Existing terminal/planning continuation coverage. Real provider execution was not exercised during UI QA. |
| Open terminal | Resizable bottom drawer; close detaches the view, distinct from stopping the process | Terminal-context and recovery fixtures. Real active provider PTY was not driven during UI QA. |
| Open workspace | One toggle, tabs, plus chooser, hide preserves tabs and unsaved edits, chat expands back | Workspace-tabs and pane-resize fixtures plus live desktop/phone checks. |
| Side chat | Saved related conversation with bounded parent context, same agent/model defaults, explicit switching | Workspace-tabs fixture plus server coverage from tabbed-workspace release. Real side-agent send remains unverified. |
| Plan/file review and edit | Version selection, exact-version discussion, direct supported text editing, reversible saves | Artifact editor/diff and workspace draft-retention fixtures. Code previews preserve literal source text. |
| Review working changes | Per-file diffs, expandable untracked list, honest empty state | Review-paths browser fixture. Selective staging is outside the current supported UI. |
| Share | Private-first; explicit whole-conversation sharing; team terminal control; owner-only external approvals | Share-review/shared-create fixtures and earlier server checks; no live sharing or external actions in this audit. |
| Phone navigation | One active primary surface, workspace return, touch-sized controls, bounded composer | Emulated 390px live checks and browser fixtures. Physical phone/virtual keyboard and long-running reconnect need user-device feedback. |

## Remaining acceptance work

- Exercise a sustained, representative multi-session workflow with the owner's normal provider sessions, including a side conversation, terminal reconnect, and plan revision. Current UI checks deliberately do not submit real messages or drive provider terminals.
- Verify physical phone keyboard, selection, dictation, and reconnect after backgrounding. Browser viewport emulation does not prove these behaviors.
- Continue checking readability and action discovery against the supplied Codex screenshots; retain Manifest's restrained blueprint theme. General browser/file-tree capabilities and selective Git staging have not been added merely to match screenshots.

Native Codex computer use is blocked in this environment. Comparison uses the owner's screenshots, not an asserted live Codex inspection. The implementation history and detailed limits are in `2026-09-11-chat-interaction-paths.md`.

## Final integration check

At deployed `eeaa69e`, `go test ./server ./agentchat ./chatthreads ./artifacts` and `go build ./...` passed. The deployed workspace, reading-navigation and response-copy walkthroughs passed at desktop and 390px phone widths; copy used a mocked clipboard. Earlier fixture results above cover mutating flows without real publication or provider input. The user's vault plan was updated with the current release and superseding project-grouping decision.

The requested UX implementation across the existing capability set is complete. Physical-device and normal provider-session use remain validation limits and sources of subsequent feedback, rather than claims made from browser fixtures. No broad Codex feature parity, unimplemented roadmap features, or real external-action execution is asserted.
