# Chat projects, models, and cleanup — September 11

User feedback: old chats obscure testing; no discoverable way to create a project; coding models require manual input; tabbed review repeats its title and navigation.

Implemented:
- Sidebar Projects + creates a named project, including empty projects. New chat offers project selection; project rows offer New chat. The landing also exposes project selection. Existing conversations can be moved with their Project menu. Existing inferred working-folder groups remain compatible.
- New coding chats have a Model dropdown and pass the exact selection to session creation. Existing coding chats expose model selection without requiring a duplicate “new coding session” provider. A different model uses the existing context-preserving continuation flow for subsequent messages; it does not mutate a running process.
- Codex choices come from the installed CLI's visible model cache, with configured defaults as fallback. Claude choices follow deployment configuration (Fable, Opus, Sonnet). Hidden models are excluded. Actual subscription entitlement/provider execution is not certified by this metadata list.
- Side-chat model selection uses the same catalog, inherits the current selection, and is visible without expanding advanced settings.
- Tabbed artifacts omit their redundant interior heading/back button. Version controls wrap on narrow panes; untracked files use compact filename/folder rows. Standalone artifact views retain their heading.

Cleanup: archived 25 audited old Manifest inbox entries using revision-checked merge. Three live process entries were archived from the inbox without stopping them. No task/provider/team history was deleted. Archive is reversible from the inbox filter. Before-state and exact inventory are saved privately in system/workbench/plans/chat-cleanup-2026-09-11/archive-backup.json. Entries created after the inventory were not included.

Validation: server/agentchat/chatthreads/artifacts tests and build; model catalog visibility/default test; model-picker race/current-model test; delivery fixture verifies exact selected model and project assignment through a route change; lifecycle, workspace tabs, terminal context, simple shell and artifact editor fixtures. Live-browser asset overlays verified project creation, model dropdowns, exact continuation payload and narrow layout with mutations mocked. No real agent messages were sent for QA.

Project creation here organizes Manifest conversations; it does not clone repositories or provision server folders. Working folder remains an explicit coding-chat field. Further visual refinements should follow real testing feedback.
