# Agents and Settings on phones

The mobile Agents schedule hid run/pause controls, truncated job names behind owner prefixes and status badges, and spent the first screen on duplicate headings and the entire agent directory. Settings lacked the shared page gutter, compressed navigation, and retained desktop-oriented host/value and form layouts.

Agents now folds its directory through the shared accessible disclosure (open state survives repaint), separates linked agent identity from full job names, and retains visible run/pause and health controls. The duplicated next-up summary and page title disappear on phone; the schedule retains each next fire. Runs and wizard layouts wrap at the central phone breakpoint. Settings uses the shared page column, four visible navigation destinations with current-page semantics, divider-based connection rows, labeled metadata, wrapping actions/forms, and stacked host/value rows. Navigation clears stale header status from the previous Settings group; late responses cannot apply another group's header status.

All phone rules live in 95-mobile.css and use existing tokens/touch sizing. Native links and the existing collapsibleSection are reused. No jobs, accounts, schedules, connections or model settings were changed.

Validation: Chromium read-only browser checks of seven routes (schedule, runs, new agent, and all four Settings groups), at 320/390/768/1000/1440 widths in Jarvis and light themes. Checked document horizontal containment, visible schedule actions, all Settings destinations, selected-link semantics and directory state after repaint. No browser page errors. Inspected rendered phone and desktop screenshots. Requests other than GET/HEAD were blocked in the browser harness; no job or connection action was executed. Physical iPhone testing remains user feedback.

Passed JavaScript syntax checks, git diff --check, focused server CSS/Settings/Hermes/UI/theme tests, and Go build. Browser harness and screenshots are temporary local QA artifacts under /tmp/agents-settings-*.cjs and /tmp/{before,final,desktop}-*.png.
