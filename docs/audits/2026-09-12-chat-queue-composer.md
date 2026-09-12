# Native follow-up queue and steady mobile composer

The pending-message row required Steer even after the prior run finished. The
existing delivery outbox now drains automatically at a connected, running,
idle/done prompt. Steer remains an explicit mid-run dispatch. The event hub checks
promptly while connected; the existing 60-second AgentLoopTicker provides delivery
with no browser, including after restart. No additional scheduler or message store.

The server compares and claims the exact saved message before calling the ordinary
input handler, which repeats readiness under the input mutex and retains attachment,
shared-access and runtime identity checks. One queued message per recipient per sweep
preserves order; an uncertain earlier send holds later messages. Edit, remove and
Steer use the same CAS boundary. Delivery receipts prevent replay. A crash after a
claim but before acknowledgement leaves an inspectable uncertainty rather than an
automatic retry. The browser reconciles authoritative queue changes during polling
and wake without rebuilding unchanged controls.

Mobile typing repeatedly reset the live textarea height to auto and then regrew it.
Viewport events also forced window.scrollTo(0, 0), competing with caret reveal.
The shared offscreen measurement helper keeps the focused field in place and caches
unchanged measurements. Viewport events are coalesced and account for offsetTop
without forced scrolling. Long text retains the existing height cap and internal
scrolling; phone wrap and desktop layouts stay consistent with the conventions.

Browser grounding: [MDN VisualViewport](https://developer.mozilla.org/en-US/docs/Web/API/VisualViewport)
documents the separate visual viewport, keyboard resize and viewport offsets.
[MDN field-sizing](https://developer.mozilla.org/en-US/docs/Web/CSS/Reference/Properties/field-sizing)
describes native content sizing; an offscreen measurement retains compatibility
with older installed mobile browsers and the composer's existing wrap rules.

Validation includes persisted queue delivery after server recreation, FIFO,
working/blocked/unknown holds, cancellation, stale claims, no uncertain replay,
server rejection of a stale automatic send, explicit Steer, mobile capped typing
without live style writes or page scroll corrections, simulated keyboard geometry,
and existing phone/desktop chat controls. Browser automation does not emulate the
physical iOS keyboard; hardware Safari remains the final device-level check.
