# Spec: Kanban Board View for the Manifest Todos Surface

**Date**: 2026-08-11
**Repo**: `~/src/manifest` (Go, single binary, go:embed web assets)
**Author of intent**: Benjamin (operator). **Ein**: Hermes (assistant), writing this spec for **you** (Claude Code) to execute.

> **HOW TO EXECUTE THIS SPEC**: This is a buildtall-style implementation spec. Follow it phase by phase. Every phase must compile, pass `go test ./...`, and pass `go vet ./...` before you proceed to the next. Do **not** modify the vault. Only touch files under `~/src/manifest/`. Rebuild the binary with `go build -o manifest .` and restart `manifest.service` **only** if the operator asks you to (the operator owns deployment).

---

## Goal

Add a **Kanban (column) board view** to the existing Manifest **todos surface**, giving the operator a visual, drag-and-drop task board in the same dashboard that already shows the ranked todos list. The board must be **stigmergic**: it is a pure *projection* over the existing `todos` substrate — the same fixpoint files (`to do.md`, property `## todos`, aion backlog) that are already the source of truth. The board **must not introduce a parallel data store**; it renders and writes only through the existing `/api/todos` contract.

Current state (verified):
- Backend: `server/todos.go` + `server/todos_unified.go` expose `GET /api/todos` returning `{ domains, areas, split, rows[], counts{}, ... }`.
- Frontend: `server/web/js/90-todos.js` renders the todos surface as a **ranked list** with sub-tabs (FOCUS / AION / REAL ESTATE / PERSONAL) + a "Decisions waiting on you" lane. This is a list, **not** a kanban.
- Existing write endpoints: `POST /api/todos/item` (add), `/check`, `/update`, `/rank`, `/bucket`, `/issue`, `/issue/resolve`, `/drop`.

This spec **adds a board view alongside** the existing list — it does not replace it.

---

## Desired End State

The operator opens the todos surface and can toggle between **List** (existing `90-todos.js`) and **Board** (new). The Board renders every open task row as a **card**, grouped into **columns** derived from the existing data model — by *state* (Todo.State() = open | waiting | done) and/or by *bucket/domain/property*. Cards can be **dragged between columns**, and that drag persists through the existing `/api/todos/*` endpoints (e.g. `/rank`, `/update` with `domain`/`bucket`/`owner`/`waiting`), writing the marker back into the owning file. The board **reads** `GET /api/todos` and needs **no new backend data model**.

This follows buildtall's **personal-ontology** composition rule: the surface (board) is a projection over the dataspace (fixpoint files); it never becomes a second source of truth.

---

## What We're NOT Doing (anti-scope)

- **Not** building a standalone app — this lives inside the existing Manifest surface.
- **Not** adding a new datastore, CRDT, or sync engine. The board is **read-only projection + existing write endpoints**.
- **Not** touching the vault, property files, or aion backlog formats — all writes go through existing composite-id routes (`prop:`, `aion:`, or default `to do.md`).
- **Not** changing the existing List view or its interaction model.
- **Not** adding realtime/WebSocket sync — a refresh / refetch on action is sufficient (matches the existing `loadTodos()` pattern).
- **Not** a full "project management" system (no estimated story points, no burndown, no assignee avatars beyond the existing `[owner::]` field).

---

## Implementation Phases

### Phase 1: Backend — expose board-friendly shape

**Goal**: Make sure `GET /api/todos` carries enough to render columns without the frontend re-deriving private state.

**Context**: `unifiedView()` already returns `rows[]` (each with `id, text, owner, rank, container{kind,slug,name}, state, source, ...` from `unifiedRow`). The frontend currently buckets rows itself into tabs via `tabOf(r)`. For a board we need, per row: **state** (open/waiting/done) and a **group key** (domain name, or bucket slug, or property slug, or "aion").

**Files to Modify**:
- `server/todos_unified.go`
  - **Change**: Verify `unifiedRow` already exposes `state` and `container`. If `state` (open/waiting/done) is not on the JSON row, add it (it exists as `Todo.State()` and `unifiedRow` likely carries it — confirm and expose as `state` if missing).
  - **Change**: Add a `group` field to `unifiedRow` (or reuse `container.name`/`slug`) so the frontend can column by domain/bucket/property/aion cleanly. Keep it additive.

**Testing**:
- `server/todos_unified_test.go`: existing tests must still pass. Add a test asserting `unifiedRow` JSON includes `state` (and `group` if added) for a fixture doc.

**Verification**:
- `go build ./...`
- `go vet ./...`
- `go test ./...`

**Expected Outcome**: `GET /api/todos` rows carry explicit `state` + grouping; existing list view still renders identically.

---

### Phase 2: Frontend — Board view (new JS)

**Goal**: Render the board and let operator drag cards between columns, persisting via existing endpoints.

**Files to Create**:
- `server/web/js/91-board.js` — the Board renderer + drag/drop handler. **Reuse** existing helpers from `90-todos.js` where possible (`el()`, `postJSONOk()`, `showToast()`, `todosApi()`); the two view files can share a small namespace guard.

