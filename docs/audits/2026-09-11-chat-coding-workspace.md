# Chat robustness and coding-workspace audit

**2026-09-11 · Benjamin’s direct assignment · audit and recommendations only**

Audited checkout: `4e7906d5267bb4b1d2f1e0514e0c92b59ff28e14`.
The deliverable is this report and its reproducible fixtures. No product behavior
was changed, no deployment was performed, and no task was closed.

## Assessment

Manifest already has substantial foundations for the requested workflow: native
Codex/Claude transcript adapters, persistent runtime identities, recoverable sends,
versioned task plans beside conversation, explicit immutable artifact context,
and a captured working-folder diff. **The plan side panel exists. A selective
Git file-tree review surface does not.** Terminal’s Files browser does not fill
that gap: it is a separate fleet file manager, without Git change/hunk semantics.

Two behavioral defects were reproduced: automatic terminal attachment recovery
can stall after an outage, and Chat exposes herdr quick keys the adapter rejects.
There is also stale tmux-specific landing copy. None was fixed in this audit.
The passing existing suite did not catch the two reproduced sequences. These are
current defects; this audit does not establish the historical introducing commit.

The largest workflow gaps are (1) questions to a working board coding agent are
parked for a subsequent run rather than delivered to that run, (2) standalone
coding chats still use a terminal-like presentation, and (3) code review remains
one whole-folder snapshot without file selection, reviewed markers, or safe
revert. Build on the existing conversation/artifact contracts rather than making
another workbench or another plan store.

## Scope, evidence, and boundaries

Read `ARCHITECTURE.md`, `docs/ui-conventions.md`, current implementation, existing
fixtures, and the September 9 herdr execution audit. There was no checkout
`AGENTS.md`, no applicable ancestor file, and no nested file under `server` or
`docs`. The owner’s preference for a Codex-app-like workflow is the design target;
this is not a fresh vendor-product comparison or a claim about current vendor UI.

All requests used temporary records, controlled text, fixture sockets, or
intercepted browser requests. No production HTTP endpoint, real conversation,
email, owner terminal pane, or active daemon was modified. Unrelated untracked
files were preserved. No agent subprocess or model API was invoked by the audit.

Evidence has distinct levels:

- **Existing behavioral fixtures, freshly run:** full `server`, `agentchat`, and
  `chatthreads` suites; targeted race checks. Go handlers and real Unix socket
  adapter requests are exercised against fake herdr responses. Node fixtures
  exercise actual frontend functions with simulated browser/device state.
- **New observations:** the supplied Go overlay probes reproduce unsupported keys,
  parked board questions, native chat first-send/retry, and controlled headless
  commands. These are audit probes, not new automatically discovered product tests.
- **Real browser, fixture transport:** Chromium runs the actual Terminal and
  artifact-workspace JavaScript with actual styles and DOM. Network is blocked;
  WebSocket and xterm are fakes. This proves UI control flow, not end-to-end PTY
  rendering. Plan interactions and default/Jarvis layouts at 1440, 1000, and 390px
  were exercised. Desktop and phone screenshots were inspected.
- **Real PTY, fixture CLIs:** the production `boardLaunch()` command obtained from
  dispatched fixture runs executes under a real PTY with shell functions replacing
  `codex` and `claude`. Codex `exec --json` and Claude `--print --session-id`, pinned
  model arguments, JSONL tee, and durable exit=0 were checked. Only the constant
  TMPDIR setup is redirected to a temporary directory. These tests do not prove
  authenticated CLI compatibility, model availability, or real agent behavior.

The HTTP Chat launch/input fixture and PTY headless-command fixture are separate
checks, **not one browser → real herdr → authenticated CLI end-to-end test**.
Legacy tmux input/Keep behavior, exact herdr attach targets, and live inventory
endpoints were fixture-tested. A fresh real tmux/herdr daemon attach, actual
mobile keyboard, remote host, daemon restart, and authenticated agent state/Q&A
were not exercised. Historical live results in the September 9 report are useful
context, not credited as new passes here.

## Current defects to fix

### R1 · P1 · Attachment recovery stalls after an outage

**Trigger:** open an attached Terminal; disconnect the browser socket; leave
inventory unavailable when the one 1.2-second reconnect attempt fires; restore
inventory afterward, as a later SSE update or visibility refresh would do.

**Observed:** inventory returns `connected`, but the socket stays CLOSED and the
status remains **“Disconnected · retrying”**. No new socket is constructed.
Explicit **Reconnect** does recover.

