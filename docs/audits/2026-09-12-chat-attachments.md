# Chat attachments

Implemented multiple-file selection, clipboard images, and file drop through the quiet composer +. Images have thumbnails; documents have filename/type/size cards. The existing attachment workspace handles read-only image/PDF/text preview and download. The Files tab includes uploaded chat context.

Private Codex/Claude, Hermes, and spirit sends retain opaque attachment IDs and resolve them to immutable local files. Native request receipts preserve the visible user message while the agent receives exact local paths with file contents explicitly labeled reference material. Remote/legacy terminal backends reject attachment sends rather than silently dropping context. Existing team portal uploads retain their shared artifact path.

Storage uses independent private directories, not the shared content-addressed artifact pool. Limits: eight files per message, 20 MB per file, 100 files/200 MB per chat. Draft removal deletes unsent uploads; chat deletion deletes private uploads. Restore does not resurrect bytes. Shared/task artifacts, legacy uploads and provider-managed transcripts/copies are not deleted. Abandoned landing drafts expire after seven days on startup or next upload.

Validation: server lifecycle/isolation/format/size/preview/removal tests, actual native delivery fixture with multiple attachments and idempotent retry, browser upload/navigation race fixture, steering regression, and intercepted live-page checks on desktop and phone in light and Jarvis themes. No test messages are sent to real agents.
