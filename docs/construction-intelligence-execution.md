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

## P1 — durable problems inside the property and the shared Home

Commits `fa2e01d9` (P1.0 private durable pool mode), `c010fcbb` (P1.1 domain
store), `ab6b4d70` (P1.2 server, auth guard, inputs, UI entry).

- Storage: `<dataDir>/construction/` (0700) with a private `artifacts.Registry`
  (0700 dirs, 0600 files, fsync of file + directory on every write). The only
  mutable file per problem is `projects/<sha256(kind NUL id)>/problems/<id>/head.json`;
  commits validate → stage immutable documents (`Retain`, content-addressed) →
  write the receipt document → append membership → atomic head rename + dir
  fsync → publish the ledger projection (recoverable from receipts).
- Replay/CAS: request id + canonical payload hash → one receipt; reuse with
  different content → 409; stale expected revision → 409 with the current
  token; a second writer (process or Store) → 503 via `flock`.
- Tests (all pass under `confine`): `go test -v ./construction ./artifacts -count=1`;
  `go test -v ./server -run '^TestConstruction(Problem|Inputs|Boundary|Concurrent|Replay)' -count=1`.
  Covered: reopen with the same ids/hashes, crash at four commit stages
  (before/after head rename), lost-ACK replays (create, command, upload),
  newer schema read-only with exact bytes, unknown extensions preserved,
  tampered blob reported (not reset), symlinked head refused, 0700/0600 modes,
  cross-project 404s (including identical bytes deduplicated across projects),
  Host/Origin/Fetch-Metadata/nonce/principal refusals, routes absent from the
  portal, deal-share and bare web handlers, generic `/api/artifacts*` routes
  blind to construction content, vault file hashes and source-writer counters
  unchanged in every server test.

## P2 — canonical assemblies and deterministic geometry

Commits `570e28f1` (P2.1 template, compiler, rules, operations), `1ce0858c`
(P2.2 GLB and assembly endpoints).

- Template `roof-masonry-junction`: every §4 member, every number a labelled
  illustrative user-assumption; wall `unknown`, orientation `unresolved`.
- Compiler: pure Go prisms (ribbon/convex caps), all closed and outward-wound
  (`TestConstructionGeometryWatertightAndOutward`); fixed-id golden
  `construction/testdata/roof-wall/assemblies.json` (template hash, 13,640
  triangles, stack 166 mm, structural embedment 64 mm; 150 mm insulation →
  stack 216 mm, embedment 14 mm).
- Rules: blocking (layer gap, disconnected, reverse lap, unsupported strategy,
  budgets) refuse the commit; stored issues keep ids by rule+target and carry
  specialist-review flags. Insulation 100→150 raises
  `structure.fastener.stack-changed` and `structure.fastener.embedment`;
  `structure.specification` is always critical.
- Branches: unknown/cavity/solid/headwall/sidewall (`TestConstructionValidation*`);
  through-wall refused until a cavity exists; step flashing and stepped
  sidewall trays refused as not modelled for corrugated metal; the headwall
  apron is switched off (never rotated) at a sidewall.
- `go test -race ./construction -count=1`: pass.

## P3 — four-pane workbench

Commits `1d4d6033` (P3.1 views, renderer, panes), `b30fca45` (P3.2 browser
journey + recorded stub).

- `go test -v ./server -run '^TestConstructionWorkbenchBrowser$'` (real Go
  backend): pass. Observed on this machine (Chromium 145, SwiftShader WebGL2):
  first render 68–79 ms after renderer creation, scene build 35–42 ms, render
  call avg 8.9–10.1 ms / max 28–32 ms, JS heap 33–40 MB, 13,640 triangles.
  These are software-WebGL numbers, not device performance.
- Explode invariance: an orthographic pick of the isolated outer wythe before
  and after a 473 mm presentation offset returns the same canonical point
  (drift 0 mm, 3/3 runs) — after fixing a real bug where picks used last
  frame's matrices.
- Stub mode: `node server/testdata/construction-workbench.cjs` (no backend)
  renders the recorded fixture regenerated by `TestConstructionStubFixtureCurrent`.
- Screenshots: `/tmp/manifest-construction-qa/shots/workbench-{320,390,768,1100,1440}-{default,jarvis}.png`,
  `workbench-section-realistic-1440.png`, `stub-{390,1440}.png`.
- Direct handles are numeric (slider) handles with a server-computed tentative
  preview and Commit/Cancel; there is no 3D drag gizmo in this MVP.

## P4 — sections and exports

Commits `46b4acb1` (P4.1 sections, drawing IR, SVG/PDF, package),
`ab471cd6` (P4.2 export routes, 2D section UI).

- `go test -v ./construction -run 'Section|SVG|PDF|GLB|DetailExport' -count=1`: pass.
  Analytic checks: layer areas D·t/cosθ; rafter, wythe boxes; stack chain
  200/25/1/100/2/38/0.5 mm; wall chain 100/100 (unknown) and 100/50/100 with
  the cavity labelled unresolved; corrugation height 18.5/cosθ and 31 crests in
  an along-wall cut; oblique wythe area footprint/|n_z|; coplanar interface
  reported once; tangent plane at the wall top; golden
  `construction/testdata/roof-wall/sections.json`.
