# Construction Intelligence

Private construction research and design inside the existing Manifest
contexts: a **construction problem** belongs to an OODA property
(`#/properties/<slug>/construction/<id>`) or to the shared Home
(`#/tasks/home-construction/<id>`). It holds editable millimetre assemblies,
deterministic geometry and true sections, evidence with page-verified quotes,
staged research runs, a problem-local evidence-pinned catalog, owner
decisions, views, exports and a private recovery bundle.

> Research/design assistance; not approved for construction; field, code,
> structural and manufacturer verification required.

That notice rides on every view, drawing and export. "Accepted for project"
is a human project decision — never approval for construction. Geometry
validity is not physical appropriateness, code compliance or licensed review.

The first owner-facing problem is **761 N Euclid — Back Addition**, entered
from TASKS › Home construction. Every bundled fixture is visibly synthetic;
the historical 761 drawing is an optional owner input that the code never
reads, and nothing about the real site is assumed.

## Where things live

- **Records**: `<dataDir>/construction` — outside the vault and every
  team/public tree (`main.go` passes those as forbidden roots). Directories
  0700, files 0600, fsync on write, a single-writer `flock`.
- **Model**: each problem has one mutable `head.json` naming immutable,
  content-addressed document revisions (problem, assemblies, validation
  reports, catalog, evidence, decisions, runs, views, derived exports) in a
  private artifact registry. Every change is one commit with a receipt
  (operations with before/after, actor, request id, payload hash); a request
  id replays to the same receipt. `members.jsonl` is the per-problem ACL:
  a content hash alone grants nothing.
- **Source records** (property, Home tasks, goals, budgets) are only read.
  Tests prove zero writes and byte-identical vault files.

## The workbench

Four synchronized panes (keyboard-resizable on desktop; one at a time on a
phone): **Agent** (steward, open questions, research runs, ask the agent),
**Model** (pinned three.js r180 renderer with a technical fallback, picking,
section plane, explode, measurement, views), **Assembly** (junction,
parameters, layer stack, selected part, material/product with a previewed
substitution) and **Research** tabs: Problem, Runs, Evidence, Catalog,
Alternatives, Decisions, Issues, History, Export.

## Assemblies, geometry, sections, exports

- Canonical units are mm in the `construction-xyz-zup-mm/1` convention;
  components have stable ids (removed ids are tombstoned, never reused).
- Every edit is a typed command (`SetDimension`, `SetJunctionStrategy`,
  `SetWallCondition`, `SetProduct`, …). The same command from the owner and
  from an agent produces the same geometry hash; receipts tell them apart.
- Deterministic validation: blocking findings refuse the commit; other
  issues (wall condition unknown/unverified, moisture transitions, fastener
  review, product evidence, evidence applicability…) keep stable ids.
- Sections are true plane cuts of the compiled mesh; one drawing IR gives
  SVG (physical units) and vector PDF at a stated paper and scale; GLB, the
  section and the redacted **detail package** are all tied to one exact
  assembly revision and geometry hash.

## Research runs and evidence

A run has eight durable stages — decompose → plan → acquire → extract/verify
→ synthesize → compile → validate → publish — and each claim and result is
its own commit (attempt ids, parents, epochs, input/result hashes, error
classes, native identity). Stage results are retained artifacts; a resume
reuses a completed stage only when its inputs still match (correcting the
questions re-runs decomposition and planning, not acquisition).

- **Quotes are verified** against the retained page text (exact, or a
  recorded whitespace/typography normalisation). A quote not on that page,
  or a page that does not exist, adds nothing. A URL alone is never
  evidence: source URLs are metadata and are never fetched.
- Source text is data: instruction-like text is recorded as a warning and
  can change nothing (no stage has an action field it could fill).
- Disagreement is kept, not averaged; code text from a jurisdiction the
  site has not established is never "directly applicable"; secondary
  discussion is a lead only. Applying evidence for another orientation, wall
  type, product or jurisdiction raises a deterministic issue.
- Synthesis proposes up to three **conditional** alternatives only where
  verified evidence supports them, built from the same typed commands and
  published (fenced by epoch and stop request) for owner review; gaps are
  listed as missing research.
- Cancel persists the stop first; a running stage is cancelled; a late
  result is kept as an unselected `fenced` attempt. Retry starts a new epoch
  and attempt with an explicit parent. On restart every running stage is
  reconciled to `disconnected` before anything can dispatch; resume never
  resends an agent request whose outcome is uncertain — only an explicit
  retry does, with a new request id. Refreshing never starts work.

**Acquisition today**: imported owner documents (inputs registered as
sources). Autonomous web acquisition is not implemented (no search provider)
and is reported unavailable; PDF/OCR page extraction is not bundled (PDFs are
retained; excerpts are requested). Tests use a synthetic fixture adapter.

## Catalog and substitution

