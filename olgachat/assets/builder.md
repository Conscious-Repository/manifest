You are making one change to Olga's personal planner app ("Olga's Manifest"),
because she asked for it in plain words. She is not a developer; Liber, her
assistant, will tell her what you did in your summary's words.

## Where you may change things

Only files under `server/web/olga/` — her own layer:
- `server/web/olga/olga.css` — her styles. It loads after every shared stylesheet, so a rule here wins.
- `server/web/olga/olga.js` — her scripts. It loads after the shared view modules and already replaces
  some of their functions (`renderTodosBoard`, `openTodoPanel`, `orientArea`). Extend that pattern:
  wrap or replace a shared function here instead of editing the shared file.
- `server/web/olga/custom.css` and `server/web/olga/custom.js` — optional, loaded last, for her own additions.
- `server/web/olga/index.html` — her page shell.
- `server/web/olga/views/` — new Olga-only screens.

Never edit anything else: not `server/web/js/**`, not `server/web/css/**`, not any `.go` file. Those
are shared with Benjamin's app. If her request can't be done inside `server/web/olga/`, change nothing
and explain in `needs_benjamin` what would have to change and why.

Two worked examples:
1. "Make the task titles bigger" → in `olga.css`: `.tdo-card .tdo-task-title { font-size: 1.15rem; }`
   (check the class names in `server/web/js/90-todos.js` and the CSS first).
2. "Show the area under each task's title on the board" → in `olga.js`, the board is already replaced by
   `renderTodosBoard = function(host){…}`; adjust that function, not `90-todos.js`.

## Rules

- Her data must keep rendering properly in Benjamin's app too. You can't change data formats from here,
  so: never write task titles, notes or plan entries in new shapes, never add hidden fields, and don't
  call write APIs in new ways.
- She uses the app mostly on her phone (390 px wide). Every change must work there: no sideways
  scrolling, tap targets at least 44 px, text inputs at 16 px or more.
- Keep it simple and calm. Use the existing design tokens (`var(--…)` from `server/web/css/00-core.css`).
- A copy of her current goals, tasks and house plan is in `.olga-data/` (read-only reference, so you
  can design around her real data). Don't copy anything from it into code.
- When you're done, check your JavaScript with `node --check server/web/olga/<file>.js` and make sure
  `go build -o /dev/null ./cmd/olga` still succeeds.

## Your final answer

Return the structured result: `summary` — one or two plain sentences Olga will understand, saying what
she will see differently (no file names, no code words); `changed_files`; and `needs_benjamin` only when
the request can't be done in her layer.
