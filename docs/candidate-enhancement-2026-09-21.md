# Candidate enhancement and focused review

Owner request: 2026-09-21. Implement within the private recruiting source queue.

## Research and decisions

The supplied screenshots show the existing source queue and Ashby's single-person
review with adjacent navigation and decisions. Reuse Manifest's source cards,
quiet controls, evidence disclosures and responsive layout, not Ashby's styling.
ARCHITECTURE.md and docs/ui-conventions.md govern this change; this checkout has
no AGENTS.md. Existing lookup calls OpenAlex, ORCID, GitHub and PubMed, then uses
DeepSeek only for missing profile fields/topics. It neither reads candidate pages
in depth nor produces a recruiter brief. Existing web transport already enforces
public-address, redirect, robots, content-type and body-size boundaries.

Primary references consulted:
- https://api-docs.deepseek.com/guides/json_mode/ — JSON output still needs output
  validation and sufficient output budget; do not equate JSON with factuality.
- https://help.openalex.org/data/authors/ — durable author IDs and ORCID support
  stronger identity than a name search; shared names remain ambiguous.

Use the existing configured lab endpoint, not a new paid service. Temperature zero
reduces variation but does not guarantee identical model output. The retrieval
order, URL budget, merge rules and claim validation are deterministic.

## Implementation plan

1. Extend the additive draft JSON contract with a separate generated brief:
   overview, experience, education, public work/posts, questions and gaps. Every
   substantive item includes a source URL and an exact supporting quote. Record
   model, timestamp, evidence snapshot and partial-source failures. Old runs work.
2. Keep the public-index order fixed. Refuse ambiguous multiple exact-name hits;
   preserve existing values. Read a bounded set of known candidate profile URLs
   and relevant same-host links, using the existing guarded web transport. Require
   the candidate's full name on fetched pages; do not scrape protected social
   platforms. Display supplied social links for manual inspection. A missing
   education/work history is a gap, never an invented CV.
3. Run DeepSeek after evidence gathering even when topic chips already exist.
   Require supported claims; treat fetched text as untrusted. Keep generated
   prose separate from verbatim citations and human-authored notes. An unavailable
   endpoint leaves collected evidence usable and reports incomplete enhancement.
4. Rename the source-card action to Enhance. Add Review all candidates → and a
   focused review surface over the currently filtered queue, with stable identity,
   previous/next and existing Add to People, Pass and Later actions. Show full
   brief, profiles and all evidence. Decisions remain explicit and single-person;
   enhancement never sends messages or changes candidate stage.
5. Verify malformed/unsupported claims, bounded fetch and identity checks,
   deterministic ordering, partial failures, persistence, and review navigation.
   Run relevant Go/Node checks and the repository suite; inspect responsive UI
   where browser tooling is available. Commit only intended files, push main via
   SSH, deploy using the standing delivery flow and verify served assets.

## Boundaries

This is evidence-backed research assistance, not an automated hiring decision or
an exhaustive internet/background check. No sensitive-trait inference, personality
scores or auto-rejection. Sources may be unavailable, stale, ambiguous or sparse;
unknowns remain visible. Source evidence survives acceptance through the existing
record writer; the generated brief remains a derived run-cache artifact under
existing retention rules. Existing saved People/ATS review remains available.

## Implemented behavior and verification

Enhance gathers indexes in fixed order, then reads at most five known/profile
pages (20-second web budget) before inference. Index calls have individual
12-second budgets. Namesake collisions remain unresolved. DeepSeek receives at
most 24 evidence rows and 24,000 snippet characters, emits at most three items per
brief section, and uses the configured model at temperature zero. Quoted support
and evidence indexes are validated; this validates provenance, not the semantic
truth of every generated sentence. The UI labels interpretation explicitly.
A bounded four-minute model budget accommodates the lab endpoint's latency;
the queue remains usable during inference. Concurrent decisions invalidate stale
results instead of being overwritten. Failure and ambiguity reports persist.

The focused page follows the current source filters, preserves selection by
run/draft identity, advances after a decision/Later, retains previous/next keyboard
focus, and displays profile links, source quotes, model input, gaps and follow-up
questions. Existing People/ATS review is unchanged. No candidate was accepted,
passed, contacted or moved in the live store during verification.

Verified before delivery:
- `go test ./...` passed; relevant recruiting/server tests rerun after changes.
- `go test -race ./recruiting/...` passed, including concurrent decision coverage.
- Tests cover unsupported/fabricated citations, input bounds, multiple namesakes,
  exact-name page attribution, URL/robots/social restrictions, persistence, and
  enhancement despite already having topic chips.
- Node selection and failure-message checks passed.
- Playwright fixture checks passed for navigation, keyboard focus, Later,
  supporting-quote disclosure, and no horizontal overflow at 1280/1000/390px in
  light and Jarvis themes; desktop and phone screenshots visually inspected.
  Reproduce with `NODE_PATH=<playwright installation>/node_modules node
  server/testdata/recruiting-focused-review-browser.cjs`.
- A real configured DeepSeek call with a fictional candidate produced five
  supported items spanning overview, experience, education and public work.
  Earlier 45/90-second attempts timed out; a minimal completion succeeded and the
  longer bounded request succeeded. Endpoint availability is distinct from
  inference completion.

Limits: this uses the existing indexes and a small traversal from known URLs,
not an unrestricted general-web search. Pages must explicitly identify the
candidate; initials, absent personal pages, protected social platforms and PDFs
can leave substantial gaps. Social destinations remain inspectable links.
Temperature zero does not make inference byte-reproducible. Generated briefs
remain in the source-run cache under its normal retention policy; accepted
records retain the underlying evidence. Commit/push/deployment outcomes are
recorded in the work order's durable result rather than predicted here.