**Cause:** `attachTerm()` schedules one timeout in `ws.onclose`.
`loadTermSessions(true)` only attaches when there is no `termInst`; a disconnected
instance still exists. Once the one timer has passed, quiet inventory refreshes
cannot finish attachment recovery. This is attachment failure, not proof of
process failure. The current unknown/unavailable process policy must be retained.

**Evidence:** `server/web/js/73-terminal.js`, functions `attachTerm` and
`loadTermSessions`; [browser reproducer](fixtures/2026-09-11-chat-browser.cjs).
Observed tuple: `{connectivity:"connected", sockets:1, ws:3,
label:"Disconnected · retrying"}`. Manual recovery raises sockets to 2.

**Recommended correction:** let refreshed inventory resume an outstanding
attachment when the socket is closed and the complete selected runtime identity
still matches. Bound retries; cancel on navigation, detach, or occupant change.
Do not resend any input during reconnect. Show an actionable disconnected state
whenever no retry is actually pending.

**Acceptance:** repeat outages longer than the first retry, recovery while
hidden, switching sessions during retry, replaced occupant, and explicit detach.
Recover only the exact selected pane; never launch a process or replay input.

### R2 · P2 · Chat’s Tab and Shift-Tab buttons fail on herdr

**Trigger:** in a live herdr coding chat, use the visible `tab` or `shift-tab`
quick-key button.

**Observed:** `herdrTerminalRuntime.SendKey` rejects `\t` and `\x1b[Z` as
`unsupported terminal key`; no `pane.send_keys` request is made. Ctrl-C and Up
pass the same fixture. Ctrl-D is also unsupported by this API, but is not in
Chat’s current quick-key list, so it is not counted as another visible Chat bug.

**Cause:** `chatTermQuickKeys` and shared `TERM_KEY_CODES` advertise more keys
than the herdr adapter’s mapping implements. The raw Terminal WebSocket forwards
bytes separately; this finding does **not** mean Tab is broken in raw xterm.

**Evidence:** `48-chat.js` (`chatTermQuickKeys`, `chatTermKey`), `73-terminal.js`
(`TERM_KEY_CODES`), `server/terminal_herdr.go` (`SendKey`), and
`TestAuditAdvertisedHerdrKeys` in the [Go probes](fixtures/2026-09-11-chat-probes.go.txt).

**Recommended correction:** define a backend capability contract for quick keys;
implement verified protocol mappings or omit unsupported controls with an explicit
explanation. Verify both button-to-HTTP and adapter-to-daemon behavior, including
dialogs and shared-chat routing. Preserve no-replay semantics for uncertain keys.

### R3 · P3 · Coding-chat landing still identifies the runtime as tmux

`renderChatTermLanding()` renders the literal **“metis · tmux”** even though new
local coding chats use herdr. Related source comments and empty-transcript copy
also describe older wiring. This is source-confirmed presentation debt, not a
claim that the running process silently falls back to tmux.

Render host/runtime from the actual selected or proposed destination. Keep
unknown identity explicit. For an unavailable transcript, explain the observed
condition and recovery action rather than assuming the rollout is “not wired.”

## QA results by requested edge case

