# Chat interaction audit — September 11

Reference: owner's supplied Codex screenshots and requested conversation-first workflow. This is a comparison to those visible interactions, not a claim of complete Codex feature parity.

| Path | Problem | Revision |
| --- | --- | --- |
| Open More | A full work-order prompt displaced actions; Rename and End appeared joined | Prompt becomes an optional Conversation details disclosure; actions receive separate full-width hit targets |
| Dismiss menus | Expanded details could linger over the conversation | Outside pointer dismissal and Escape-to-close/focus return |
| Choose next agent | Copy mixed recipient selection with creating a separate conversation; three competing buttons | Short explanation, agent choice and Use agent; related-chat creation remains in More |
| Select coding agent | Working folder/model appear only when needed | Preserve conditional fields, using the same existing creation and recovery handlers |
| Open Changes | Runtime/path metadata and untracked filenames dominated a raw dump | Changed-file count, per-file selector and literal colored diff; metadata and untracked filenames collapsed separately |
| No tracked changes | Long technical output looked like a failed or incomplete review | Explicit No tracked changes state; untracked files remain discoverable and are not represented as reviewed content |
| Discuss/edit/restore | Must retain exact selected revision | Existing immutable version selection and discussion hooks retained; rendering does not alter snapshot bytes |
| Resize/open/close side pane | Must preserve conversation and draft | Existing splitters and view lifecycle retained; tested phone bounds |

Verification uses isolated browser fixtures with actual renderer/dialog code. Covered desktop/390px header, More controls, Terminal/Conversation switching, per-file selection, untracked disclosure, literal script-like text, empty review, coding fields, cancellation, and dialog bounds. No real agent input or external action was dispatched.

Remaining limits: Changes is a captured working-tree review, not a live filesystem browser or selective Git staging UI. Untracked file contents are not included. The recipient chooser preserves the existing execution semantics; switching agents does not redirect work already accepted. Physical-device acceptance and real-send UX remain owner testing. Further changes should follow concrete feedback rather than adding all controls visible in Codex.

## Task and terminal context follow-up

The coding transcript is the execution conversation; the task activity thread contains task comments and run summaries. These are not identical records. The coding header now offers Task details in the existing task panel while preserving its originating chat route, instead of routing into another chat. Closing or continuing from the task panel returns to that conversation. Task activity remains reachable through existing task routes; this is not a data/history merge.

Terminal deep links now reveal the selected pane before reusing an existing attachment, reset a deliberately paused attachment on explicit navigation, and expose Back to conversation for a chat-origin link. A confirmed ended process remains ended: the empty state explains how to resume from chat. No automatic launch or input replay was added.

