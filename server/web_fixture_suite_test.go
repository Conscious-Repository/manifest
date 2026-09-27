package server

import (
	"os/exec"
	"path/filepath"
	"testing"
)

// The standalone browser and node fixtures in testdata/ that had no Go
// wrapper, so neither `make test` nor `go test ./server` ran them. Each test
// names one fixture; the comment is the change that introduced it. Browser
// fixtures skip only where Playwright does not resolve (set NODE_PATH to a
// node_modules that has it); node-only fixtures run wherever node exists.

// runFixture runs testdata/<file> with node. fromRoot fixtures resolve web
// assets against the repository root, so the working directory is set
// explicitly rather than inherited from whoever invokes the test.
func runFixture(t *testing.T, file string, browser, fromRoot bool) {
	t.Helper()
	node, err := exec.LookPath("node")
	if err != nil {
		t.Skip("node unavailable")
	}
	if browser {
		if err := exec.Command(node, "-e", "require.resolve('playwright')").Run(); err != nil {
			t.Skip("playwright unavailable (set NODE_PATH to a node_modules that has it)")
		}
	}
	script, err := filepath.Abs(filepath.Join("testdata", file))
	if err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command(node, script)
	if fromRoot {
		cmd.Dir = ".."
	}
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("%s: %v\n%s", file, err, out)
	}
}

// Apply responsive interaction safeguards across Manifest screens (app-responsive.cjs).
func TestFixtureAppResponsive(t *testing.T) { runFixture(t, "app-responsive.cjs", true, false) }

// Review immutable diff hunks and preserve their collapsed state (artifact-hunk-review.cjs).
func TestFixtureArtifactHunkReview(t *testing.T) {
	runFixture(t, "artifact-hunk-review.cjs", true, false)
}

// Highlight immutable diff hunks without changing review coordinates (artifact-hunk-syntax.cjs).
func TestFixtureArtifactHunkSyntax(t *testing.T) {
	runFixture(t, "artifact-hunk-syntax.cjs", true, false)
}

// Preview saved link destinations in the artifact workspace (artifact-link-preview.cjs).
func TestFixtureArtifactLinkPreview(t *testing.T) {
	runFixture(t, "artifact-link-preview.cjs", true, false)
}

// Show exact-version metadata for unsupported artifact previews (artifact-metadata-review.cjs).
func TestFixtureArtifactMetadataReview(t *testing.T) {
	runFixture(t, "artifact-metadata-review.cjs", true, false)
}

// Show artifact origin and exact revision provenance in review (artifact-provenance.cjs).
func TestFixtureArtifactProvenance(t *testing.T) {
	runFixture(t, "artifact-provenance.cjs", true, false)
}

// Reconcile pending artifact reviews before retrying decisions (artifact-review-recovery.cjs).
func TestFixtureArtifactReviewRecovery(t *testing.T) {
	runFixture(t, "artifact-review-recovery.cjs", true, false)
}

// Add version-bound artifact review decisions and diff line gutters (artifact-review.cjs).
func TestFixtureArtifactReview(t *testing.T) { runFixture(t, "artifact-review.cjs", true, false) }

// Recover artifact text saves through atomic version receipts (artifact-save-receipt.cjs).
func TestFixtureArtifactSaveReceipt(t *testing.T) {
	runFixture(t, "artifact-save-receipt.cjs", true, false)
}

// Persist artifact edit recovery state before saving versions (artifact-save-recovery.cjs).
func TestFixtureArtifactSaveRecovery(t *testing.T) {
	runFixture(t, "artifact-save-recovery.cjs", true, false)
}

// Highlight code artifacts with exact-source plain text fallback (artifact-syntax.cjs).
func TestFixtureArtifactSyntax(t *testing.T) { runFixture(t, "artifact-syntax.cjs", true, false) }

// Add source-anchored CSV and TSV artifact review (artifact-table-review.cjs).
func TestFixtureArtifactTableReview(t *testing.T) {
	runFixture(t, "artifact-table-review.cjs", true, false)
}

// Improve chat readability, version text edits, and track email replies (artifact-text-editor.cjs).
func TestFixtureArtifactTextEditor(t *testing.T) {
	runFixture(t, "artifact-text-editor.cjs", true, false)
}

// Retain calendar source identity and expose partial reads (calendar-partial-read.cjs).
func TestFixtureCalendarPartialRead(t *testing.T) {
	runFixture(t, "calendar-partial-read.cjs", true, false)
}

