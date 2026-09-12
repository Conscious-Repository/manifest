# Responsiveness audit beyond Chat

The chat fixes revealed three reusable lessons: keep interactive DOM mounted,
separate network completion from current user intent, and eliminate avoidable work
on typing and polling paths. This pass audited the analogous paths in Tasks,
Feed/Consume, Writing, Contacts, Reading, Files, Agents and Terminal. Source review
also checked Day's render paths and the existing task-panel poll safeguards.

| Surface | Confirmed issue | Implemented change |
|---|---|---|
| Tasks | Toolbar replaced the search input on every keystroke, then forced focus/caret restoration. | Mount the toolbar once; update shared filter controls and counts in place. |
| Feed | Refresh rebuilt the filter buttons. | Stable filter nodes and immediate selection feedback. |
| Consume | Refresh detached even the reused search input; narrow toolbar overflowed; failures looked empty. | Keep header/search mounted, invalidate stale requests during debounce, wrap phone controls and show failed loads with Retry. |
| Writing | Comments reset the focused textarea to auto height on every character. | Shared offscreen measurement, write height only when needed; phone input uses the body-size token. |
| Contacts | Old page/nearby/search replies could overwrite newer views; search results remained selectable during debounce. | Latest-request checks for success/error; immediate invalidation; failed duplicate checks do not offer a false empty result. |
| Reading | Catalogue sequencing began after debounce, leaving stale matches briefly actionable. | Invalidate and clear old choices immediately on input. |
| Files | Old directory or host-home requests could win after navigation. | Capture request identity, reject stale success/error, and make old file controls inert while resolving the new directory. |
| Agents | A slow run poll could overlap another or finish after leaving the view. | One in flight, skip hidden pages, recheck scope before publishing the result. |
| Terminal | Inventory refresh recreated session rows, armed end controls and selector options. | Reconcile unchanged rows/options with the shared keyed helper. |

The existing task panel already avoids overlapping polls and protects active text
editing. Terminal inventory already coalesces concurrent requests. These remain.
On-demand Day/shelf rendering does not require a blanket conversion to keyed DOM.
No new framework, scheduler, input transport or external-action authority is added.
A duplicate chat flex-wrap rule from the intervening attachment change was removed;
the original identical declaration continues to apply.

Research grounding is shared with the [chat responsiveness audit](2026-09-12-chat-responsiveness.md):
[web.dev INP guidance](https://web.dev/articles/optimize-inp) and
[layout guidance](https://web.dev/articles/avoid-large-complex-layouts-and-layout-thrashing).
The implementation follows Manifest's tokens, shared components, explicit actions,
error-versus-empty distinction, stable focus and centralized mobile rules.

Validation: browser tests at 320/390/768/1280 in both themes cover mounted Tasks,
Feed and Consume controls, Writing at its height cap, toolbar bounds, armed Terminal
controls, and stale catalogue/contact results inside the debounce window. Request
unit tests deliberately reverse Files/Contacts responses, leave Nearby mid-request,
and overlap/background Agents polls. Existing Feed stale-paint guards were updated
for the retained-header structure. Full server tests and an isolated release build
are the release checks. These are controlled interaction tests, not measured field
INP or a physical iPhone keyboard test.
