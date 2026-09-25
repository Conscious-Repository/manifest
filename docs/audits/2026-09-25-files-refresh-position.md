# Files reading position during refresh

Files retains the last successful list while refreshing the same scope/search. A failed refresh reports its error beside the retained results. Scope or server-search changes clear those results, preventing a failed new query from presenting an old list as its answer.

Rendering records the first visible file identity and its viewport offset, then restores that anchor after replacement. New rows inserted above the reader do not move the visible file. If that file disappears, the existing pixel-position fallback applies. Refresh uses the position at response time, allowing the reader to continue scrolling during the request.

The full-frontend Chromium regression failed before the implementation: a refresh replaced the visible file with a loading row and reset scroll from 650 to zero. It now passes for a delayed refresh, insertion above the viewport, failed refresh, and failed scope change. Existing capture, route, polling, phone and listener cleanup journeys also pass.

JavaScript syntax, release build and diff checks pass. Full `make test` retains the existing re-intake canary failures for unchanged `hermes/authority.go` and `hermes/claude_successor.go`; server passes. This fixture verifies visible Files reading position, not physical devices or every inspector's recovery behavior.