// Improve chat readability, version text edits, and track email replies (chat-conversation-layout.cjs).
func TestFixtureChatConversationLayout(t *testing.T) {
	runFixture(t, "chat-conversation-layout.cjs", true, false)
}

// Keep stale delivery notices out of new chats and allow dismissal (chat-delivery-notices.cjs).
func TestFixtureChatDeliveryNotices(t *testing.T) {
	runFixture(t, "chat-delivery-notices.cjs", true, false)
}

// Normalize chat dialogs and audit responsive spacing (chat-dialog-spacing.cjs).
func TestFixtureChatDialogSpacing(t *testing.T) {
	runFixture(t, "chat-dialog-spacing.cjs", true, false)
}

// Review complete conflicting drafts and clear stale task context (chat-draft-conflicts.cjs).
func TestFixtureChatDraftConflicts(t *testing.T) {
	runFixture(t, "chat-draft-conflicts.cjs", true, false)
}

// Open chat file links in workspace tabs and make feed panels responsive (chat-file-links.cjs).
func TestFixtureChatFileLinks(t *testing.T) { runFixture(t, "chat-file-links.cjs", true, false) }

// Add scroll-aware latest navigation and record chat UX acceptance (chat-latest-browser.cjs).
func TestFixtureChatLatestBrowser(t *testing.T) {
	runFixture(t, "chat-latest-browser.cjs", true, false)
}

// Add chat project creation and explicit coding model choices (chat-model-picker.cjs).
func TestFixtureChatModelPicker(t *testing.T) { runFixture(t, "chat-model-picker.cjs", true, false) }

// Interrupt native chat runners with durable targeted stop receipts (chat-native-interrupt.cjs).
func TestFixtureChatNativeInterrupt(t *testing.T) {
	runFixture(t, "chat-native-interrupt.cjs", true, true)
}

// Select reviewed knowledge note versions as private chat context (chat-note-context.cjs).
func TestFixtureChatNoteContext(t *testing.T) { runFixture(t, "chat-note-context.cjs", true, false) }

// Add persistent accessible chat pane resizing (chat-pane-resize.cjs).
func TestFixtureChatPaneResize(t *testing.T) { runFixture(t, "chat-pane-resize.cjs", true, false) }

// Serialize conversation polls and discard superseded responses (chat-poll-order.cjs).
func TestFixtureChatPollOrder(t *testing.T) { runFixture(t, "chat-poll-order.cjs", false, true) }

// Keep background conversation prefetch from replacing newer state (chat-prefetch-order.cjs).
func TestFixtureChatPrefetchOrder(t *testing.T) {
	runFixture(t, "chat-prefetch-order.cjs", false, true)
}

// Add durable project context and recoverable instruction editing (chat-project-context.cjs).
func TestFixtureChatProjectContext(t *testing.T) {
	runFixture(t, "chat-project-context.cjs", true, false)
}

// Include sidebar folder projects when starting a chat (chat-project-picker.cjs).
func TestFixtureChatProjectPicker(t *testing.T) {
	runFixture(t, "chat-project-picker.cjs", true, false)
}

// Recover native question answers across reloads and uncertain delivery (chat-question-recovery.cjs).
func TestFixtureChatQuestionRecovery(t *testing.T) {
	runFixture(t, "chat-question-recovery.cjs", true, false)
}

// Clarify chat actions and agent selection, add per-file change review (chat-review-paths.cjs).
func TestFixtureChatReviewPaths(t *testing.T) { runFixture(t, "chat-review-paths.cjs", true, false) }

// Fence stale explicit conversation loads across refreshes and navigation (chat-session-load-order.cjs).
func TestFixtureChatSessionLoadOrder(t *testing.T) {
	runFixture(t, "chat-session-load-order.cjs", false, true)
}

// Add explicit sharing review and recoverable confirmation in chat (chat-share-review.cjs).
func TestFixtureChatShareReview(t *testing.T) { runFixture(t, "chat-share-review.cjs", true, false) }

// Add coding agents to existing shared conversations (chat-shared-create.cjs).
func TestFixtureChatSharedCreate(t *testing.T) { runFixture(t, "chat-shared-create.cjs", true, false) }

// Add workbench keyboard navigation and attention shortcut (chat-shortcuts.cjs).
func TestFixtureChatShortcuts(t *testing.T) { runFixture(t, "chat-shortcuts.cjs", false, false) }

// Preserve coding chat context through task details and terminal navigation (chat-terminal-context.cjs).
func TestFixtureChatTerminalContext(t *testing.T) {
	runFixture(t, "chat-terminal-context.cjs", false, false)
}

