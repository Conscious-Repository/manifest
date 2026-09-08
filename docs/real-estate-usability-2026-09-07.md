# Real Estate usability audit — 2026-09-07

The owner's priority is to preserve the current Backlog (including Aion's analogous layout) and reduce the clicks needed to reach a property's work page. This audit reviewed Backlog, Portfolio, a property work page, Goals, Money, Contractors, Settings and Map using live data through the read-only browser preview. No business records were changed during testing.

## Findings addressed

1. **Property access took an unnecessary detour through metadata editing.** Portfolio row/address clicks now open the property page directly. An explicit Edit details action retains the metadata inspector, and the empty inspector no longer occupies space before selection.
2. **Property navigation was inconsistent across views.** A native, keyboard-accessible property switcher now appears across Real Estate, including property pages. It remains visible while scrolling. Backlog property task/decision references are direct links, as are property headings in Goals. Links stop row-selection propagation and retain normal browser link behavior.
3. **The map and parcel thumbnail displayed API-key-required watermarks.** Replaced the failed basemap with OpenStreetMap's standard browser tile endpoint and visible attribution on both maps. Requests use normal browser caching and referer behavior; there is no prefetch or offline download.
4. **The property editor initially misrepresented the entity as blank.** The saved entity is now present immediately, before additional registry options arrive.
5. **A dense property page overflowed on phones.** At 390px the content initially measured 568px wide. Work rows and contract labels now wrap, the property title has a full line, and the unit table scrolls inside its own container. Final page width measured 390px.

## Three browser walkthroughs

- **Reach a property's work:** selected 4848 Fountain from Map using the switcher; opened it from Portfolio through its address link; opened it directly from a Backlog property reference. Each reached the work-page heading without a metadata-editor detour.
- **Edit without losing access:** used the separate Portfolio Edit details control and checked its inspector. No fields were changed. The work-page link remains available there.
- **Use it on a phone:** switched between 4848 Fountain and 743 N Euclid at 390×844, inspected dense work/contract rows, measured overflow, and scrolled 2,613px down the property page. The property switcher remained visible at the top of the content viewport. Restored the desktop viewport afterwards.

JavaScript syntax checks, `git diff --check`, and `go test ./server` passed. These are expert browser walkthroughs, not recruited-user testing or a physical-device Safari test.

## Research and application

[Buildium maintenance workflows](https://www.buildium.com/features/property-management-maintenance-software/) connect work, ownership, status, estimates and accounting within property context. [Buildertrend project navigation](https://buildertrend.com/help-article/navigating-project-management/) links project updates and related work; its [Daily Logs guidance](https://buildertrend.com/help-article/daily-logs-overview/) emphasizes recorded progress and issues. Applied here as direct access to the existing property work surface rather than a new dashboard or duplicate task system.

The map change follows the [OpenStreetMap tile usage policy](https://operations.osmfoundation.org/policies/tiles/): standard HTTPS endpoint, visible attribution, ordinary browser identification and caching, no bulk requests.

## Remaining observations

- Backlog already provides decisions and owner-grouped work the owner values; retained its layout.
- Money is primarily a filing workbench, not a property financial overview. Its current search targets unfiled work while filed transactions live under entity histories; a unified history search is a future improvement, not part of this navigation fix.
- Contractors already exposes direct property links and working/bidding filters. Goals has useful property grouping; improved link semantics without changing the hierarchy.
- Settings contains financial assumptions and registry writes. Reviewed the presentation without changing values.
- Property work pages remain long. A future pass could add section navigation and folds for completed work, informed by use of the now-direct property paths.
