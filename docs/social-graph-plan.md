# Social graph plan — one graph, lenses, the graph as the home view (revised)

Revised 2026-09-18 after a code check, a values check and a research pass;
approved by Benjamin the same day. Planning only — nothing here is built.
Phases start on his word, in order.

## Context

The draft's intent stands unchanged: *a private graph of people I might want
to reach, coloured by what I am doing about each of them, that answers "who
do I know who can get me to X" — recruiting first, fundraising and selling
next — without letting inbound candidates and swept strangers leak into my
contacts.*

Three things prompted this revision:

1. **Five places where the code disagrees with the draft** (bridge people's
   storage tier; source nodes with no edges to draw; edge dedupe that throws
   away the second shared paper; whole-graph mode with no bound; two state
   machines on one human). Each is a decision, settled below.
2. **A values check** against what the codebase already commits to
   (ARCHITECTURE §2 state tiers, §4 write boundary, §9 identity firewall;
   D12 no LinkedIn adapter; D15 no adapter writes contact details; "a run
   is a cache, never a record"; the 2026-09-07 audit's "deterministic
   processing around bounded inference"). One item in the draft — the
   LinkedIn browser capture — fails it and is deferred.
3. **Research** on the four questions the draft answered from instinct:
   what a co-authorship tie is worth, what makes an intro path trustworthy,
   what the recruiting-privacy norm is for people who never applied, and
   where LinkedIn's terms actually draw the line.

## Values check (what the plan must not contradict)

| Commitment | Where it is written | Effect on this plan |
|---|---|---|
| Derived/rebuildable state lives in `dataDir`; the vault holds records; wrong tier = bug | ARCHITECTURE §2 | Bridge people are **cache, not vault rows** (D-F). The `Into the graph` outcome shipped 2026-09-11, which writes `network/people.md` rows for swept strangers, is superseded. |
| Every vault write is a user action or an approved proposal, through `vaultwriter` | ARCHITECTURE §4 | Drawing never writes. "Add to contacts" and "pursue" are the only writes; sweeps write cache. |
| Contacts firewalled from business records; explicit link only, nothing infers unification | ARCHITECTURE §9 | Contact ↔ graph person is `ref:` and nothing else (D-A holds). Enrichment never writes into a note. |
| No adapter reads LinkedIn (D12); no adapter ever writes an email or phone (D15) | `recruiting/sources/*`, `SanitizeDraft` | Pasted names carry no contact details; LinkedIn capture deferred (D-E). |
| A run is a cache of a search, never a record; a pass is a search-time suppression that is remembered forever (`passed.md`) | `recruiting/runs.go`, `passed.go`, ui-conventions "Deleting a run (2026-09-10)" | §8 lifetime rule is the same rule drawn: bridge people live and die with their source's cache. |
| Inference is bounded: deterministic first, model only over cited evidence, claims carry citations, no self-reported confidence badge | `docs/recruiting-audit-2026-09-07.md`, `lookup.go` | §5 enrichment contract stands; add: cached against input hash, never on sweep. |
| Every outbound message is an approval with a receipt, from a named mailbox | `docs/email-approvals.md` | §6 rejection mail goes through `email.prepare`; Ashby is never a sender. |
| An absence shows only when it changes the call; delete cheap things, archive people | `docs/ui-conventions.md` | Bridge people are cheap (cache); touched people are archived, never deleted. |

## Research applied

- **Co-authorship weight.** Bibliometrics settled this: *fractional counting*
  gives each paper total weight 1 spread over its author pairs, so a
  two-author paper is a strong tie and a 40-author consortium paper is
  nearly nothing (Newman's 1/(n−1); VOSviewer offers both and the results
  differ sharply). Adopt fractional counting per work, summed over shared
  works, recency-decayed. Full counting (weight 1 per paper) is what the
  draft implied and is the wrong instrument for intro paths.
  [Perianes-Rodriguez, Waltman & van Eck 2016](https://www.sciencedirect.com/science/article/abs/pii/S1751157716302036) ·
  [Newman 2001](https://www.researchgate.net/publication/11881413_Scientific_Collaboration_Networks_II_Shortest_Paths_Weighted_Networks_and_Centrality)
- **What makes an intro path trustworthy.** Relationship-intelligence tools
  (Affinity, Introhive, 4Degrees) score a tie on **frequency, recency and
  reciprocity**, and *decay* it — "a connection that was strong two years
  ago and silent since isn't a connection you want carrying your
  introduction". Reciprocity is the underrated signal: forty outbound emails
  with two replies is a weak tie. Our derived edges already have recency
  (`same_meeting` is dated) and the calendar/transcript pull can give
  reciprocity. Path ranking should weight the *connector's* tie to you by
  these, not only the far edge's confidence.
  [Affinity — which relationships drive warm intros](https://www.affinity.co/blog/which-relationships-drive-warm-introductions) ·
  [Introhive](https://www.introhive.com/blog-posts/relationship-intelligence-automation/)
- **People who never applied.** The recruiting-privacy norm (GDPR Art. 14 for
  EU subjects; ICO guidance; the ICO's Nov-2024 audit of AI recruiting tools)
  is: building a database of sourced people "in case" is not a lawful basis;
  notify within a month or do not keep; retain unsuccessful/passive people
  6–12 months absent consent. The answer that fits this codebase without a
  notice step is structural: bridge people are a **cache**, carry public
  professional identifiers only (name, affiliation, ORCID/OpenAlex/GitHub,
  works — never email or phone, which D15 already guarantees), fade when
  stale and vanish when their source is archived. Nobody is contacted until
  you *pursue* them, and pursuing is what creates a record and a human
  touch. (Owner's choice 2026-09-18: cache-only + stale fade, no notice.)
  [ICO draft guidance](https://trilateralresearch.com/data-protection/ico-issues-draft-guidance-on-employment-records-and-the-recruitment-and-selection-process) ·
  [Workable on legitimate interest](https://resources.workable.com/tutorial/how-to-approach-gdpr-legitimate-interest-in-recruiting)
- **LinkedIn.** The User Agreement prohibits copying profile data "through any
  means, including crawlers, browser plugins and add-ons, and manual work";
  the 2022 hiQ judgment held that clause enforceable as a contract term (the
  earlier CFAA ruling only said scraping public pages is not a federal
  crime). D12 was therefore not merely practical. A bookmarklet is a "browser
  add-on" by the agreement's own words. (Owner's choice 2026-09-18: defer;
  decide after phases 1–3.)
  [hiQ v. LinkedIn (IAPP)](https://iapp.org/news/a/data-scraping-and-the-implications-of-the-latest-linkedin-hiq-court-ruling) ·
  [Morgan Lewis summary](https://www.morganlewis.com/blogs/sourcingatmorganlewis/2022/12/linkedin-v-hiq-landmark-data-scraping-suit-provides-guidance-to-data-scrapers-and-web-operators)
- **Graphs at scale** (from the 2026-09-05 pass, unchanged): van Ham & Perer
  "search, show context, expand on demand"; Kumu degree-bounded focus;
  Linkurious supernode guardrails; the Obsidian global-graph critique
  ("wallpaper after the first week"). Whole mode must be bounded by
  construction, as ego mode is.

## Decisions

### D-A. One graph, two tiers of person, lenses on top — unchanged

Contact (vault note) and graph person (system side). `NetworkPerson.Ref`
already links them; one node drawn. A lens = a filter + a colour meaning + a
set of panel actions, nothing more. Component at the top-level Network tab;
Recruiting opens it with the lens preset.

### D-B. Status — stored only for people you have touched

The draft: an owner-set `status` on *every* graph person. The code: swept
strangers are edge endpoints (`ext/orcid/…`, `contact/…`) with no row to
hold a field, and under D-F bridge people are cache. So:

| Status | Stored? | How it is known |
|---|---|---|
| `in_touch` | no | tier: a contact, or a network row with `consent: owner` |
| `pursuing` | **projected from the candidate record** | any active candidate (`stage` ≠ archived); role optional |
| `bridge` | no | tier: present in a source's run cache, not a record |
| `passed` | yes (`passed.md`) | tombstone; also marks the ATS-archived |

One derivation direction (D-H): for a candidate, status is a projection of
`stage`; it is never stored a second time. This closes the one-fact-two-
spellings class before it opens.

### D-C. Fill = status, source hue = links and rings — unchanged, with a prerequisite

Requires D-J (membership edges), because today no edge joins a person to a
source node.

### D-D. Dropped sources land people straight in the graph — unchanged in effect, corrected in mechanism

A run writes its people and edges **into its own cache** (dataDir), and the
graph reads cache ∪ records. There is no queue to clear; the Sources view
becomes a list of sources with their coverage. Promote to `pursuing` →
`AcceptDraft` creates the candidate record and repoints the person's
external keys onto it (`edges_identity.go`, unchanged). `passed.md` keeps
suppressing at search time (`runs.go` dedupe loop, unchanged).

### D-E. Company sources — as drafted, minus the browser capture

Build order for biotech/deep tech: **patents (PatentsView/Google Patents,
`coinventor` exists) → ClinicalTrials.gov → OpenAlex institution sweep →
GitHub org members → Wikidata P108/P1416 → SEC EDGAR**. Company site crawl
exists (`sources_web.go`); extend to press pages. **Pasted names** are a
first-class intake: you type or paste the people you choose, recorded as
`consent: owner_import`, no provenance claimed beyond "the owner typed it",
no contact details (D15). Former employees drawn *former* from affiliation
dates (`EvidenceAffiliation` already carries them — a rendering rule, not
new sourcing). Coverage per company on the source row. No licensed
databases; no enrichment vendors.

**LinkedIn: deferred** (owner, 2026-09-18). Not in phases 1–3. Revisit with
the coverage numbers in hand; if it returns, it returns as a values decision
with the User Agreement quoted, not as a practicality.

### D-F. Bridge people are cache, not records — NEW

`network/people.md` holds only people you have touched: `consent: owner`
connectors and, after this plan, nothing written by a sweep. Bridge people
are the run cache drawn: id, label, affiliation, external keys, edges, all
re-derived on re-sweep. Archive the source and they go, unless an edge ties
them to someone `in_touch`/`pursuing`, in which case they stay as that
person's bridge. No timer; the node fades with its source's age. This is §8
of the draft made consistent with ARCHITECTURE §2 — and it is also the
data-minimisation answer: nothing is kept "in case".

Consequence: the `Into the graph` draft outcome (`Store.GraphDraft`,
`POST /sources/graph/{run}/{draft}`, 2026-09-11) is retired in phase 3. Its
one shipped row (David C. Garrett) migrates to the cache tier or is promoted
by the owner.

### D-G. Whole mode is bounded — NEW

Ego mode keeps `graphRingCap`/`graphMaxNodes`/≤3 hops. Whole mode is bounded
by the **lens × status × source filters**, with the omitted count shown the
way ego mode shows it, and a hard ceiling (240 nodes) that ranks candidates
and connectors before bridge people before strangers when it cuts. Default
for recruiting is **ego mode with "show Ashby" on** — strays are surfaced by
the filter, not by drawing everything.

### D-H. One state machine per human — NEW

Candidates have `stage`; status is derived from it (D-B). Non-candidates
have tier. Nothing stores status twice.

### D-I. Edges accumulate; weight is fractional and decays — NEW

`Edge.Evidence` becomes a list of work refs (DOI/OpenAlex/PubMed/patent id,
each with its year and author count) and `edgeKey` **merges** a repeated
claim instead of refusing it. Weight = Σ over shared works of 1/(n−1) ·
recency decay. Same for `coinventor` and `same_grant`. Path ranking uses
edge weight and, for the first hop, the connector's tie to you (recency,
frequency, reciprocity from calendar/transcripts). `MinPathConfidence` stays
as the floor.

### D-J. Membership edges — NEW

A run emits `member_of` edges (person → source node, carrying the seed id,
the affiliation date range and `inferred: false` when the source states
it). Source nodes are projected into `graph/` as `org`/`paper`/`repo`
entities (kinds exist). Without this, source nodes have nothing to draw
links from and D-C cannot render.

## What already exists (reuse, do not rebuild)

- Seed classes, intake cascade, scaffold: `recruiting/model.go`,
  `intake.go`, `intake_refine.go`.
- Adapters + edge kinds: `recruiting/sources/` (`source.go` declares kinds).
- Edge claims, identity seam, repoint on accept: `recruiting/model.go`
  `Edge`, `edges_identity.go` (`extIndex`, `repointEdges`, `ExtKeyFromURL`).
- Derived edges (calendar, notes), owner node, labels: `server/recruiting_people.go`
  (`personIndex`, `coAttendanceEdges`, `coMentionEdges`).
- Ego graph API + client: `server/recruiting_graph.go`,
  `server/web/js/97-rec-graph.js`, `css/97-rec-graph.css` (force sim,
  floating controls, persisted forces, fit, right-click menu).
- Paths: `recruiting/network_paths.go` (`DerivePaths`, `PathEdges`, `OwnerSeeds`).
- Lookup (exact-match, add-never-overwrite): `recruiting/lookup.go`.
- Passed tombstones + search-time suppression: `recruiting/passed.go`, `runs.go`.
- Ashby: `recruiting/ashby.go` (`SyncBack`, `ChangeStage`, `ArchiveReasons`,
  archived counts as of 2026-09-11), `server/recruiting_ashby*.go`.
- Contacts read: `vaultindex.PeopleNotes/PeopleByEmail/CoMentions`,
  `contacts/service.go` (`PastMeetingParties`).
- Email approvals: `server/email.go` (`email.prepare`), `docs/email-approvals.md`.
- General graph: `graph/` (entity kinds incl. org/paper/repo; `ties.go`, `knowledge.go`).

## Phases

Each ships alone and leaves the surface working.

**1. Model (D-B, D-F, D-H, D-I, D-J).**
`recruiting/model.go` (Edge evidence list + weight; `member_of` kind in
`sources/source.go`), `edges_identity.go` (merge on `edgeKey`), `runs.go`
(run cache carries people + edges as a drawable projection; `Execute` emits
`member_of`), `store.go` (`NetworkEdges` reads records ∪ run caches; status
projection helper), `network_paths.go` (weighted ranking; connector-tie
term). Retire `GraphDraft`/`Graph` route (keep tombstones). Tests: round-trip
of the evidence list; a second shared paper raises weight instead of being
refused; a 40-author paper ranks below a 2-author one; a bridge person
disappears when their source is deleted and survives when tied to someone
pursuing; the vault stays byte-identical across a sweep.

**2. Graph as home (D-C, D-G).**
`server/recruiting_graph.go` (whole mode with lens/status/source filters and
the ranked cut; source nodes and `member_of` in the reply), `97-rec-graph.js`
+ css (mode switch, filter checkboxes in the floating card, fill-by-status
with source hue on links/rings and a `colour by` flip, profile panel on
click, right-click: set status / associate role / add to contacts / paths /
enrich / pass), rail reorder in `96-aion-recruiting.js` (Graph · Places ·
Triage). Ego mode kept. Tests: bounded whole mode; a graphed stranger is
`bridge`; contacts drawn as `in_touch` and never written.

**3. Direct landing + Places (D-D, D-E first four sources).**
Sweeps land in cache immediately; Places = intake box on top, one row per
source with hue, member count, current/former split, last sweep, cadence
toggle; runs collapsed under their source. New adapters: patents,
ClinicalTrials.gov, OpenAlex institution sweep, GitHub org; pasted-names
intake (`consent: owner_import`). Tests per adapter: caps (the 15-author /
8-PI rule), durable ext keys, no contact field ever written (D15), coverage
numbers on the row.

**4. Enrich (§5 as drafted, plus the audit's contract).**
Exact-match lookup first; then a bounded model pass over cited evidence
only (the audit's evidence-packet → atomic facts → validated template
pipeline; DeepSeek local, temperature 0, cached against the input hash);
proposed links become identity only on a click; 2–3 sentence dated summary
with history; contacts get a system-side sidecar, never a note write.
Per-click, or batch over `pursuing` only.

**5. Ashby + Triage (§6 as drafted).**
Inbound drawn by employer org node; the triage card; reject = one click that
is the approval of the rendered `email.prepare` draft from ben@aion.bio +
`ChangeStage` to Archived with the reason; one editable boilerplate record
with `{first_name}`/`{role_title}`; unified seen-before check by external
id + name key across `passed.md` and Ashby archives, surfaced as a flag.
Already answered: Ashby's stage change sends no mail.

**6. Overlay and lenses (§7 phase 6 as drafted).**
Contacts tier drawn from `vaultindex` (person notes as `in_touch`, wikilinks
as `owner_said` "vault link", calendar/transcript overlap as the derived
edges that exist); one write path, "add to contacts", through `vaultwriter`
+ approvals. Fundraising lens over `fundraising/`. Path weighting tuned here.

**Deferred:** LinkedIn (any form), Wikidata and SEC adapters (after 1–3 show
coverage), a 12-month source TTL (not chosen; revisit if the cache grows
past what stale-fade keeps legible).

## Verification

- `go build ./... && go test ./recruiting/... ./server/... -count=1`
  (`TestCorpusGoals` fails on main already; the `chat_shared_plans` tests
  belong to another in-flight change — neither is this plan's).
- Phase 1: the vault under `system/aion/recruiting/` is byte-identical
  before and after a sweep (the `snapshot`/`assertOnlyChanged` idiom in
  `runs_test.go`); `network/people.md` gains no row from any adapter.
- Phase 2: CDP against a scratch vault seeded with two labs and one paper —
  whole mode stays under the ceiling and reports the omitted count; toggling
  `bridge` off removes exactly the cache-tier nodes; a source node's links
  carry its hue; click opens the panel without leaving the graph.
- Phase 3: sweep a company through each adapter against a recorded fixture;
  assert the coverage line, the current/former split, and D15.
- Phase 5: the rendered rejection draft on the card equals the approval's
  envelope byte-for-byte; the receipt is in the ledger before Ashby is
  called; a reapplicant is flagged, not dropped.
- Live smoke after each phase on metis, as the 2026-09 passes did.

## Status (stamped as phases ship)

- **Phase 1 — model.** SHIPPED c03f4ed + c1f0ba0 (2026-09-18), live.
- **Phase 2 — graph as home.** SHIPPED 93c2bed + d94e895 (2026-09-18), live.
  Finding while verifying: ego mode from the owner was empty because the
  Google calendar token had expired (`invalid_grant`), not because of the
  graph — the reply now names it (68a8dc2). Reconnect is the owner's.
- **Phase 3 — direct landing + Places.** SHIPPED 78e5dc6 + ab73c15
  (2026-09-18). Adapters: PatentsView (key-gated on `PATENTSVIEW_API_KEY`;
  not yet set on metis), ClinicalTrials.gov, OpenAlex institution sweep,
  GitHub org members; pasted names as `owner_import`; place cadence (marks
  due, never fires). Sources tab folded into Places.
- Phases 4–6: not started.
