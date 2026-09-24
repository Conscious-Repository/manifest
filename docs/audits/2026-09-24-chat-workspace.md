# Chat workspace audit — September 24, 2026

Owner requested a faster, clearer chat workspace inspired by Codex, while keeping Manifest's shared UI conventions. This release is a bounded milestone, not desktop-Codex feature parity.

## Findings and corrections

| Area | Finding | Correction / evidence |
| --- | --- | --- |
| Finished run | Codex conversation `3309fbc65d7d9fa5` has explicit completed transcript evidence but an open native process with pending questions. The sidebar appeared blocked. | Show **Run finished · input pending**, separately from process and task acceptance. Current working state supersedes prior completion. Disconnection remains explicit. Claude `stop_reason: end_turn` now supplies completion evidence; a new owner turn supersedes it. |
| Initial paint | Draft and read-position recovery blocked rendering an already-fetched transcript. | Paint the transcript first; recover composer state afterward. Restore the reading bookmark only if no intervening reading gesture occurred. |
| Background work | Native transcripts polled every 1.5 seconds even after completion; screen capture was requested whenever a process existed. | Active/pending transcript interval 750ms; settled interval 10s (roughly six reads/minute versus forty). Runtime transitions force reconciliation. Hidden embedded panes skip periodic tails. Screen reads are limited to input-blocked sessions and visible terminals. These are polling targets, not an end-to-end latency promise. |
| Rendering | Existing keyed rendering and stage cache were already strong. | Retained them. Browser fixture with 300 history turns: append reparsed one response and retained the original row; measured about 0.8ms locally. |
| Native commands | Ordinary sends add context, which can turn `/goal` into prompt prose. | Separate single-line command envelope bypasses context wrapping, preserves durable request-ID receipts, and never silently stages a command after a busy response. A regression sends `/status` from a side-context session and checks exact bytes plus no replay. |
| Discovery | Composer offered no slash affordance. | Codex/Claude shortcut picker with keyboard navigation. Other installed commands can be typed. Results and interactive menus open in the actual terminal drawer. Commands depend on the installed CLI, account and extensions. |
| Sidebar / panes | The conversation list consumed width and existing workspace tabs showed one surface at a time. | Chats toggle (Ctrl+Alt+B); Focus, Split, Workbench and Four panes presets. Open existing independent conversations, side chats, activity, plans or files. Hiding preserves mounted tabs/drafts. Narrow screens show the selected tab only. |
| Activity | Tool completion with an empty result was indistinguishable from a started step. | Explicit completion flag, completed/error glyphs and accessible labels, latest tool in activity summary; existing expandable details and scroll/focus preservation retained. |
| Browser preview | Aside exposes errand results; no current agent-browser screen stream was found. Aside CLI was absent from the inspected noninteractive server PATH. | No fake live preview added. A supported session/screenshot stream and its lifecycle remain follow-up work. |

## Boundaries

- Commands are for private herdr Codex/Claude sessions. Native menus use the terminal; this is not a recreated menu system.
- Commands wait until the native agent is ready. Busy commands retain the draft instead of entering the ordinary follow-up queue.
- `/new`, `/clear`, `/fork` and `/resume` are refused from the composer because they change native conversation identity without updating Manifest's binding. Use New chat / Side chat. Do not claim the full native command roster is integrated.
- No new model execution, email delivery, task acceptance, or queued user instruction was required for the visual audit.
- Multi-pane state uses existing per-conversation workspace persistence. Layout/sidebar preferences are local-device UI preferences. Small screens retain all tabs but display one at a time.
- True token-event transport, native session remapping, and live browser video/screenshot preview remain outside this milestone.

## Verification

- Real Chrome against live read-only API data with local release assets: sidebar hide/show, independent existing conversations, 2–4 surfaces, Focus restores full width and retains draft, slash filtering and Enter selection, responsive fallback at 900px and 390px, no page overflow.
- Read-only preview rejects writes deliberately; its draft-sync warning is a preview limitation, not evidence of a production regression.
- Focused chat/native/Claude/CSS tests passed locally; build and JavaScript syntax checks passed.
- Browser conversation fixture passed desktop/phone and both themes, including nested activity disclosure, focus and keyed-row retention. Updated its stale attachment stub and expected activity label.
- Linux full repository suite: server and other affected packages passed. Existing `cmd/re-intake-canary/TestCanarySourceCallGraphIsolation` fails because hashes for `hermes/authority.go` and `hermes/claude_successor.go` need re-audit. Neither package/file is changed by this release. The Mac full suite also exposes existing Linux-specific bounded-execution/path failures; it is not reported as green.
- Final Linux server suite passed after the corrections. Deployment and native smoke results are recorded below after verification.

## References

- [Codex native commands](https://developers.openai.com/codex/cli/slash-commands)
- [Claude interactive commands](https://code.claude.com/docs/en/interactive-mode)
- Canonical owner plan: `system/workbench/plans/2026-09-11-manifest-agent-workbench.md` in the vault.
