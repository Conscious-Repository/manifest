# Shared deal underwriting

The owner requested a general deal-level underwriting home, previewable in
Manifest and under the corresponding deal in the OODA team portal. External
password-link publication remains separate from the existing team sign-in.

## References and decisions

Reviewed Conscious-Repository/oodagroup's deprecated DealPage.jsx, including
DashboardGrid, SummaryProforma, Sources & Uses, ConstructionTimeline and
CashFlowSection. Retain its summary → evidence → calculation structure.
Do not copy its financial defaults or automatically generated narratives.

Reviewed all five pages of the supplied February 2026 4852 proforma, including
the rendered dashboard. It establishes useful expectations: project budget,
financing sources/uses, unit mix, annual operating bridge, refinance sizing,
cash flows, sensitivity and return metrics. Its 1.21x and 1.26x DSCR examples
use different reserve treatment; explicit numerator labels avoid ambiguity.
Its 4852 assumptions are not authority for Bayard/Euclid.

Fannie Mae defines underwritten DSCR from net cash flow and annual debt service:
https://mfguide.fanniemae.com/node/3781
The interface distinguishes NOI from cash flow after replacement reserves,
and names NOI-based screening coverage separately. These are presentation
principles, not a claim that this deal meets an agency loan program.

FDIC construction-lending examination guidance evaluates construction budgets,
completion and the rationale for interest reserves:
https://www.fdic.gov/risk-management-manual-examination-policies/construction-and-land-development-lending.pdf
The view separates paid expenses, commitments, current estimates, reserve
assumptions and dated financing terms instead of adding them indiscriminately.

## Data and access

One Go projection reads current deal membership, member ledgers/sources/docs,
scoped contracts, current operating assumptions and the deal source sidecar.
Both signed-in surfaces share identical renderer and calculation assets,
with parity tests. `deal_underwriting` is the generic sidecar key; legacy
`lender_diligence` remains readable. The optional lender name describes a
financing scenario; it does not define the page or template.

GET /api/deals/{slug}/underwriting (Manifest)
GET /api/ooda/deal/{slug}/underwriting (existing OODA sign-in)

Document routes resolve only member-property files, associated contract/receipt
references and explicitly included deal references. Cross-deal paths are
rejected, and local file paths are checked after resolving symlinks.
No anonymous link or new password gate is created.

## Freshness

Visible underwriting views request the live projection every five seconds and
on tab visibility return. Content-derived ETags suppress unchanged payloads.
New expenses, membership, sources, assumptions and document inventory changes
produce a new revision. On errors, retain the last rendered data with a
visible warning. Navigation/unmount aborts requests and disposes polling.
Expanded details and scroll position survive changed-data refreshes.
This is near-real-time polling, not an instantaneous push guarantee.

## Deliberately incomplete inputs

The current dated Bayard/Euclid scenario remains available alongside live
operating projections. Owner confirmation was requested for the current
vacancy/expense assumptions and deal-specific reserves, hold period, growth
and disposition inputs. Multi-year cash flow, IRR and equity multiples remain
incomplete until those values and distribution terms are established. No
reference-PDF defaults, market rents or loan approval have been invented.
