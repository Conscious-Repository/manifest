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
