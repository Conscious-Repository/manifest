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
