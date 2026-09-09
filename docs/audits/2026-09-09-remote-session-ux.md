# Remote Claude Code / Codex sessions

## Interaction principles

- Keep the native PTY available for full-screen applications, completion, permission menus, and raw keyboard interaction. Chat is a readable transcript plus an input bridge, not a claim to replace every CLI feature.
- Provide Enter, Escape, Tab, Shift+Tab, arrow, and interrupt keys on touch screens. The keys send literal terminal input; they do not invent approval decisions.
- Keep session identity visible and support independent direct links for separate desktop tabs. Background sessions keep running on the existing server runtime.
- Preserve drafts per conversation while switching within Chat. Do not move attachment references or failed messages into a different session.
- Separate headers from scrollback. Preserve live-screen scroll position when reading older lines, with an explicit Latest action for the transcript.
- On mobile, fit the PTY to the visual viewport, collapse launcher navigation after attaching, and keep a Keyboard button available without forcing the keyboard open on entry.

## Primary references

- Claude Code interactive mode: https://code.claude.com/docs/en/interactive-mode
- Claude Code remote control: https://code.claude.com/docs/en/remote-control
- Codex CLI: https://developers.openai.com/codex/cli/features
- xterm.js Terminal API: https://xtermjs.org/docs/api/terminal/classes/terminal/

## Limits

Drafts are kept in the current browser tab's memory, not synced across devices or persisted across reloads. Concurrent sessions can be switched in Manifest or opened in separate tabs; this pass does not add a split-pane terminal grid. A mobile viewport check does not substitute for testing a physical iOS/Android keyboard and network handoff. Native remote-control services are reference patterns, not newly enabled integrations.