// Reload full terminal history after an unavailable interval (chat-terminal-history-recovery.cjs).
func TestFixtureChatTerminalHistoryRecovery(t *testing.T) {
	runFixture(t, "chat-terminal-history-recovery.cjs", false, true)
}

// Make terminal initial transcript failures bounded and recoverable (chat-terminal-load-failure.cjs).
func TestFixtureChatTerminalLoadFailure(t *testing.T) {
	runFixture(t, "chat-terminal-load-failure.cjs", false, true)
}

// Prevent stale terminal loads from replacing newer conversation state (chat-terminal-load-order.cjs).
func TestFixtureChatTerminalLoadOrder(t *testing.T) {
	runFixture(t, "chat-terminal-load-order.cjs", false, true)
}

// Show terminal transcript read failures without discarding retained work (chat-terminal-read-health.cjs).
func TestFixtureChatTerminalReadHealth(t *testing.T) {
	runFixture(t, "chat-terminal-read-health.cjs", true, true)
}

// Bound terminal tail reads and preserve evidence on failed responses (chat-terminal-tail-failure.cjs).
func TestFixtureChatTerminalTailFailure(t *testing.T) {
	runFixture(t, "chat-terminal-tail-failure.cjs", false, true)
}

// Add tabbed chat workspace with contextual side conversations (chat-workspace-tabs.cjs).
func TestFixtureChatWorkspaceTabs(t *testing.T) { runFixture(t, "chat-workspace-tabs.cjs", true, true) }

// Expose owner recovery for uncertain approved email (email-reconcile-ui.cjs).
func TestFixtureEmailReconcileUi(t *testing.T) { runFixture(t, "email-reconcile-ui.cjs", true, false) }

// Surface tracked replies as durable private Feed notices (email-reply-notices.cjs).
func TestFixtureEmailReplyNotices(t *testing.T) {
	runFixture(t, "email-reply-notices.cjs", true, false)
}

// Improve chat readability, version text edits, and track email replies (email-watch-ui.cjs).
func TestFixtureEmailWatchUi(t *testing.T) { runFixture(t, "email-watch-ui.cjs", true, false) }

// Open chat file links in workspace tabs and make feed panels responsive (feed-panels-responsive.cjs).
func TestFixtureFeedPanelsResponsive(t *testing.T) {
	runFixture(t, "feed-panels-responsive.cjs", true, false)
}

// Add Excalibur retirement authority and observability (phase1-agents.cjs).
func TestFixturePhase1Agents(t *testing.T) { runFixture(t, "phase1-agents.cjs", false, false) }

// Retire three Excalibur rituals across manual launch surfaces (phase2-retirement.cjs).
func TestFixturePhase2Retirement(t *testing.T) { runFixture(t, "phase2-retirement.cjs", true, false) }

// Enhance recruiting candidates with cited DeepSeek briefs and focused review (recruiting-focused-review-browser.cjs).
func TestFixtureRecruitingFocusedReviewBrowser(t *testing.T) {
	runFixture(t, "recruiting-focused-review-browser.cjs", true, false)
}

// Distinguish incomplete people lookups and preserve model diagnostics (recruiting-lookup-message.cjs).
func TestFixtureRecruitingLookupMessage(t *testing.T) {
	runFixture(t, "recruiting-lookup-message.cjs", false, false)
}

// Route recruiting draft review through canonical email approvals (recruiting-outreach-approval.cjs).
func TestFixtureRecruitingOutreachApproval(t *testing.T) {
	runFixture(t, "recruiting-outreach-approval.cjs", true, false)
}

// Continue shared terminal conversations across Manifest and team portals (shared-chat.cjs).
func TestFixtureSharedChat(t *testing.T) { runFixture(t, "shared-chat.cjs", true, false) }

// Fix terminal recovery and unsupported chat shortcuts (terminal-recovery-browser.cjs).
func TestFixtureTerminalRecoveryBrowser(t *testing.T) {
	runFixture(t, "terminal-recovery-browser.cjs", true, true)
}

// Dock chat terminals and separate conversation lifecycle actions (chat-lifecycle.cjs).
func TestFixtureChatLifecycle(t *testing.T) { runFixture(t, "chat-lifecycle.cjs", true, false) }

// Simplify chat navigation and add focused in-chat terminal view (chat-simple-shell.cjs).
func TestFixtureChatSimpleShell(t *testing.T) { runFixture(t, "chat-simple-shell.cjs", true, false) }