| Area | Fresh result | Evidence / remaining limit |
|---|---|---|
| Terminal inventory and registry rail | Live herdr orphan panes appear without adopting them into saved chat history; dead registry history is excluded; replacement handles refuse close. The old Terminal registry/history rail has intentionally been retired. | `TestTerminalLiveInventoryIncludesOrphansNotHistory`, `TestTerminalLiveCloseRejectsReplacementHandle`, frontend live-surface tests. Preserve Chats history; do not rebuild the retired rail. |
| Legacy tmux cockpit and herdr attach | Legacy literal input, bracketed multiline paste, keys, resume, and Keep wrappers pass. Exact herdr handles and service executable resolution pass. | `terminal_input_test.go`, `terminal_keep_test.go`, `terminal_live_test.go`, `web_terminal_live_test.go`. No fresh remote-host or real daemon attachment test. R1 affects browser recovery. |
| working / blocked / done / idle | All labels remain advisory. They do not release the checkout writer or establish task completion. Exact occupant/session mismatches are refused. | `TestBoardHerdrLabelsCannotReleaseWriter`, runtime and event projection tests. Fake detector labels; no claim that installed CLIs reliably emit them. |
| Socket/SSE outage | Unknown/unavailable does not become failed, relaunch, or release the writer. Snapshot polling survives subscription stalls; subscription errors do not invalidate a successful snapshot. | Event/runtime/mapping/board fixtures and targeted race tests. Browser attachment reconnect is separately defective, R1. |
| Owner-comment suppression | Plain owner comments do not dispatch. Explicit Ask/mention routes are distinct. | `TestComposerModes`, `TestCommentIgnoresAgentAndAskNeverPlans`, new board-question probe. The noncoding Ask semantics in that named test do not imply coding Ask is read-only. |
| Direct assignment and models | Explicit/default models reach commands and durable records; malformed/cross-agent model requests fall back with a visible warning; parked dispatch retains override. | `board_models_test.go`, including `TestCodingModelFallbackAndRetry`. Fixture defaults observed: Codex `gpt-6-astra`, Claude `fable`. Provider acceptance was not tested. |
| Codex/Claude from Chat | A draft starts once on first message. Working-state multiline input reaches `agent.prompt`; duplicate request IDs return receipts without re-sending. Native JSONL parsing, exact identity, related/continuation context also pass. | New `TestAuditNativeChatWorkingInput` for both kinds, existing draft/transcript/continuation fixtures. A submitted prompt is not proof of a real agent answer. |
| Headless sessions inside runtime panes | Dispatched fixture run commands execute successfully in a real PTY with controlled Codex/Claude functions; expected arguments, output and durable exit verified. | `TestAuditHeadlessLaunchInControlledPTY`. Headless execution is not the same protocol as interactive chat steering. |
| In-thread Q&A while work runs | Native Chat can submit to a working runtime. Board-thread Ask parks, then launches a second run after current completion. | New working-input and board-question probes; `startCodingTask`, `relayToAgent`, `relaySweep`. Gap G1 below. |
| Plan/artifact revision beside chat | Preview alone does not send; Discuss selects the exact revision; edit/restore/review/CAS save works. Stale saves, external edits, exact context, and proposal receipt binding pass backend fixtures. | Browser fixture; `chat_artifacts_test.go`, `chat_plan_revisions_test.go`, `chat-load-race.cjs`. Generic artifacts are preview/version context; editable task plans have the save path. |
| Review working-folder changes | Snapshot includes staged and unstaged tracked differences against HEAD, lists untracked filenames, preserves the index, excludes untracked contents, and remains immutable after files change. | `TestWorkingChangesReadOnlyReview`; `workingChanges()`. Shared tree, not agent-exclusive changes. Remote/non-Git/unborn-HEAD and oversized output cannot provide this preview. |
| Undo / reversibility | Plan restore creates another version; preview and draft edits do not execute. There is no general run/file/hunk undo UI. Ending a process and forgetting history are not Undo. | Artifact browser/backend tests and source review. Changes snapshots lack complete recovery bytes for untracked/deleted work; they are not rollback checkpoints. |
| Draft recovery | Local/server revision checks, conflicting edits, focus recovery, navigation/upload races, and post-acceptance preservation of newer typing pass. | `chat_state_test.go`, Node draft/landing/upload/focus fixtures. Backup retention of `dataDir/chat-state` is architectural policy, not verified backup operations. |
| Cross-device send recovery | Second simulated browser recovers acknowledgement without resend; CAS merge preserves another record; absent/unconfirmed receipts remain pending; failure to persist recovery prevents dispatch. | `TestChatDeliveryAcrossDevices`, receipt concurrency/crash tests. Simulated independent devices, not two physical devices. Legacy tmux sends do not have herdr request-ID recovery parity. |

## Concrete delta toward the desired experience

### G1 · Give a working coding conversation an explicit question/steer contract

The board uses one active coding writer per checkout. Its busy gate correctly
prevents a second writer, but `relayToAgent` parks Ask as a pending relay and
`relaySweep` later starts another coding work order. The new probe verifies this
sequence. It does not silently drop the question; it also does not answer it while
the original run is working. This is a workflow gap under the current explicit
execution policy, not permission to remove the writer guard.

The UI should distinguish **message recorded**, **queued for a later run**,
**delivered to this run**, **agent working**, and **result ready for review**.
Provide an explicit way to question/steer the exact existing conversation where
the runtime supports it. A headless `codex exec`/Claude print invocation needs an
appropriate controlled continuation or a clearly separate question conversation;
sending text into its PTY does not establish a conversational input channel.
Selecting a recipient must not imply that an already-started run changed model.

