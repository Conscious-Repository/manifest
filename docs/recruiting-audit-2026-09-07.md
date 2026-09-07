# Recruiting review audit and factual-summary plan

2026-09-07 · Manifest / private Aion recruiting

## Objective and owner decisions

Make search results and candidate backgrounds easier to digest across People,
Sources, Places, Network and role workspaces. Benjamin explicitly chose **factual
summaries now; define criteria later**. Existing human scoring remains available;
this pass does not introduce AI fit scores, ranking, rejection, or outreach.

The first implementation is local UI work over existing records. DeepSeek
integration is a proposed next phase, not a shipped candidate summarizer.

BuildTall values carried forward: portable evidence, rebuildable projections,
small composable changes, no duplicate source of truth, direct human control, and
no new framework or scheduler. The prior September 5 source-enrichment decisions
remain relevant: people outlast a search, passing is contextual, topics need
attributable evidence, and a missing network path means limited coverage.

## Observed usage and all-view audit

Read-only live snapshot: 29 search runs, 129 pending results; 75 candidate records
(40 inbound awaiting triage, six new, one reviewing, 28 archived); four roles;
one saved lab; two network people and 91 edges. Source runs: 21 PubMed, four
OpenAlex, three web, one manual. Counts describe this snapshot, not fixed UI data.

| Surface | Observed friction | Change / next step |
|---|---|---|
| Sources | Search history is the default work unit; 129 people hidden inside run disclosures. Raw bibliographic metadata repeats paper titles. | Default review queue, search across backgrounds and searches, role/status filters, 20-result disclosure, separate search history. |
| Result cards | Initials and a paper title can look like a complete candidate profile; accepting has an ambiguous destination. | Readable source excerpts or labeled publications, source links, explicit missing profile fields, “Add to People.” Keep citations, lookup, contextual pass/undo and session-only later. |
| People: considering | All inbound records look broadly alike; mouse-only rows; repeated return to list to review another person. | Keyboard-operable review rows, evidence/resume coverage, previous/next within the visible queue. |
| Candidate inspector | Edit fields lead; original resume is a narrow wall; inspector disappears at tablet widths. | Fold profile editing, use available source excerpts, widen reading pane, outline recognized resume sections without generating facts; make tablet inspector accessible. |
| People: who I'd ask / everyone | Candidate, connector, and origin filters do not consistently describe what is visible. | Audit all three facets and ensure candidate origin filters do not hide candidates on “everyone”; show its connector subset with search. Preserve owner-asserted relationships. |
| Sources / Places / roles | An unrelated selected candidate remains open, taking width and offering consequential actions out of context. | Only mount the candidate inspector in People when the selected candidate is visible. |
| Places | “Sweep” starts work but there is little route back to the existing results. | Show last matching search and link to results; match explicit source URL/name, never fuzzy identity. |
| Role workspace | Saved searches show original “new” counts after decisions; only rerun is offered. | Derive pending counts from current draft status and link directly to results. Existing criteria are unchanged. |
| Network | “91 edges” suggests a rich graph but the selected centre has no reachable graph. | Explain the scope of recorded connections and give a route back to candidate review. Preserve the owner's graph controls and tuned layout. |
| Outreach / Ashby | Scoring, triage, resume, provider state and send gates coexist in a crowded inspector. | Keep current consequential actions and guards; improve reading/navigation around them. No send/reject or Ashby mutation was exercised during audit. |

Additional data-quality findings: several imported organization fields are weak
(e.g. a platform name rather than an employer). PubMed drafts can carry abbreviated
names and no affiliation. One cached search labeled as a nonexistent query has
results. These are reasons to test ingestion and identity quality before adding
fluent biographies; they do not establish that any particular person is invalid.
No current candidate record was corrected or discarded during this audit.

## Research applied

