# Files refresh after local artifact mutations

Successful native output capture, coding result capture and workspace artifact saves emit a local artifact-change event. An existing Files tab reloads its current scope and query using its existing abort/fencing logic. Closing Files removes the listener. Saves with incomplete required receipts do not notify.

This is local notification, with no polling or cross-device invalidation. It does not cover every possible artifact mutation or promise scroll preservation. Existing explicit refresh remains available.

Validation:

- Full-frontend Chromium journey opens Files before native capture, retains its search and conversation scope, observes the captured output without explicit refresh, and checks listener removal after close. Existing coding capture, stale route, polling and phone journeys pass.
- Artifact save recovery and text editor browser fixtures pass.
- JavaScript syntax, release build and diff checks pass.
- `make test`: server passes (41.049s); the only failing package is the existing re-intake canary, reporting unchanged `hermes/authority.go` and `hermes/claude_successor.go` hashes.

Production checks are read-only; fixture captures and saves do not submit production outputs or messages. Broader workbench acceptance remains open.
