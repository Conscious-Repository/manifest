# Chat: state and responsiveness pass (2026-09-21)

Owner goal: the chat surface should feel like the state-of-the-art harnesses
(Codex, ChatGPT) — instant to open, instant to switch, never losing its way
on a phone — while staying manifest-quiet (`docs/ui-conventions.md`).

## What was measured

Opening Chats or moving between threads fanned out to ~13 requests before the
rail could paint (roster → one session list per agent + spirits + terminal
registry + four inbox state slots + review counts + task conversations), then
the thread itself, then its draft and reading position. Each is a round trip
from a phone over the tailnet; the chain was three to four deep. On a phone
that is 400 ms – 1 s per switch, with the previous thread on screen meanwhile
(fixed 2026-09-12 by the stage cache) and the list repainted from scratch.

Two phone defects rode on top: the composer fell under the iOS keyboard on
focus and came back only after a refit (iOS pans the visual viewport; the
shell sat in the layout viewport), and "‹ Chats" vanished whenever a thread
opened with no head yet or failed to load (the back control lived only on a
mounted head).

## What the state of the art does

- One snapshot for the list, kept warm in memory and on disk, rendered
  before it is revalidated (stale-while-revalidate).
- The stage turns over synchronously; the network confirms.
- Threads are prefetched on intent (pointer down / hover), so the tap is
  a paint, not a request.
- The app follows the visual viewport while a keyboard is up, so the
  composer never leaves the visible window.
- The way back is chrome, not content: it exists whether or not the thread
  loaded.

## What shipped

1. `GET /api/chat/inbox` (server/chat_inbox.go): every rail list in one
   request, composed in-process from the existing handlers so each part keeps
   its exact shape and rules. The client applies each part with the appliers
   it already had; the old fan-out stays as the fallback.
2. Stale-while-revalidate: the inbox paints from memory (and, on a fresh
   page, from a localStorage snapshot) and revalidates after the paint; a
   thread switch no longer waits on the lists at all.
3. Prefetch on intent: pointer-down on a rail row fetches that thread into the
   stage cache (agent and coding threads), so the tap paints from cache.
4. The phone shell follows the visual viewport while a keyboard is up
   (98-mobile.js `follow`, `.app-shell.mf-keyboard`), and the viewport meta
   asks Chrome to resize content for its keyboard.
5. "‹ Chats" is mounted on a bare head whenever a thread route is open and no
   head is available (loading, failed, or empty), so the way back never
   depends on the thread.

## Not done, on purpose

Keyed reconciliation of rail rows (the list still repaints as a whole on a
change); a service worker; server push for the inbox. Each is a further step
with its own risk; none was needed to meet the goal above.
