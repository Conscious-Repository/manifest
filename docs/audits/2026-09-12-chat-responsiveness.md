# Chat responsiveness

Research informed this implementation:

- [web.dev: Optimize INP](https://web.dev/articles/optimize-inp): responsiveness
  includes the work before the next visual update. Reduce unnecessary rendering
  and provide prompt feedback to an interaction.
- [web.dev: Layout thrashing](https://web.dev/articles/avoid-large-complex-layouts-and-layout-thrashing):
  avoid repeated synchronous layout and large document updates. The existing
  offscreen textarea measurement remains; this pass reduces transcript churn.
- [W3C: Sequential updates](https://www.w3.org/WAI/WCAG21/Techniques/aria/ARIA23):
  chat announcements should concern new information. Replacing the entire history
  on every update is a poor basis for focus, selection or assistive technology.
  This pass preserves existing nodes rather than adding a live region that would
  announce each streamed chunk.

Application of Manifest values: quiet existing surfaces and typography; no new
animation or framework; stable focus and reading position; honest pending state;
explicit Steer and unchanged approval/delivery boundaries.

Native transcript updates previously cleared the entire history and reparsed
all Markdown. Shared keyed reconciliation now retains unchanged turns and updates
only changed/new rows. Approval and operation controls remain mounted while their
inputs stay unchanged. Planning timelines keep their existing rendering path.

The older streaming path artificially revealed received text at about 85 chars/sec.
It now displays all received text on a short batched paint and skips unchanged
frames. Receipt reconciliation runs independently of transcript polling, with one
in-flight recovery per conversation. Send shows immediate pending feedback and
retains its accessible label through composer refresh.

Validation: a 300-message browser fixture verifies one Markdown render for a new
message, retention of historical DOM, nested disclosure state and keyboard focus;
phone/desktop bounds and both themes; stream tests verify full received text,
no idle re-render, delayed-frame catch-up and final completion; a deliberately slow
receipt test proves transcript polling continues without duplicate recovery calls.
These are controlled regression checks, not a production INP or physical iPhone
keyboard measurement.