Keep question/steer delivery separate from starting a new write-capable task.
If implementing a read-only Ask mode, record that as a deliberate change to the
current coding-assignment policy. Do not quietly change the existing owner rule
that coding assignments and mentions authorize work.

### G2 · Use native-chat presentation for coding conversations

The main directory is searchable/recent-first, with recipient selection and
optional work grouping. Native continuation code now supports coding recipients
and projects attributed turns; it goes beyond the older architecture note that
limited continuation to Hermes. Preserve native transcript ownership and exact
runtime links rather than copying histories to make the UI look unified.

However, standalone terminal-backed conversations still set `.chat-main.term`;
`chatTermPaintLines` uses prompt glyphs, raw output lines and a terminal surface.
This is direct tension with the requested comfortable chat experience. Reuse the
ordinary chat message anatomy for prose, readable assistant responses, and a
stable composer. Fold tool activity into inspectable rows. Keep raw terminal and
permission-screen inspection one deliberate action away. Do not hide actual tool
errors or CLI permission prompts behind a generic “thinking” bubble.

### G3 · Extend the existing plan panel; do not rebuild it

`chatOpenWorkingArtifact` already mounts the shared `artifactWorkspace` beside
conversation. Plans use the vault plan section and revision snapshots; restore
saves a new revision. The browser fixture confirmed desktop/tablet conversation
visibility and phone document-only mode with Back to chat. The component currently
switches at 900px, while the general phone band is 860px; resolve the 861–900px
behavior deliberately in the next layout pass.

Give the conversation one stable **Plan / Changes** inspector entry, preserve its
selected version and each pane’s scroll position when switching, and show whether
the owner is viewing an old version or editing a recovered draft. Make linked-task
plan discovery consistent across ordinary, standalone coding, and continuation
views. Keep “Discuss this version” distinct from opening, saving, or executing.

### G4 · Add a Git change tree and selective review

Today Changes captures one `.diff` artifact; the workspace renders that artifact
as plain `pre` text. Comparing two artifact versions compares entire snapshots,
not a navigable Git file/hunk review. Terminal Files is a general allowlisted
local/remote filesystem browser with upload/move/delete; it is not scoped to the
current run’s Git changes and does not provide this review model.

First add structured, read-only change inventory tied to the runtime’s resolved
repository/folder: path, status, staged/unstaged distinction, rename information,
binary/truncated indication, base revision and content identity. Render a
collapsible file tree, filter by changed/reviewed files, and load a selected file’s
diff lazily. Selection must mean **show/discuss this file or hunk**, not implicitly
stage, discard, approve, or send it. Mark a reviewed file stale when its content
changes. Treat new files, deleted files, renames, and large/binary files explicitly.

Show shared-tree attribution honestly. A directory shared by two agents or owner
edits cannot be labeled “changes by Codex” merely because Codex is selected.
A real per-run baseline/worktree contract is needed before offering that filter.

### G5 · Introduce reversible code actions only after review identity is sound

No existing Changes snapshot constitutes a complete undo mechanism. Design a
reviewable per-file/per-hunk restore with a captured preimage, expected current
content, actor/run provenance, and a durable receipt. Refuse if the owner or another
process edited the affected bytes after review. Preserve staged and unstaged state
intentionally; never use a blanket reset as “undo agent.” Retain recoverable bytes
for untracked files before any destructive operation.

Keep plan-version restore, code restore, interrupt/end process, and transcript
forget as distinct actions. Start with read-only review; add narrowly scoped code
restore after the concurrency and recovery contract is tested.

### G6 · Bring recovery and affordances to one consistent standard

Keep local + server drafts, request identities, acknowledgement recovery and
no-replay behavior. Make pending delivery visible near its message on every
supported route, including first-send landing → native chat, while retaining newer
typing. Show legacy tmux limitations explicitly until those sessions drain.
Expose supported quick keys from one capability source and fix R1 before adding
more runtime modes. Validate focus return, keyboard navigation, long file paths,
scroll ownership and actual phone keyboard behavior in the integrated product;
the isolated component layout check is not a complete accessibility certification.

## Recommended build order and completion gates