Every problem starts with generic material families whose technical
properties are unknown until a source states them (typed units, test
condition, provenance; verified values need verified evidence; each change
is a new material revision). Actual products are entered from sources:
documents pin the retained source revision, a fact verifies only from a
passage of the product's own document, evidence for another model is
flagged, and products carry geography, check date and lifecycle
(active/stale/withdrawn). Pins never follow catalog head. Choosing a product
previews the substitution — fact diff, dimension changes, re-run validation —
before the owner applies it. No procurement, stock, price or certified
performance exists here.

## Decisions, comparison, history

Anyone may propose a decision; it binds one exact assembly revision and
keeps that revision's unresolved issues. Only the owner accepts (naming the
exact decision revision) or rejects. Acceptance pins the problem's selected
revision; a later edit shows the acceptance as stale while the selection
stays put. Comparisons diff exact revisions by stable id (parameters,
junction, parts, materials/products, issues, evidence, geometry); assembly
history lists every revision with its actor; restore is a new revision.

## Agents

- Alfred is the default steward; Zeck only when explicitly chosen. Agent
  identity is separate from provider/model; requested and observed model are
  recorded separately, unknown stays unknown, and a mismatch blocks further
  autonomous steps until the owner accepts it.
- Agent steps are **real native chat deliveries** (Accept → Claim → existing
  runner → Finish) in a run's own native conversation, carrying the exact
  retained packet (hash-verified, as data) and only an explicit bounded tool
  scope. Construction keeps only the native identity, never a transcript.
- Every claim of a queued construction step passes a gate: it runs only if
  this process owns the run on the current epoch with the attempt running
  and no stop requested — so stops and restarts survive the startup drain;
  unrelated chats are untouched.
- Agent edits go through an in-process, revocable capability bound to one
  problem: drafts/proposed alternatives, views, annotations, evidence links,
  decision proposals. Owner-only operations (accept/reject, steward, problem
  facts, sources/evidence/catalog entry) answer 403 and change nothing.

**Preflight** (`GET …/preflight`) is observed, not assumed: native store,
runner, exact-byte packet delivery, page extraction scope, an explicit
bounded toolset without web/shell/file/MCP/memory names, no fetch authority,
no vault write. Tool confinement and OS-level write access cannot be
observed from the server, so they are `unverified`, and unverified checks
block. In this build native construction steps therefore cannot be switched
on by configuration: native research and "ask the agent" are **unavailable**
in production and the UI says why (only isolated test fixtures allow
unverified checks). A live provider trial needs separate authorization and a
real preflight.

## Export and restore

- **Detail package** (Export tab): the selected revision's assembly,
  validation, section SVG/PDF, GLB, schedule, evidence summary, README and a
  manifest of hashes; private narrative, inputs and contacts omitted.
- **Private recovery bundle** (`GET …/problems/{id}/export`, Export tab):
  the problem's complete retained closure — head, membership, ledger state,
  creation intents, every member artifact object and its exact bytes — plus
  read-only copies of the native conversations it points at, with a manifest
  of paths, sizes, SHA-256s, versions and completeness per category; what
  could not be included is listed, never claimed.
- **Restore** only into a NEW directory outside forbidden roots:

      construction-restore -bundle problem-recovery.zip -verify-only
      construction-restore -bundle problem-recovery.zip -target /abs/new/dir \
          -forbid /path/to/vault [-native-out /abs/new/native-dir] [-max-total-mb N]

  - **Target rules.** The target must not exist in any form, including an
    empty directory or a dangling symlink. Its parent must be an existing
    real directory with no symlink anywhere in its path. Every directory up
    to `/` must be owned by root or by you, and none may be writable by
    group or others unless it has the sticky bit (as `/tmp` does).
    `-native-out` follows the same rules and is checked before the restore
    starts.
  - **Validation in memory first.** The bundle is read from the file, not
    loaded whole, and checked in memory before anything is written:
    - A header pass, before any entry is decompressed, refuses too many
      entries (over 50,000), an entry over 32 MiB, and a decompressed total
      over the budget (256 MiB by default). It also refuses an entry over
      1 MiB that expands more than 500×, and an archive that decompresses to
      more than 100× its size (once the total passes 16 MiB).
    - Then every path, size and hash is checked.
    - `-max-total-mb` raises the budget for your own larger bundle (at most
      2048) and never lifts a ratio check.
  - **Staging.** The store is rebuilt and verified in a private staging
    directory beside the target. Artifact ids and revisions must come back
    identical, and the problem is reopened from the restored bytes. Only
    then is the target name created exclusively (0700) and the stage renamed
    onto it, and the problem is verified again from there. A refused restore
    leaves no target and no stage.
  - **Native copies** are written only to the explicit new directory,
    through an `os.Root` anchored on it (names cannot leave it, and files
    are created exclusively). They are never resumed.
  - **Limits of the target checks.** The store writes use path names. The
    rules refuse unsafe targets rather than anchor every write to a
    directory descriptor, so they protect against other OS accounts, not
    against a process running as you. The restore does not claim to prevent
    every race on path names.
  - **Export side.** The export refuses a bundle a restore would refuse. It
    stores entries uncompressed when compression would exceed a ratio
    budget. When a bundle needs more than the default budget, its
    `README.md` gives the `-max-total-mb` to use.

