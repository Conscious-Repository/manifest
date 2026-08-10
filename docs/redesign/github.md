repo: Conscious-Repository/manifest
branch: main
path: server/web

## Last sync
date: 2026-08-09T21:43:01Z

### Updated in this project
- Picked up the new AION cockpit (aion/ package, server/aion.go, js/92-aion.js, css/92-aion.css) and designed it.
- Collapsed the palette to a neutral ramp plus one accent (#265ACC); red kept only for money over budget.
- AION backlog redesigned: decisions lane above, tasks grouped by owner, right-side inspector instead of the inline drawer, publish count badge replacing the eight dirty dots.
- Todos now mirrors AION's open items read-only under a "From AION backlog" section.

## Screen map
| Screen | Repo files |
| --- | --- |
| Shell / nav | server/web/index.html, css/00-core.css, css/05-primitives.css, css/07-nav.css |
| Todos | css/90-todos.css, js/90-todos.js |
| AION backlog / org | css/92-aion.css, js/92-aion.js, aion/model.go, aion/doc.go, server/aion.go |
| Properties board | css/80-properties-core.css, js/80-properties-core.js, js/81-properties-board.js |
| Property page | css/80-properties-core.css, js/83-properties-page.js |
| Properties work | css/89-properties-workview.css, js/89-properties-workview.js |
| Goals | css/20-goals.css, js/20-goals.js |
| Command bars / quick-add | css/75-bars.css, js/75-bars.js, js/99-boot.js |
| Shared primitives | js/05-components.js (ghostInput, typeahead, moneySlot, makeDirtyBar) |

## Sync history
- 2026-08-09T20:11:38Z — first import: recreated Todos, Properties, Goals and nav chrome; AION did not exist yet.
