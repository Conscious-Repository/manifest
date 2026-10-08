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

## P6 — generic materials, sourced products and substitution

P6.1 (domain): `construction/catalog.go` (products, material properties,
substitution, catalog validator), `catalog_rules.go`, `catalog_fixture.go`,
`catalog_test.go`, `construction/testdata/roof-wall/catalog.json` (three
visibly fictional products: a documented corrugated sheet, an undocumented
120 mm board, and a board whose fact cites another model's sheet);
`SetProduct` now pins the product's current revision and can apply its
dimensions through `SetDimension`. P6.2 (server/UI):
`server/construction_catalog.go`, `construction_catalog_test.go`,
`web/js/87-construction-catalog.js` (Catalog tab, substitution preview in the
inspector), research browser script extended.

- `go test -v ./construction -run 'TestConstructionCatalog' -count=1`: 3 tests
  pass. Covered: all 13 initial families present, every generic property
  unknown (no conductivity/compatibility/capacity invented), no default
  products; material properties need typed units and a test condition,
  verified-fact needs verified evidence, a stated value is a new material
  revision with the pinned revision still resolvable; product verification
  is derived (a fact verifies only from a verified passage of the product's
  own retained document), facts without evidence stay unverified, evidence
  for another model is noted and flagged (`product.evidence-mismatch`),
  15 cm is stored as 150 mm, a length in degrees and an unknown family are
  refused; substitution report (fact diff, dimension 100 → 120 mm that applies
  to the part, fictional note); applying it keeps the component id, sets
  120 mm and re-runs fastener review; `product.region` (site location
  unknown) and `product.dimension-unverified` flagged; an insulation board on
  the roof sheet refused; UpdateProduct makes revision 2 while the pin stays
  at 1; stale reaches the pinned component (`product.stale`), withdrawn
  cannot be pinned; forged verification in a stored catalog is refused.
- `go test -v ./server -run 'TestConstructionCatalog' -count=1`: pass
  (owner product/material routes, route family refusal, substitution preview
  writes nothing and shows fastener + product checks, apply through the
  assembly command route, cross-project 404, agent principal 403).
- `TestConstructionResearchBrowser` now also adds a product in the Catalog
  tab, previews the substitution in the inspector (thickness 100 → 120 mm,
  critical counts; model hash unchanged by the preview), applies it, marks
  the product stale and sees `product.stale`; screenshots
  `catalog-substitution-preview-1440.png`, `catalog-tab-1440.png`.
- Actual manufacturer acceptance needs authorized live source review; none of
  these fixtures is a real product.

## P7 — human/agent command equivalence, decisions and compare

P7.1 (domain): `construction/decisions.go` (ProposeDecision; owner-only
ApproveDecision/RejectDecision binding the exact decision revision;
`DecisionStates`), `construction/history.go` (`AssemblyHistory` from the
revision chain annotated by receipts; `CompareAssemblies`),
`history_test.go`. Server: `server/construction_commands.go` (assembly
history, compare of exact revisions, decisions read + decision command
route) + test. P7.2: `server/construction_agent_tools.go` (in-process,
revocable, problem-bound agent command capability; strict structured reply
parser) + test; UI `web/js/87-construction-decisions.js` (Decisions tab with
server comparison, assembly revisions with restore in History).

- `go test -v ./construction -run 'TestConstruction(CommandHumanAgent|Decisions|Compare)' -count=1`:
  3 tests pass — the owner's SetDimension and the agent's identical command
  give the same geometry hash (owner edit → undo → agent edit), receipts and
  history name owner vs `agent:alfred` with its capability; a second
  browser's undo on a stale revision is 409 naming the current revision;
  free text, a script op and an extra field are not commands (422); a removed
  id is never reused; an agent may propose (the decision binds the exact
  revision and keeps its unresolved issues) but agent approval/rejection is
  403 with the head byte-identical; the owner must name the exact decision
  revision; acceptance sets the selection and owner-selected lifecycle; an
  edit makes it stale while the selection stays on the accepted revision;
  approving a proposal whose assembly moved is 409; a later acceptance
  supersedes; comparisons match parts by stable id and diff issues.
- `go test -v ./server -run 'TestConstruction(CommandHistory|AgentTool)' -count=1`:
  2 tests pass — "increase insulation" as a structured agent reply and as the
  owner's control give identical geometry; a replayed reply returns the
  original receipt; agent approval 403 with an unchanged head; a capability
  cannot act on another problem or perform owner-only families (sources,
  steward, products); free text and extra fields are refused; revoked and
  expired capabilities are refused; no HTTP route accepts a capability.
- `TestConstructionResearchBrowser` now also proposes a decision, accepts it,
  edits the alternative (stale shown, selection pinned), compares the
  accepted revision with the current one and restores the accepted geometry
  from History; screenshot `decisions-compare-1440.png`.
- Deviation recorded: `server/chat_record_context.go` and
  `server/chat_owned_files.go` were not edited — the construction packet is
  composed from the construction store by the native seam (P8), so no chat
  record/owned-file path carries construction data.

## P8 — native delivery, preflight, cancel/retry/resume

P8.1 (native seam): `agentchat/delivery.go` gains an optional
`MessageContext.Construction` (nil and omitted for every other delivery) +
`agentchat/construction_context_test.go`; `server/agentchat.go` gets two
narrow hooks — every claim (first and in-loop) passes
`gateConstructionDeliveries`, and a construction turn uses
`constructionTurnPrompt` (exact retained packet, verified by hash, as data)
with only its bounded tool scope instead of the chat window/MCP preamble;
`server/construction_runs.go` (the NativeStep over Accept/Claim/runner/
Finish, cancel through RequestStop/CancelQueued, reply read from the
transcript, native-state reconcile), `server/construction_preflight.go`
(observed checks + `GET …/preflight`), `server/construction_steward.go`
("ask the agent": one native delivery, reply executed only as typed commands
under a short-lived problem-bound capability), domain
`Store.AttachConversation`/`RetainPacket`, protocol stub
`server/testdata/construction-hermes-stub.py`, tests
`server/construction_runs_test.go`. P8.2 (UI): native identity on the run
card, "Ask the agent" in the agent pane, `testdata/construction-native.cjs`
+ `TestConstructionNativeBrowser`.

- Preflight is observed, not assumed: native store, runner, exact-byte
  packet delivery, page extraction scope, an explicit bounded toolset with no
  web/shell/file/MCP/memory names, no fetch authority, and no vault write.
  Tool confinement and OS-level write access are `unverified` (not
  observable from the server), so production wiring (no `AgentToolsets`, no
  `AllowUnverifiedNative`) keeps native research **unavailable** with the
  reason shown; only isolated fixtures allow unverified checks.
- `go test -v ./server -run 'TestConstructionNative' -count=3`: 4 tests pass
  each time — one Accept per attempt in the run's own native conversation;
  the stub re-hashes the packet it received (declared = computed = attempt
  packet hash), sees toolset `construction-none` (not the chat's
  `web,memory`) and no chat preamble; requested vs observed model kept apart
  (runner-report); passages verified (1 kept, 1 fabricated rejected); the
  problem points at the native conversation; mismatch → waiting-input,
  resume refused until the owner accepts, then completes with no second send;
  `zeck` (no such profile) fails as `capability` with nothing sent and no
  substitution; without a bounded toolset native runs are 503; cancelling a
  running step interrupts the native delivery (stop requested, process
  killed by context) and cancels the run; the drain gate cancels a queued
  construction step its process does not own while an unrelated queued chat
  completes normally (a real bypass was found and fixed here: the turn loop's
  next claim skipped the gate); receipt states map to not-sent / uncertain;
  the steward request runs the stub's typed SetDimension as `agent:alfred`
  with a `cxcap-` capability and gives the owner's geometry, and an
  approving reply is refused with only the packet record committed.
- Regression: `go test ./agentchat` pass (fingerprints of nil and
  non-construction contexts unchanged, golden-checked); `go test ./server
  -run 'TestAgentChat|TestChatSupervision|TestHermes|TestChat'` pass, incl.
  `TestAgentChatPreflightFailureFinishesReceipt` and
  `TestAgentChatRecordsDispatchedToolScope`; `go test -race ./construction
  ./agentchat` and `go test -race ./server -run
  'TestConstruction(Native|Research…|AgentTool|Sources|Catalog|Command)'`
  pass.
- `TestConstructionNativeBrowser`: native run shows "tools
  construction-none · observed stub-model (runner-report)"; "Ask the agent"
  applies one agent command (thickness 150); screenshot
  `native-agent-1440.png`.
- Live provider trial: not run (needs separate authorization and a real
  preflight); the stub proves protocol only.

## P9 — export/restore, journey, security, docs

P9.1: `construction/export.go` (deterministic private recovery bundle: head,
members, ledger state, creation intents, every member artifact object and
its exact bytes, native extras, manifest with path/bytes/SHA-256, versions
and completeness per category), `construction/restore.go` (`ReadBundle` +
`Restore` into an empty root only), `construction/export_test.go`,
`cmd/construction-restore/` (+ test), `GET …/problems/{id}/export` in
`server/construction_exports.go`. P9.2: `server/construction_journey_test.go`,
`server/construction_security_test.go`, `server/testdata/construction-journey.cjs`
+ `TestConstructionJourneyBrowser`, the Export tab's recovery-bundle link,
`docs/construction-intelligence.md`.

- `go test -v ./construction ./cmd/construction-restore -count=1`: 53
  top-level tests pass, 0 skipped. Round trip: deterministic bundle; the
  original root removed; restore into an empty root brings back every
  document revision byte-for-byte (same tokens), the full receipt chain,
  inputs, derived exports, stage results, older assembly revisions; the
  model recompiles to the same hash and the detail package regenerates
  byte-identically without any provider; the restored store accepts new
  commits. Refusals (nothing written): tampered blob, `../`, absolute and
  backslash paths, a symlink entry, duplicates, an unlisted file, an 8 MiB
  zero bomb (ratio), a newer schema, a store path for another subject; a
  populated target (409), a target under a forbidden root (403), a relative
  target (422). CLI: verify-only, restore with native copies to an explicit
  empty dir ("not resumed"), populated-target refusal, forbidden root, usage.
- `go test -v ./server -run '^TestConstruction' -count=1`: 32 top-level tests
  pass, 0 skipped (includes the five real-backend browser journeys).
  `TestConstructionJourneyHomePilotExportRestore`: the 761 pilot from Home
  with the Home task scope → synthetic stand-in drawing → junction,
  insulation, visible timber → geometry, SVG/PDF/GLB/package → research
  cancelled mid-run and resumed → compare → agent proposal, agent approval
  403, owner acceptance → steward edit through the native path → recovery
  bundle (complete, with the native conversation copy) → original root
  removed → restore into an empty root → identical revisions, problem,
  assemblies, evidence, decisions, views, catalog, derived records, runs and
  history; the selected revision's geometry opens; a re-export of the same
  revision has the same geometry hash; the original export downloads
  byte-exact. `TestConstructionSecurityNegativeCases`: untrusted host,
  cross-site/cross-origin/no-origin/no-nonce writes, cross-site recovery
  export, encoded traversal paths, IDOR (input/document of A via B, another
  property, Home), generic artifact route blind, another problem's assembly
  and evidence, problemId mismatch, unknown op, NaN and 1e999 literals,
  negative/excess thickness, 257 operations, >1 MiB body (413), degenerate
  and NaN sections, URL sources incl. a redirect to the metadata address,
  link-local, loopback and IPv6 loopback recorded with zero fetches,
  credentialed URL refused, no HTTP client/dial/exec in construction code,
  and A's bundle carries none of B's private documents.
- `TestConstructionJourneyBrowser`: the pilot from TASKS › Home
  construction, stand-in drawing, edits, research cancel → resume (epoch 2,
  3 alternatives), decision accepted, detail package exported, the recovery
  bundle downloaded (492,768 bytes) and then restored by the Go side into an
  empty root; reload shows the same state; 390 px usable. Screenshots
  `journey-decision-1440.png`, `journey-decisions-390.png`.
- `go test -race ./construction ./agentchat ./chatstate -count=1`: pass.
- The journey script needs the real backend URL by design (persistence and
  the boundary are claims only the backend can prove); the stub mode of
  `construction-workbench.cjs` remains the direct `node` UI-only run.

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
