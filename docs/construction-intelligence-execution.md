# Construction Intelligence — execution evidence ledger

Plan: `/tmp/manifest-construction-inputs/owner-approved-plan.md` (owner
approved, §12 binding defaults). Owner brief:
`/tmp/construction-intelligence-owner-requirements.md`. Both were read from
their explicit paths and not copied into the tree.

This ledger records what was actually run and observed in this worktree.
Nothing here is a claim about live data, live providers, deployment or a
physical device. "Offline-verified" means exercised by hermetic fixtures
under the confinement described below; "live-unaccepted" means it still needs
the owner-authorized live acceptance in plan §11.

## Worktree, base and runtime

| Item | Value |
|---|---|
| Worktree | `/home/benjamin/src/manifest-construction-intelligence` |
| Branch | `feat/construction-intelligence` |
| Base commit | `6fa9c630719b75097210ea095465d88eb157941a` (equals the plan's recorded base; no re-audit of advanced seams needed) |
| Executor | requested Claude Opus 5.5; the session reports model id `claude-opus-5-5`. No independent runtime telemetry was available beyond that. |
| Go | `go1.25.8 linux/amd64` from the local module-cache toolchain (`~/go/pkg/mod/golang.org/toolchain@v0.0.1-go1.25.8.linux-amd64`); `/usr/bin/go` is 1.22.2 and cannot build `go 1.25.8` with `GOTOOLCHAIN=local` |
| Node | v22.23.2 |
| Playwright | resolves from `NODE_PATH=/home/benjamin/.cache/manifest-qa/node_modules`; Chromium 145.0.7632.6 headless |
| WebGL in tests | WebGL2 via ANGLE + SwiftShader (software). Proves code paths, not device performance |
| Env | `GOTOOLCHAIN=local GOPROXY=off GOSUMDB=off GOFLAGS=-mod=readonly` |

## P0.1 — test confinement and baseline

Confinement wrapper (outside the repo, not committed):
`/tmp/manifest-construction-qa/bin/confine`. It runs the command in a new
user + mount + network namespace (`unshare --user --map-root-user --mount
--net`):

- network: only `lo` is up — DNS lookups fail and a TCP connect to an
  external address fails (both checked inside the namespace);
- `/private`, `/shared`, `/srv`, `/opt`, `/mnt`, `/media` are empty tmpfs;
- `$HOME` is an empty tmpfs with only an allowlist bound back: this worktree
  (rw), Go build cache (rw), Go module cache + toolchain (ro), the QA
  `node_modules` (ro), Playwright browsers (ro) and the Node distribution
  directory (ro). The dirty main checkout, the vault, agent homes and config
  files are not visible;
- `/tmp` is a fresh tmpfs except `/tmp/manifest-construction-qa` (rw).

Verified inside the namespace: `ls -A /private /shared` empty, `$HOME`
lists only `.cache go src .hermes(node only)`, `getent hosts example.com`
fails, `/dev/tcp/1.1.1.1/443` fails, a write into the module cache fails
(read-only), and the main checkout path does not exist.

Baseline before any change (`go test -json -count=1 ./...` under `confine`,
6m19s): 71 packages pass, 31 have no tests, **1 fails**: `manifest/server`
`TestFixtureSharedChat` — `shared-chat.cjs` loads
`https://unpkg.com/react@18.3.1/...`, which the network namespace refuses. This
is a pre-existing network dependency, unrelated to this feature, and stays a
baseline failure under confinement. Tests: 3713 pass, 28 skip (live/corpus
tests that need the real vault or live providers), 1 fail. Raw output:
`/tmp/manifest-construction-qa/baseline-go-test.jsonl`. The run left no
tracked-file changes (`git status` clean apart from new files).

## P0.2 — renderer dependency, GLB, sections and vector PDF

Renderer dependency: three.js **0.180.0**, MIT. The registry tarball was found
in the local npm content cache; its SHA-512 was recomputed and equals the
registry integrity
`sha512-o+qycAMZrh+TsE01GqWUxUIKR1AL0S8pq7zDkYOQw8GqfX8b8VoCKYUoHbhiX5j+7hr8XsuHDVU6+gkQJQKg9w==`.
Only `build/three.module.min.js`, `build/three.core.min.js` and `LICENSE`
were extracted into `server/web/vendor/three-0.180.0/`, unmodified (hashes in
`PROVENANCE.md`, pinned by `TestConstructionVendoredThreePinned`). No CDN, no
install, no network.

Spike (`tools/construction-spike/`): one corrugated panel, one bent flashing,
two wythes and a 1000 mm calibration cube.

| Check | Command | Result |
|---|---|---|
| Go proofs | `go test -v ./tools/construction-spike -count=1` | 9/9 pass, 0 skip |
| Browser proof | `node tools/construction-spike/viewer.cjs` (under `confine`) | pass: three r180, WebGL2 SwiftShader, 173083 drawn pixels, semantic pick incl. hide-to-expose; screenshot `/tmp/manifest-construction-qa/spike-viewer.png` |

Observed: GLB 97924 bytes, sha256
`0d53a80f9870067f2a2a9b0032135d768f09ee2e24b2e00b0cb37b0fe43201b6`, identical
across compilations; corrugation chord error 0.1735 mm at 16 segments per
76 mm wave; oblique wythe section area equals the analytic footprint/|n_z|;
a coplanar cut is reported once; 100 mm at 1:5 prints as 56.693 pt; PDF text is
escaped printable ASCII with the PDF base-14 Helvetica (no font program is
embedded, so viewers substitute a metric-compatible Helvetica; no raster
content).

Go/no-go: GO for a stdlib-only Go GLB/section/SVG/PDF path and a pinned local
three.js renderer. Blender: absent and not needed.

## P0.3 — native scope and auth contract

`go test -v ./server -run '^(TestArtifactPreviewSelectedBytes|TestAgentChatPreflightFailureFinishesReceipt|TestAgentChatRecordsDispatchedToolScope)$' -count=1`
under `confine`: 3/3 pass (5 preflight subtests).

Findings from source inspection:

- The private listener binds `127.0.0.1:<port>` (`main.go`); `Server.Handler`
  has no general owner-authentication middleware. Existing private writes
  check at most `Origin == Host` (`sameOrigin`). The upstream tailnet/proxy
  configuration is not visible from source and was not inspected (UNKNOWN).
- Native chat turns run the Hermes CLI with the `readTools` toolset label
  recorded per delivery (`agentchat.ToolScope`). A toolset label is not proof
  of path confinement; construction therefore never hands a mutation tool to
  the native runner (see P7/P8).
- The global artifact registry is enumerable and readable by id/hash through
  generic routes (`/api/artifacts`, `/api/artifacts/get`,
  `/api/artifacts/content`) with no membership check. Construction content
  must not be stored where those routes can reach it (see P1).

## Decisions taken (plan-conformant defaults)

1. **Subject binding (§12.1).** `subjectRef` is typed `{kind: "property"|"home", id}`
   from the first schema. Property problems live under
   `/api/properties/{slug}/construction`, Home problems under
   `/api/home/construction` with `id: "home"` (the one shared Home domain);
   optional scope links a property work node or a shared Home task by exact
   id. Source records are only read.
2. **Private artifact pool.** Construction documents and inputs use the
   existing `artifacts.Store` + `artifacts.Registry` code, instantiated under
   `<dataDir>/construction/artifacts` with an opt-in private/durable mode
   (0700 directories, 0600 files, fsync). Generic artifact routes are bound
   to the global registry and cannot enumerate or read construction content;
   per-problem membership is the ACL for construction downloads.
3. **Trusted-local-host posture (§12.2).** Construction routes are registered
   only in `Server.Handler`, check Host against loopback plus configured
   trusted hosts, require same-origin `Origin`/`Sec-Fetch-Site`, and require a
   per-process mutation nonce. A hostile process with the owner's OS identity
   is outside the MVP boundary.

## Commits

| Commit | Unit |
|---|---|
| (filled in as each unit lands) | |