// Add editable pending coding messages with explicit steering (chat-steering.cjs).
func TestFixtureChatSteering(t *testing.T) { runFixture(t, "chat-steering.cjs", true, false) }

// Render conversation markdown like a flagship harness: numbered and nested
// lists, hanging bullets, language-labelled code with exact copy (chat-codex-transcript.cjs).
func TestFixtureChatCodexTranscript(t *testing.T) {
	runFixture(t, "chat-codex-transcript.cjs", true, false)
}

// Tile whole conversations like a tiling window manager: dwindle splits,
// keys from page and tile, no frame reloads, saved arrangement (chat-tiles.cjs).
func TestFixtureChatTiles(t *testing.T) { runFixture(t, "chat-tiles.cjs", true, false) }

// One model · effort picker on every agent, /model and /effort as surface
// commands, the exact recipient on send (chat-composer-models.cjs).
func TestFixtureChatComposerModels(t *testing.T) {
	runFixture(t, "chat-composer-models.cjs", true, false)
}

// The live status line: working time, step, Stop/Esc, context meter, and
// "Worked for" on finished replies (chat-status-line.cjs).
func TestFixtureChatStatusLine(t *testing.T) { runFixture(t, "chat-status-line.cjs", true, false) }

// Phone chat: one chip row, a titled head, an honest immediate Codex echo,
// busy send ink, legible Activity and context, runs kept apart (chat-mobile-uiux.cjs).
func TestFixtureChatMobileUIUX(t *testing.T) { runFixture(t, "chat-mobile-uiux.cjs", true, false) }

// Phone chat pass 3: a queued send's state, Edit and Cancel under its own
// bubble, a grouped reply meta row, one transcript rhythm, a two-row composer
// after a send, one primary per region (chat-mobile-pass3.cjs).
func TestFixtureChatMobilePass3(t *testing.T) { runFixture(t, "chat-mobile-pass3.cjs", true, false) }

// The virtual browser tool captures and compares two pages end to end
// (tools/ui-compare, ui-compare.cjs).
func TestFixtureUICompare(t *testing.T) { runFixture(t, "ui-compare.cjs", true, false) }

// Chat performance budgets: cold/warm open, switch, idle requests, heap, and
// tiles with a hidden workspace, measured by tools/perf/chat-perf.cjs over
// the app's own cache headers (chat-perf-budget.cjs).
func TestFixtureChatPerfBudget(t *testing.T) {
	runFixture(t, "chat-perf-budget.cjs", true, false)
}

// Tiles and polling: a hidden tile is silent, an unfocused one polls at the
// manager's cadence, and showing or focusing a tile reads it at once
// (chat-tiles-panes.cjs).
func TestFixtureChatTilesPanes(t *testing.T) {
	runFixture(t, "chat-tiles-panes.cjs", true, false)
}

// One composer-first new chat: chips for agent, model, project and folder,
// the draft carried across an agent switch, starters, Ctrl+Alt+N, phone
// (chat-new-flow.cjs).
func TestFixtureChatNewFlow(t *testing.T) { runFixture(t, "chat-new-flow.cjs", true, false) }

// Steer vs queue on a native agent: cannot-steer said in words, Tab queues,
// ↑ pulls a queued message back (cancelled first), Tab moves focus when idle
// (chat-steer-queue.cjs). The coding-agent side is in chat-steering.cjs.
func TestFixtureChatSteerQueue(t *testing.T) { runFixture(t, "chat-steer-queue.cjs", true, false) }

// Attention across tiles: needs you / error / done, Alt+N across
// workspaces, notifications only when allowed (chat-tiles-attention.cjs).
func TestFixtureChatTilesAttention(t *testing.T) {
	runFixture(t, "chat-tiles-attention.cjs", true, false)
}

// The live +N −M chip on a coding session's header (chat-changes-chip.cjs).
func TestFixtureChatChangesChip(t *testing.T) { runFixture(t, "chat-changes-chip.cjs", true, false) }

// The single-chat UI pass: clean load, legible meta and menu, 44px phone
// targets, a whole run-state hint, a shell that fills its column
// (chat-single-view-pass.cjs).
func TestFixtureChatSingleViewPass(t *testing.T) {
	runFixture(t, "chat-single-view-pass.cjs", true, false)
}

// The second single-chat pass: one phone header, body-size sent text, a
// chip-sized model target, the queued state stated once, the one-recipient
// chip, and no visible phone target under 44px (chat-single-view-pass2.cjs).
func TestFixtureChatSingleViewPass2(t *testing.T) {
	runFixture(t, "chat-single-view-pass2.cjs", true, false)
}