**Files to Modify**:
- `server/web/index.html`
  - **Change**: Add a List/Board toggle in the todos surface header (e.g. `filter-chip` buttons "LIST" / "BOARD", mirroring the existing tab-chip markup).
  - **Change**: Add a container div `<div id="todosBoard"></div>` shown when Board is active, hidden otherwise; keep the existing `#todosRows` for List.
  - **Change**: Include `js/91-board.js` script tag after `90-todos.js`.
- `server/web/js/90-todos.js`
  - **Change**: On tab/toggle, if Board active, call `renderBoard(todosCache)` instead of `renderTodos()`; if List, the existing path. Keep List behavior byte-for-byte unchanged.
  - **Change**: `loadTodos()` already refetches on action; ensure it re-renders the active view.

**Board behavior**:
- **Columns** = configurable derive: default columns are **Open / Waiting / Done**, with an optional grouping toggle into **domain/bucket/property/aion** columns. Start with the simple Open/Waiting/Done default; grouping-by-container can be a second toggle if time permits (see Phase 3).
- **Cards**: one per `rows[]` item; show text, owner chip (if set), container tag (domain/property/aion). Done cards shown struck-through or in a muted Done column.
- **Drag & drop**:
  - Dropping an Open card into **Waiting** → `POST /api/todos/update` with `waiting` set (requires a `who`; use default or prompt lightly).
  - Dropping a Waiting card into **Open** → `POST /api/todos/update` with `waiting: ""`.
  - Dropping any card into **Done** → `POST /api/todos/check` with `checked:true`.
  - Un-done (drag back to Open) → `POST /api/todos/check` with `checked:false`.
  - Drag to reorder **within** a column → `POST /api/todos/rank` (already exists).
- **Interaction**: HTML5 drag-and-drop (no new dependency) or lightweight pointer events — match whatever `90-properties-board.js` already uses for its board, so the codebase has one drag idiom (check `81-properties-board.js` first and reuse its approach).

**Testing**:
- Manual (in browser via `hermes dashboard` / served surface): toggle List↔Board, add a card, drag across Open/Waiting/Done, reload — state persists and both List and Board agree.
- No unit-test framework exists for the vanilla JS; keep logic minimal and mirror `90-todos.js`'s proven patterns.

**Verification**:
- `go build ./...` (embed picks up new assets)
- `go vet ./...`, `go test ./...`
- Manual browser check (operator).

**Expected Outcome**: Board view renders from existing data; drag actions persist to the owning fixpoint file; List view unaffected.

---

### Phase 3 (optional / stretch): Grouped board by container

**Goal**: Add a grouping toggle so the board columns are **domains/buckets/properties** instead of Open/Waiting/Done (advanced).

**Files to Modify**: `91-board.js`, `90-todos.js` (toggle).
- Group cards by `container.name`/`slug`; cross-column state shown as a per-card badge instead of a column.
- Drag → move card to another group's column → `update` with the new domain/bucket (existing endpoints already support domain move and bucket placement).

**Verification**: same as Phase 2.

---

## Final Verification (Technical Acceptance Criteria)

- [ ] `go build ./...` succeeds
- [ ] `go vet ./...` clean
- [ ] `go test ./...` passes (no existing test broken; new state/group test present)
- [ ] Board view toggles on/off; List view unchanged
- [ ] Cards render from `GET /api/todos` rows
- [ ] Drag Open↔Waiting↔Done persists; reload consistent
- [ ] Drag reorder persists via `rank`
- [ ] Done cards appear in Done column, struck through
- [ ] No new datastore; all writes route through existing `/api/todos/*`
- [ ] Vault (`/private/consciousrepo`) untouched

---

## Rollback Plan

1. Each phase is an atomic commit. If Board breaks the surfaces, revert the last commit (`git revert <sha>`) — the List view and backend are untouched until Phase 2, so reverting Phase 2 restores prior behavior.
2. Board is additive JS + a toggle; removing `91-board.js` and the toggle restores exactly the prior UI.
3. No migration: there is no schema change and no data transformation, so rollback is trivially safe (`to do.md` format is unchanged).

---

## Notes

- **Reuse, don't reinvent**: check `81-properties-board.js` (the property board) and `90-todos.js` for existing board/drag idioms; the codebase already has one. Align `91-board.js` to it.
- **No comments rule / code style**: match the existing Go + JS style in the repo (concise, idiomatic). Do not add explanatory comments that restate the code.
- **go:embed**: new `.js`/`.html` files under `server/web/` are embedded at build time — a rebuild is required to see changes; the operator owns deployment/restart.
- **Fixpoint invariant**: `to do.md` (and property/aion files) are byte-stable round-trip sources of truth. The board writes only marker fields via existing endpoints (check, waiting, rank, domain/bucket move). Never rewrite a file wholesale or reorder lines outside the existing serializer's contract.
- **Unspecified (ask the operator)**: whether to replace the List view's default tab with Board or keep List as default. Spec defaults to **keep List as default, Board behind a toggle** — confirm if you want otherwise.