## Access boundary

Construction is a **local host-trust feature, not an authenticated one**.
Manifest has no verified owner authentication. The routes answer a request
only when all three hold:

- its TCP peer (`RemoteAddr`, which net/http records from the accepted
  connection; nothing in the handler chain rewrites it) is a loopback
  address;
- its `Host` names loopback (`localhost`, `127.0.0.0/8`, `::1`);
- it carries no proxy forwarding header (`Forwarded`, `X-Forwarded-*`,
  `X-Real-IP`, `Via`, `Tailscale-*` and similar).

Whoever reaches the routes that way is treated as the owner. That is a
property of the connection, not proof of a person. Every process on this
computer qualifies. So does any remote client whose traffic an
operator-created TCP forward or tunnel delivers onto loopback: `ssh -L` or
`-R`, socat, `tailscale serve --tcp`, or a proxy that strips its headers and
rewrites Host. The application cannot detect a raw TCP forward: at the
application layer it is indistinguishable from a local browser.

**Remote use is therefore unsupported**, whether through the tailnet, the
LAN, a reverse proxy, a port forward or any other relay or tunnel. Do not set
one up in front of Manifest's private listener. Remote use needs a verified,
authenticated owner gateway, which does not exist; adding one means changing
`server/construction_auth.go`, with its own review.

What the checks refuse, with `403` kind `remote-disabled`: direct
tailnet/LAN connections (non-loopback peer), DNS-rebinding pages and proxies
that keep the public name (non-loopback Host), and HTTP reverse proxies and
`tailscale serve` (forwarding headers). The property page, the Home tab and
the workbench then show that explanation instead of the feature. The
construction list pages carry a "Local only" note that says the same.

Host, Origin, Sec-Fetch-Site, the nonce and tailnet identity headers are
written by the caller, so none of them counts as authentication.

**Configuration.** Construction has one setting, `disabled`. A config that
sets `construction.trustedHosts` asks for remote access: Construction then
refuses to start and logs why. A key this build does not know has the same
effect. The session reports `accessModel: local-host-trust`,
`remoteAccess: unsupported` and `rawTcpForwardDetected: false`.

Inside local trust, these are defences, not authentication:

- the routes exist only on the private `Server.Handler`, never on the
  portal, share or public listeners;
- Origin and Sec-Fetch-Site refuse cross-site requests;
- every mutation needs the per-process nonce from `…/session` (CSRF);
- the actor is derived server-side (owner); request bodies and headers
  cannot assert it.

A resolver seam used only by tests sees the transport peer alone, with no
headers, cookies, URL or body. It is consulted only after the checks above,
and it can only narrow access.

```json
"construction": { "disabled": false }
```

## API (both prefixes)

`/api/properties/{slug}/construction` and `/api/home/construction`:
`GET /session`, `GET|POST /problems`, `GET /problems/{id}`,
`POST /problems/{id}/commands`, `GET /problems/{id}/history`,
`POST /problems/{id}/inputs`, `GET /problems/{id}/artifacts/{artifact}`,
assembly `GET …/assemblies/{asm}[/geometry|/glb|/validation|/history|/section|/substitution]`,
`POST …/assemblies/{asm}/commands|preview|exports`,
research `POST /problems/{id}/research-runs`, `GET …/{run}`, `GET …/{run}/events?after=`,
`POST …/{run}/start|cancel|retry|resume|questions`, `GET …/{run}/attempts/{attempt}/result`,
`GET|POST /problems/{id}/sources|evidence`, `GET /problems/{id}/evidence/paths`, `GET /problems/{id}/graph`,
`GET|POST /problems/{id}/materials|products`, `GET /problems/{id}/compare?a=&b=`,
`GET|POST /problems/{id}/decisions`, `POST /problems/{id}/agent/requests`,
`GET /problems/{id}/agent/requests/{req}`, `GET /preflight`, `GET /problems/{id}/export`.

Errors: 404 missing (no cross-project existence leaks), 403 forbidden
(kind `remote-disabled` for a request recognisably not local: a
non-loopback peer or Host, or proxy forwarding headers), 409
stale (with the current revision), 413 too large, 422 invalid, 503 capability
unavailable.

## Pending live acceptance (not provable offline)

Real 761 site conditions and measurements; the actual wall condition,
jurisdiction, climate and loads; manufacturer acceptance of real products;
an authorized live source adapter and provider preflight; a verified,
authenticated owner gateway before any remote or tailnet use (until then
remote use is unsupported, and a raw TCP forward cannot be detected);
physical-device and realistic-render quality. Fixtures prove protocol, not suitability.
