# Recruiting usability walkthroughs — September 7, 2026

Three task-based browser walkthroughs by the implementation agent, followed by
retests. These are expert usability inspections, not a study with recruited users;
no completion-time improvement or user-success percentage is claimed. Benjamin
requested a more intuitive surface and the earlier clean-line visual treatment.

Baseline live data: 129 pending source results and 41 inbound candidates. Tests
used existing read paths and session-only Later/bring-back. No candidate was added,
passed, rejected or contacted to manufacture a test outcome.

| Scenario / success condition | Baseline observation | Fix and retest |
|---|---|---|
| 1. Find a result, recover from an empty filter, set it aside and find it again. | Choosing a role with no results left only a generic message; no reset. Later removed the matching result with no feedback. | One-click Reset filters restores 129 pending results. “Later this session” explains its duration and confirms with a toast offering a route back. Later filter and bring-back verified with the same result. Role clicks stay in Sources and show pending source counts. |
| 2. Read one candidate, then review successive candidates using the keyboard. | Clicking Next changed the candidate but focused BODY; another Enter could not continue. | Preserve the navigation control through local and asynchronous repaints. Verified focus remains Next at positions 2/41 and 3/41, including an Enter activation. Added a named position status and explicit Back to People action. |
| 3. From a saved Place, resume reviewing that search's undecided results. | Review results opened Search history; a previously passed person appeared first. | Opens the review queue scoped to that exact search, clearing stale filters. Yablonskiy Lab opens with 24 pending results and a visible search heading. All search results restores the full queue. Completed searches display their decisions. |

## Visual and responsive follow-through

Candidates, source results, search history and resume text now use horizontal
hairlines, square/absent outer frames and open whitespace. The selected candidate
keeps a quiet selection marker. Action buttons remain recognizably interactive.
This is a recruiting preference, not an unsolicited redesign of other tabs.

Desktop Jarvis verified visually. Phone inspection also exposed a breakpoint
crossing issue: the desktop role rail could remain expanded until navigation.
The existing mobile breakpoint handler now repaints recruiting in both directions,
including transferring a selected candidate to/from the shared phone sheet.
No additional resize listener or alternate breakpoint was introduced.

## Verification

- Seven Node behavioral tests, including exact run scoping, stale filter recovery,
  decided-run access and the existing evidence/queue tests.
- Modified JavaScript syntax and `git diff --check`.
- Server regression suite (includes embedded web checks).
- Browser replays of the three scenarios; computed borders show zero outer radius
  and side borders on result rows; phone horizontal overflow checked.
- Delivery: commit, push to main, deploy using `make deploy`, verify served assets
  and the active Metis service. Live verification is reported in the completion.
