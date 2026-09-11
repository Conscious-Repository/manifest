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