| Order | Bounded delivery | Gate before moving on |
|---|---|---|
| 1 | Fix R1/R2, refresh runtime copy, add permanent behavioral regressions. | Delayed outage recovery reconnects exactly once to the right occupant; no input replay; every visible quick key works or is unavailable with a reason. |
| 2 | Stabilize coding chat as a native conversation: shared message presentation, explicit recipient/run/model, truthful queued/delivered state and working-run question route. | Ask during a controlled long run has a documented delivery/answer path; no second writer starts; plain comments remain silent; headless limitations are visible. |
| 3 | Add read-only Git file tree and per-file diff in the existing inspector, with Plan / Changes switching. | Staged + unstaged + untracked + renamed + deleted + binary + large-file fixtures; keyboard selection; conversation and draft survive panel switches; no mutation or send from preview. |
| 4 | Add selected-file/hunk discussion and reviewed markers bound to immutable bytes. | Content changes invalidate review; selected context reaches only the intended recipient/version; unselected/private files do not enter context. |
| 5 | Add scoped code restore/checkpoints and explicit reversible actions. | Stale restoration refuses; index/working-tree semantics verified; recovery bytes and receipts survive restart; unrelated edits remain byte-identical. |
| 6 | Run integrated desktop/tablet/phone and two-device runtime QA. | Authenticated scratch Codex/Claude interactive + headless sessions, real herdr/tmux attach, actual keyboard/focus, long outage, restart, duplicate/lost send and final-result recovery. Never use owner sessions as probes. |

Stage 6 is the full integration acceptance pass; each earlier stage should still
run its own bounded scratch integration checks. Prefer these small deliveries over
a new editor framework, a permanent extra rail, or a second conversation store.
Keep Manifest tokens, quiet metadata, native controls, optional inspector, readable
prose, and the existing phone document surface. Conversation remains the primary
place to work; the plan, selected code, and terminal are contextual tools.

## Validation record and reproduction

Fresh commands completed successfully:

```text
go test -count=1 ./server ./agentchat ./chatthreads
  manifest/server       27.723s
  manifest/agentchat     0.076s
  manifest/chatthreads   0.005s

go test -race -count=1 ./server -run \
 'Test(TerminalEvents|TerminalInputReceipt|BoardHerdr|ChatPlanVersion|CodingDraft|CodingModelFallback|ComposerModes|TypedMentions)'
  manifest/server       12.718s

go test -overlay=/tmp/manifest-chat-audit-overlay.json -count=1 -v ./server -run '^TestAudit'
  manifest/server        0.113s
  R2 reproduced; parked Ask observed; both native first-send probes passed;
  both controlled headless PTY command probes passed.
```

Authenticated `TestHerdrLive*` tests are opt-in and were not run. The first browser
probe used invalid short fixture revision hashes; the save guard correctly refused
it. The fixture was corrected to 64-character hashes and rerun successfully. This
was a fixture error, not an additional product defect.

The final browser pass reproduced R1, verified manual reconnect, exact plan context,
restore/edit/review/save, zero page errors, no horizontal document overflow at the
three widths, and the expected conversation visibility. Runtime and artifact
transport remain fakes. Temporary logs: `/tmp/manifest-chat-audit-{tests,race,probes,browser}.log`.
Screenshots: `/tmp/manifest-chat-audit-{default,jarvis}-{1440,1000,390}.png`.
Key results are preserved above; those temporary files are not required to read
this report.

Recreate the Go overlay without writing a test into the shared checkout:

```sh
python3 - <<'PY'
import json, os
root = os.getcwd()
with open('/tmp/manifest-chat-audit-overlay.json', 'w') as f:
    json.dump({'Replace': {
        root + '/server/zz_chat_audit_test.go':
        root + '/docs/audits/fixtures/2026-09-11-chat-probes.go.txt'
    }}, f)
PY
go test -overlay=/tmp/manifest-chat-audit-overlay.json -count=1 -v ./server -run '^TestAudit'
```

For Chromium, use an existing Playwright installation; this audit added no package
or browser dependencies to the checkout. In the audited environment:

```sh
NODE_PATH=/tmp/manifest-browser/node_modules \
AUDIT_CHROMIUM=/home/benjamin/.cache/ms-playwright/chromium-1208/chrome-linux64/chrome \
node docs/audits/fixtures/2026-09-11-chat-browser.cjs
```

The audit probes intentionally assert the observed defects. Once a fix lands,
convert those observations into permanent tests asserting the desired behavior;
do not interpret a reproducer’s PASS as “the defect is fixed.”

The task remains for Benjamin’s review. No fixes or automatic closure are included.