- Plane-coincidence tolerance is 0.001 mm because IR positions are stored to
  0.0001 mm (found when a coplanar test split a face).
- Drawings: A3 at 1:5 by default; 100 mm prints as 20 mm (56.693 pt); an
  oversize window is refused with a message, never fitted. PNG is not
  generated server-side (the browser can save a screenshot; it is not an
  export).
- `go test -v ./server -run '^TestConstruction(Section|Export)'`: pass;
  `TestConstructionSectionBrowser` (real backend): pass; screenshot
  `/tmp/manifest-construction-qa/shots/section-2d-1440.png`.

## P5 — staged research and evidence graph

P5.1 (domain): `construction/evidence.go`, `evidence_rules.go`,
`research.go`, `research_fixture.go`, `runs.go`, tests `evidence_test.go`,
`research_test.go`, fixtures `construction/testdata/roof-wall/sources.json`
and `source-pages.txt` (12 visibly synthetic sources: three that support,
one that contradicts, a fictional manufacturer, a fictional-jurisdiction code
excerpt, a sidewall detail, a secondary forum post carrying a prompt
injection, a 404, a 429, a timeout, a paywall, a scanned PDF without a text
layer, and a leaflet against which two fabricated citations are tested).
P5.2 (server/UI): `server/construction_research.go`,
`construction_sources.go`, tests `construction_research_test.go`,
`construction_sources_test.go`, `web/js/87-construction-research.js`,
`testdata/construction-research.cjs` + `TestConstructionResearchBrowser`.

- Run lifecycle: every stage claim and finish is its own commit (request ids
  `claim-<attempt>`, `finish-<attempt>`, `dispatch-<attempt>`), so progress is
  read only from commits; attempts carry parent, epoch, input/result hashes,
  error class and native identity; results are retained private artifacts.
  Evidence enters the problem at extract, alternatives only at publish, both
  fenced by epoch and the persisted stop request (a late publish after a stop
  is kept as a `fenced`, unselected attempt). Startup `ReconcileRuns` marks
  any running stage `disconnected` (never re-run); `main` calls
  `ReconcileConstruction` before `ResumeAgentChats`, and handlers ensure it
  once.
- `go test -v ./construction -run 'TestConstruction(Evidence|Research)' -count=1`:
  9 tests + 6 subtests pass. Covered: exact (with byte offsets) and
  normalised quotes with the normalisation recorded; wrong page, absent
  quote and nonexistent page rejected (nothing added); a URL source is never
  fetched (local listener saw 0 requests) and a URL alone is never verified;
  the contradicted claim keeps both sides; fictional-jurisdiction code text is
  `adapted-precedent`, never directly applicable; secondary text is a lead
  only; the injection source only gains a warning (no decision, selection or
  approval appears); three conditional alternatives (surface / reglet if
  solid / cavity tray if cavity) with deterministic issues
  (`source.fictional`, `source.contradicted`,
  `source.applicability.{jurisdiction,product,detail,wall}`,
  `wall.condition.unverified`) and a graph path
  Source → Evidence → Claim → Junction → Assembly via `graph.Build`/`Paths`;
  compile and publish give identical model hashes; restart before/after the
  head rename at acquire, extract and publish (6 cases); cancel during
  acquisition; retry with explicit parent and replay-safe request ids;
  owner-corrected questions re-run decompose/plan only (acquire attempts 1,
  adapter calls 1); native protocol with a fake step (one send, requested vs
  observed model, mismatch → waiting-input until the owner accepts, uncertain
  outcome → resume refused / retry sends a new request id, reply finished
  while down → resume adopts it without resending).
- Mutation check: disabling the stop fence or accepting a not-found quote
  makes 7 of these tests fail (then restored).
- `go test -v ./server -run 'TestConstruction(Research|Sources)' -count=1`:
  4 tests pass (planned run inert across GETs, native refused 503 while the
  preflight is not satisfied, events strictly after a sequence, attempt
  results served exactly, cross-project reads 404, cancel → resume, restart
  between stages → disconnected → resume reuses acquisition, questions route).
- `TestConstructionResearchBrowser` (real backend, fixture slowed 1.5 s):
  pass — progress from the durable run, Runs tab (acquisition outcomes,
  rejected citations, excerpt requests, not-used evidence), Evidence tab
  (fictional badges, injection warning, contradicted claim, five-node path,
  path node selects the part), owner source + verified passage, wrong-page
  passage refused, cancel then resume (epoch 2), reload shows the same run,
  390 px without sideways scroll. Screenshots
  `research-running-1440.png`, `research-tab-1440.png`,
  `research-evidence-390.png`.
- Limitations recorded, not hidden: autonomous web acquisition is not built
  (no search provider; capability reported `unavailable`, imported owner
  documents only, fixture in tests); PDF/OCR extraction is not bundled (PDFs
  retained, excerpts requested); live source review is pending.

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