- [Ashby AI-assisted review](https://docs.ashbyhq.com/ai-assisted-application-review)
  separates explicit criteria and explanations and provides an undecided state.
  The useful pattern here is inspectable evidence and uncertainty. Role evaluation
  is deferred by the owner's choice.
- [Greenhouse structured hiring](https://support.greenhouse.io/hc/en-us/articles/360039539772-Structured-hiring-guide)
  separates defining requirements from evaluating people. Accordingly, summaries
  describe backgrounds consistently without inventing missing role requirements.
- [PubMed help](https://pubmed.ncbi.nlm.nih.gov/help/) documents author identifiers
  and name disambiguation. An exact abbreviated-name string is insufficient proof
  that two sources describe one person. Preserve source-local identities until a
  canonical identifier or explicit reviewed linkage supports combining them.
- [vLLM structured outputs](https://docs.vllm.ai/en/latest/features/structured_outputs/)
  constrain output shape. They do not establish factual truth; independent source
  validation is still required.
- [vLLM reproducibility](https://docs.vllm.ai/en/latest/usage/reproducibility/)
  treats reproducibility as a serving configuration concern. Pinning a prompt,
  seed, model and temperature is not a universal determinism guarantee. Cache an
  accepted result against exact input/version hashes to make the product stable.
- [NIST Generative AI Profile](https://nvlpubs.nist.gov/nistpubs/ai/NIST.AI.600-1.pdf)
  identifies confabulation and the need for evaluation. This informs claim-level
  factuality checks and human audit rather than a self-reported confidence badge.

## Proposed candidate brief

Consistent reading order, approximately 100–160 words when there is enough data:

1. **Background:** documented position/affiliation with dates and attribution;
   “not recorded” where the source does not establish it.
2. **Work:** up to three concrete projects, publications or responsibilities.
   Keep the person's stated contribution distinct from what the team/paper did.
3. **Experience / education:** short dated entries; preserve uncertain dates.
4. **Sources and gaps:** numbered citations, retrieval dates, disputed or missing
   facts. Source completeness describes our information, not candidate quality.

A sparse PubMed result should read “Listed author on [paper], [journal/year].
Current role, affiliation and location not established by this source.” It must
not become “MRI expert” or “experienced device engineer” because of keywords.
A CV claim remains “resume reports”; corroboration requires another attributable
source. Don't infer employment from affiliations or availability from location.

Show full sources on demand, a correction/flag action, and “generated from these
sources” with the model/version. Do not display a universal candidate score.

## Local DeepSeek feasibility checked

Read-only discovery on Metis returned several models, including
`deepseek-v4-flash-0731`, with a reported 1,048,576-token context. This is reported
capacity, not a recommendation to send enormous prompts. No serving-stack changes.

Two small fictional extraction requests using this model, temperature 0, seed 42,
512 output-token cap and JSON Schema returned the same valid JSON: a documented
job, organization, and null location. They completed in about 2.2 and 2.4 seconds.
The server reported different completion-token counts (102/103), so this is a
schema/transport smoke check, not proof of identical inference or real-CV quality.
Only fictional data was used; no candidate corpus was sent for inference.

## Implementation plan: deterministic processing around bounded inference

### 1. Freeze an evidence packet

Use existing private recruiting records, run-cache drafts and recruiting-scoped
resume artifacts. Give each source a stable ID, content hash, retrieval time,
origin URL/artifact reference and exact text. Preserve page/section offsets where
available. Flag empty extraction, truncation, OCR uncertainty and conflicting
identity before summarizing. Candidate text is untrusted data, never instructions.

No public Aion export, no copied credentials/contact lists, no tool access in the
summarization call. Use the existing private worker/job conventions and current
model endpoint configuration. A draft summary belongs in the existing run cache;
a durable candidate summary is a private, rebuildable artifact reference, not an
independent candidate database. Its loss must not lose source evidence or decisions.

### 2. Extract atomic facts into a versioned schema

JSON structure: `subject_ref`, `source_hashes`, `claims[]`, `gaps[]`,
`conflicts[]`, `model_id`, `prompt_version`, `schema_version`.

Each claim: stable local ID, allowed kind (position, affiliation, project,
publication, education, stated skill), concise text, date text as given,
`source_id`, exact quote and offsets, attribution (`resume_reported`,
`source_reported`, `owner_confirmed`). Do not ask the model for a probability.
Unknown dates/locations are null. Disallow extra keys, HTML and generated links.
A URL is resolved from the source registry, never trusted from model prose.

Use a pinned deployment/model revision (aliases alone are not version pinning),
fixed prompt/schema, temperature 0 and fixed seed. Start at one worker with a
bounded queue, input/output limits, deadline and one schema-repair attempt.
Discover and record supported serving settings; the synthetic check used
`chat_template_kwargs.enable_thinking=false`, but actual behavior needs evaluation.

### 3. Validate, then render a fixed template

Deterministic checks: schema, lengths, source ownership, hashes, quote/offset
matches, exact names/numbers/dates, duplicate claims, missing citations, source
identity linkage, and stale input. Reject invented source IDs, links and numbers.
Quote existence alone does not prove that the paraphrase follows from it.

Use a second bounded claim-verification pass for entailment, attribution,
negation, tense and identity conflicts. Same-model agreement is a useful filter,
not independent truth. Contradictions or unsupported paraphrases cause abstention
or literal-source fallback; one retry maximum, never a loop until agreement.

Render validated atomic claims with a deterministic template. A cache key includes
source hashes, parser version, model/deployment version, prompt and schema hashes.
The same key returns the accepted cached output; source/model/prompt changes make
it stale. Existing verified text remains available and clearly dated while a new
version is pending. Failed generation never becomes an empty candidate record.

### 4. Benchmark before enabling automatic display

Proposed thresholds, **not achieved results**:

| Check | Initial acceptance target |
|---|---|
| Displayed output passes schema/source/hash/quote checks | 100%; anything else withheld |
| Invented identity, employer, degree, numeric/date claim; changed negation | Zero observed critical errors on held-out cases; any failure blocks rollout |
| Factual claim precision | At least 99% observed; 95% confidence lower bound at least 98% |
| Recall of explicitly supported key facts | At least 95% on human-labeled packets; report by source type |
| Missing / ambiguous information | 100% of ambiguity test cases abstain instead of filling gaps |
| Repeat extraction | At least 99% agreement on normalized fact sets across three trials; separately require 100% cache-repeat equality |
| User outcome | Median review time at least 25% lower with no increase in factual corrections on the pilot |
| Responsiveness | Proposed p95 under 20 seconds for a bounded packet; measure under normal lab contention |

Build a private benchmark of roughly 100 packets / at least 1,000 claims, mixing
resumes, bibliographic-only results, researcher profiles, web snippets, empty/OCR
extracts, duplicate names and conflicting dates. Split tuning and hold-out by
person (not source fragment), record annotation rules and adjudicate disagreements.
Report metrics by source type; bootstrap precision by candidate to account for
correlated claims. A small sample that cannot support the confidence bound is
insufficient evidence, even if it has zero errors. Include source instructions
trying to alter the schema and an irrelevant-query negative control.

Start shadow-only on a small batch; review errors before enlarging the batch.
“Basically free” inference removes price friction but not shared capacity,
latency, evidence quality or the cost of plausible mistakes. Re-run the hold-out
checks on every model/prompt/parser change.

### 5. Integrate after the pilot passes

A private “Prepare background” action first; then optional automatic processing
when a search completes or a resume/source hash changes, using existing job
mechanisms. Display states: not prepared, preparing, source-linked brief, needs
review, stale, failed with retry. Keep raw evidence immediately usable in all
states. Bounded concurrency and cancellation; no periodic re-summarization of
unchanged packets.

Add validated brief fields to the same result-card and candidate-inspector
renderer. No automatic change to candidate stage, manual fit scores, approval
status or outreach. Corrections remain owner annotations; regenerate derived
text without overwriting them. Criteria design is a separate later decision.

## Validation and delivery

Verified: `go test ./...` passed, followed by final targeted server checks;
`node --test tools/tests/recruiting-review.test.cjs` passed five tests; modified JS
syntax and `git diff --check` passed. Browser checks covered Sources queue/history,
search and role no-match, Later/bring-back, People/Everyone, candidate previous/next,
resume outline, Places results, role saved-search counts, Network empty context,
desktop light/Jarvis, 1000px tablet reading and 390px phone sheet/navigation. The
read-only preview blocked an existing automatic resume pull; it did not synchronize
Ashby. This also exposed a raw HTML error message, now replaced by an HTTP status
message for non-JSON provider failures.

The owner authorized commit and deployment on 2026-09-07, and established
commit/push/deploy/live verification as the default delivery flow. The preview is at
`http://127.0.0.1:8769/#/aion/recruiting/sources` and rejects write requests. This document distinguishes the UI
implementation from the unimplemented model workflow. No candidate decisions,
provider synchronization, outbound mail or recruiting data writes were requested
or performed as part of verification.