Guidance: [OpenAI's Codex introduction](https://openai.com/index/introducing-the-codex-app/) describes reviewing work within the thread; [NN/g consistency and standards](https://www.nngroup.com/articles/consistency-and-standards/) supports predictable labels and navigation. Applied here as context-preserving task inspection and exact-session terminal navigation, rather than claiming identical underlying task/terminal semantics.

Fixture coverage: reopening an existing exact attachment reveals it without reconnecting; opening and closing task details retains the coding-chat route. Terminal recovery and backend tests also run. Real terminal commands are not used for QA.


## Conversation lifecycle and bottom terminal follow-up

User correction: Terminal docks below the conversation, as in the supplied Codex reference. It reuses the existing xterm stage, socket identity and recovery logic. It does not launch an agent. The drawer has a draggable/keyboard-resizable top edge, remembered height, and a close action that detaches the viewer only. Files/Changes retain the side workspace. Leaving chat restores the terminal stage to its standalone page.

Conversation row actions now separate Pin, Rename, Archive and Delete from Stop agent. Archive and Trash are owner-only, server-persisted UI organization with revision conflict protection; Restore is available in both lists. Delete explicitly moves to Trash, preserving provider transcripts, linked task history and running processes. It is not physical transcript erasure. Stop is shown only for positively live processes, and cannot become a repeated history-deletion action. The same lifecycle actions are used in the conversation header.

Row menus are viewport-positioned and keyboard traversable. Changes gets a full-width version selector and shorter Compare/Discuss actions. Immutable snapshot semantics remain unchanged.

Validation: full server and chatstate suites, Go build, fixture browser checks for archive/restore/Trash and cross-device conflict merge, menu viewport bounds, embedded exact terminal identity, outage recovery, stopped-to-running inventory changes, drawer close/restoration, keyboard resizing, desktop/phone layout, artifact editing and exact-version discussion. QA uses controlled sockets and files; no real agent input or external messages. Physical-device keyboard behavior is not certified.


## Live browser follow-up: panel closure and navigation

Walked the authenticated live Manifest surface in an isolated Chromium session, including Changes open/close, the stopped terminal drawer, More, agent chooser/Cancel, phone conversation/list filters and New chat. Compared screenshots with the user's Codex references; native Codex automation itself was denied by the computer-use tool. No agent messages or terminal input were sent. Opening Changes captured review snapshots through the normal UI.

Measured defect: at 1512px viewport, chat started at 964px but remained 482px after closing Changes because its inline flex-grow stayed 0.5. On phone the same constraint halved chat height. The sizing routine now removes that constraint whenever a side pane is absent or mobile layout applies. Live browser checks with patched assets restored 964px desktop width and full phone height; zero page errors.

Further corrections from the walkthrough: agent/workstream filters now expand from a readable Filters control, More/review actions use readable sans-serif text, and disclosure arrows identify expandable review details. Phone has one Chat header; its plus opens a new conversation instead of the global task capture form. A stopped embedded terminal hides unavailable connection/keyboard actions. These checks cover viewport emulation, not a physical phone keyboard.

## Tabbed right workspace and contextual side chat

The common chat header now has a Workspace toggle. The pane has keyboard-accessible tabs, an Add tab chooser, and separate Hide and Close-tab controls. Hide and Back to chat preserve connected artifact editors and side-chat documents; the main conversation regains its available width. Plan and saved-artifact actions come from the linked task. Opening an already-open artifact selects its tab; explicitly requested revisions refresh without overwriting an active edit. Terminal remains the independent bottom drawer.

Side chat creates a private, saved related conversation through the existing idempotent related-chat endpoint. It defaults to the current recipient/model, permits an explicit alternative, and inherits the parent’s existing workstream. Creation does not send or start a runtime. The server captures bounded attributed parent context, excludes tool traces and attachment bytes, and retains the snapshot across retries and explicit recipient switches. Replies belong to the side conversation and are not projected back into the parent's timeline. Existing artifact scope and external-action approval rules remain in force.

The side document reuses the actual chat application in a same-origin iframe with navigation chrome suppressed; it has independent composer/outbox/recipient state rather than borrowing the parent chat's singleton globals. Closing the tab does not delete the saved conversation or stop its process. Tabs are retained while hidden within the current page; reloading or navigating away closes workspace views. Saved conversations and synchronized artifact/chat drafts remain recoverable. This release does not add a general filesystem browser, web browser tab, or side-chat creation from shared portal sources.

Validation: server/agentchat suites include side-chat creation for planning/native sources and planning/coding destinations, unchanged parent state, no dispatch, immutable context, same model, idempotent recovery and changed-intent rejection. Browser fixtures exercise the real artifact editor across hide/show and tab switches, model defaults, keyboard tab navigation, mobile return and disposal. Live Manifest was inspected with local assets overlaid and side-chat creation mocked, including the real embedded composer at desktop and 390px phone widths; no model messages or terminal input were sent. A long context title initially pushed the phone composer offscreen; the final compact disclosure keeps the composer in the viewport. Native Codex comparison uses the supplied screenshots because its computer-use surface is blocked. Physical-phone keyboard and real authenticated model execution remain outside these checks.

### Compact workspace presentation

Follow-up to the owner's 13:23 screenshots: replaced the large explanatory cards with a centered icon-and-label chooser; removed the synthetic Open tab, absence/help copy, and duplicate working-folder snapshot entry. Saved artifacts live under Files, while Review captures the current folder. The plus opens an anchored picker over existing content; selecting a tab dismisses it without resetting editor state. The header uses labeled terminal/pane icons and a compact options menu. Tooltips retain secondary explanations. Checked the live app with overlaid assets at desktop/phone widths and the tab/editor fixture, including popover visibility without hiding an active editor.

### Project sidebar and composer

Project headings group coding conversations by exact working folder and host; an explicit workstream assignment takes precedence. Same-name folders remain distinct and show their paths. Groups show five recent chats plus active/pinned entries, with Show more and collapse controls; search reveals matches. Unassigned conversations remain under Recent. This is a presentation of existing metadata, not a project migration. Expansion state lasts for the page session.

The recipient/model picker now sits beside the message input in the composer footer. Runtime guidance is separate from the placeholder. Phone Enter adds a newline, and Ctrl/Cmd+Enter sends; the existing IME guard remains. Input height is bounded against the visual viewport. Validation: lifecycle/project grouping, workspace artifact drafts, simple-shell and resize browser fixtures; desktop/phone live reads with local assets overlaid; JavaScript syntax and duplicate CSS checks. No model messages or terminal input sent; physical keyboard testing remains outstanding.

### Starting a conversation

New chat now opens a compact, cancelable agent chooser instead of inserting a second native dropdown into the page header. Coding landing uses a labeled working-folder input with recent local folder suggestions; backend launcher jargon and desktop-only Enter instructions were removed. Pending delivery recovery remains available under a counted disclosure, with bounded scrolling, so old uncertain sends do not consume the phone composer. Existing exact-request retry and receipt handling remain unchanged. Live reads with local assets verified chooser navigation, folder suggestions, mobile layout and expanded recovery controls without creation, retry, or model sends. Delivery recovery regression and CSS checks passed.
