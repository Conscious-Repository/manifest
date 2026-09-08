# Deal-package assumptions and reference coverage

## Confirmed inputs

Owner confirmed existing deal assumptions: 3% rent growth, 2% operating expense
growth, 10-year horizon, 1.5% selling costs. Construction started August 1,
2026; completion target March 1, 2027; 45-day lease-up yields April 15, 2027.

Owner specified reserve defaults per residence per operating year:
1–3: $250; 4–6: $500; 7–8: $750; 9+: $1,000. Four explicit settings are added to
the canonical assumptions registry and inherited by future deal packages.
Each named lender ask can override them without changing portfolio defaults.
The legacy flat reserve default becomes $250; the new deal projection uses the
full ladder, not that flat compatibility value.

## Cap rate research

Use 8.5% as a provisional underwriting judgment for this package, not a measured
Fountain Park closed-sale cap rate. Northmarq's June 19, 2026 Q1 report reports
6.5% average metro caps, but the transaction mix favors Class B and suburban
locations. That sample is not equivalent to two-/three-unit Fountain Park rehab
collateral. Colliers' May 19 Q1 report notes mixed vacancy performance in the
city and Central West End. The existing 8.5% deal assumption is a conservative
starting point relative to those metro indicators; the precise 200bp difference
is not an empirically measured neighborhood premium.

No usable closed-sale cap-rate sample for these small Fountain Park properties
was established. The 736 reference appraisal uses sales comparison and GRM,
not an extracted cap rate. Do not label 8.5% a neighborhood market observation.
The editable ask retains the provisional status and links to the source reports.

https://www.northmarq.com/insights/insights/st-louis-multifamily-sales-activity-surges-q1-2026
https://www.colliers.com/en/research/st-louis/q1-2026-multifamily-st-louis-market-report

## Draw-planning findings

Live records at review: 751 has phase cost estimates but zero phase durations;
753/760 have phase durations but zero phase cost estimates. 748 has both.
Therefore a property-specific disbursement forecast cannot be faithfully
extracted. Show the actual phase inputs, not zeros disguised as known budgets.
Owner was asked whether to prepare a labeled draft using 751 cost proportions
and 748 durations, normalized to approved budgets. No such allocation is an
actual payment, approved draw, or certified cost-to-complete.

OCC guidance supports inspection-backed disbursements and completion tracking;
FDIC guidance compares draw requests to budgets and prior disbursements and
requires consideration of remaining completion costs. Do not invent retainage
percentages or assume expenses equal lender advances.

https://www.occ.gov/publications-and-resources/publications/comptrollers-handbook/files/commercial-real-estate-lending/pub-ch-commercial-real-estate.pdf
https://www.fdic.gov/risk-management-manual-examination-policies/construction-and-land-development-lending

## Reference coverage

| Reference section | Package implementation / remaining inputs |
| --- | --- |
| Development narrative | Deterministic property/entity/address and dated milestone fields; no generated prose |
| Property / unit mix | Live proposed unit schedules; legal count, permitted area and rent support still require evidence |
| Budget / sources and uses | Confirmed acquisition/hard/soft subtotal; construction principal and reserve; explicit additional closing-cost input (unknown until entered) |
| Operating proforma | Annual/monthly NOI-to-NCF bridge, editable vacancy/expense allowance and reserve ladder |
| Annual growth | Existing deal inputs exposed; independent rent and expense compounding |
| Multi-year cash flow | Stabilized operating projection across selected years; construction/draw/lease-up investment cash flows are not fabricated |
| Refinance | Explicit rate, amortization, LTV and cap inputs; separates NOI coverage and after-reserve NCF coverage |
| Sale/disposition | Forward-year NOI/cap-rate value and selling costs; proceeds before payoff, not net equity proceeds |
| IRR / equity multiple | Not presented without timed invested equity, debt payoff, and hold-origin confirmation |
| Diligence documents | Property-scoped files and structured evidence links, including 751 progress set |

## Editing and freshness

Manifest exposes an ask name, dates, and numeric assumption form. Portal remains
read-only. Explicit blanks are unknown, not fallback defaults. Private saves
validate ranges and whole-year/month/day inputs, reject changed projections,
and preserve unrelated source fields; source writes additionally check the
source revision under the writer lock. Pending edits pause refresh to preserve
typing. Discard reloads canonical values. Both presentations use the same
renderer and calculations. Saved package assumptions remain stable when global
settings change; live property rents and expenses continue to refresh.

## Owner-approved phase allocation

Owner authorized adding 753/760 amounts from 751 proportions while preserving
the original email totals, and scaling durations by cost share across the full
construction period. 753 hard costs including contingency total $277,000;
760 total $284,000. Each carries $15,000 soft costs separately. Completed demo
uses the recorded $12,000 / $10,500 amounts; remaining hard costs use the relative
weights of 751's exterior, rough-in, drywall, finishes and final-inspection
phases. A soft-budget phase field identifies pre-development costs without
counting them again in the hard-cost rollup. The two property source records
explicitly mark contingency included; current screening respects that flag.
Previous $2,000 closing allowances are retained as historical allocation
metadata and excluded from the confirmed subtotal, not treated as paid costs.

Phase durations are cost share × 212 days / 7; these are planning estimates,
not as-built dates. Monthly spending distributes the confirmed combined hard
and soft budgets by calendar days across Aug 1–Mar 1. Because duration is
proportional to cost, this is mathematically a constant daily spending model.
The view labels it accordingly, balances cents to the total, and presents
recorded non-acquisition expenses separately. It does not infer loan advances,
retainage, or actual funding from expenses. Existing 751/748 phase-to-budget
differences remain visible; their costs were not silently rewritten.
