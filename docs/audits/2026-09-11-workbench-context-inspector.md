# Recorded conversation context

Added a persistent Context tab with session agent/model/folder, optional project instructions and editing, linked task, and a selectable record of each available user instruction. Recorded recipients and artifact revisions come from delivery/submission data; they are not inferred from the currently selected agent. Artifact links open the exact supplied revision. Attached files reuse existing preview routes. Saved parent-context snapshots are available when returned by the conversation API.

Current project instructions are explicitly distinguished from recorded inputs. Existing conversations are not rewritten when project instructions change. The view is private, subscribes to existing transcript updates, and retains selection/disclosure/scroll with the workspace. No extra fetch or execution loop is introduced. Unknown inputs are not fabricated; this view is limited to metadata available from the current adapters.

Phone visual inspection found the active tab could be scrolled outside a crowded strip. The tab strip now reveals the active tab when shown or resized, without scrolling the conversation. Its resize observer is disposed with the workspace.

Validation: full Go suite before the final tab reveal adjustment; server suite after it; actual Chromium workspace fixture for restoration, exact revision callback, per-instruction isolation, inert markup, phone overflow and active-tab visibility. Phone screenshots were inspected. No production changes or real agent sends.

Remaining: first-class links for all specified Manifest record kinds, capability/skill context, complete cross-adapter run/event normalization, real end-to-end journeys and release gates. The original plan remains active.
