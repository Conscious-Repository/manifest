# Rehab Quartet — email-to-live reconciliation

Audit date: September 8, 2026. Source: owner-supplied “OODA Group - Check In” email thread, August 21–September 8. Read-only audit of live private underwriting API, underlying property/source records, shared renderer, and authenticated OODA portal. No budgets, transactions, or assumptions changed by this audit.

## Conclusion

The named lender ask matches the final communicated development budgets, opening rents/unit mix, corrected construction principal/reserve, equity totals, and financing terms. **51 numerical comparisons passed.** This is not an all-records consistency pass: property budgets and older generic underwriting fields still differ, and several email conditions are stored but absent from the presentation.

The September 4 arithmetic correction, expressly accepted by Benjamin, supersedes September 3's $823,040 request and $48,420 reserve. September 8's lender preference is individual notes and deeds of trust with individual releases; the revolver was deferred. Neither correspondence nor the portal is a loan commitment.

## Exact capital reconciliation

All dollars. Soft costs remain approximate allowances in the email. Hard costs include the confirmed 20% contingency once.

| Property | Acquisition | Hard incl. contingency | Soft allowance | Development total | Base loan | 12-month reserve | Loan incl. reserve | Equity |
| --- | ---: | ---: | ---: | ---: | ---: | ---: | ---: | ---: |
| 751 bayard | $18,000.00 | $256,000.00 | $15,000.00 | $289,000.00 | $202,300.00 | $12,643.75 | $214,943.75 | $86,700.00 |
| 753 bayard | $18,000.00 | $277,000.00 | $15,000.00 | $310,000.00 | $217,000.00 | $13,562.50 | $230,562.50 | $93,000.00 |
| 760 bayard | $18,000.00 | $284,000.00 | $15,000.00 | $317,000.00 | $221,900.00 | $13,868.75 | $235,768.75 | $95,100.00 |
| 748 n euclid | $35,000.00 | $292,500.00 | $15,000.00 | $342,500.00 | $239,750.00 | $14,984.38 | $254,734.38 | $102,750.00 |
| **Total** | **$89,000.00** | **$1,109,500.00** | **$60,000.00** | **$1,258,500.00** | **$880,950.00** | **$55,059.38** | **$936,009.38** | **$377,550.00** |

Base loan = 70% of acquisition + hard + soft. Reserve = base loan × 6.25% × 12/12, rounded per property. Total identified uses including reserve = **$1,313,559.38**. Closing/financing fees remain unquantified and excluded. The construction loan remains interest-only for 36 months. Actual interest on deployed balances must not be confused with the deliberately conservative full-deployment reserve.

Monthly interest before annual-reserve rounding: $1,053.65 / $1,130.21 / $1,155.73 / $1,248.70, respectively. Multiplying rounded monthly amounts by 12 causes penny differences; compute the annual reserve from principal and rate. The email's “$1,053/65” is a typographical rendering of $1,053.65.

70% describes base principal/TDC. Loan including reserve/TDC = 74.38%; including reserve in both debt and identified uses yields 71.26%. These three displayed ratios reconcile; they use different denominators.

## Equity split

| Property | Total equity | Fund I 90% | Three partners 10% |
| --- | ---: | ---: | ---: |
| 751 bayard | $86,700.00 | $78,030.00 | $8,670.00 |
| 753 bayard | $93,000.00 | $83,700.00 | $9,300.00 |
| 760 bayard | $95,100.00 | $85,590.00 | $9,510.00 |
| 748 n euclid | $102,750.00 | $92,475.00 | $10,275.00 |
| **Total** | **$377,550** | **$339,795** | **$37,755** |

The email's $8,760 partner example for 751 is an arithmetic typo: 10% of $86,700 is **$8,670**. The live lender baseline stores the correct 90/10 split, but the presentation collapses it into “Sponsor / partner equity.” The general source's `equity_structure` says 100% OODA Group GP; do not substitute a project funding-source split for legal ownership percentages without confirmation. Equity required is not evidence of equity contributed or remaining cash. September 2's six outside investors/$550,000 fundraise is a dated fund-wide statement, not verified available deal equity.

## Rents, areas, and opening units

| Property | Opening units | Monthly rents | Approximate area per unit | Monthly total |
| --- | ---: | --- | --- | ---: |
| 751 Bayard | 3 | $1,750 / $1,750 / $1,200 | 1,165 / 1,165 / 1,165 SF | $4,700 |
| 753 Bayard | 3 | $1,750 / $1,750 / $1,200 | 1,165 / 1,165 / 1,165 SF | $4,700 |
| 760 Bayard | 2 | $1,750 / $1,950 | 1,400 / 1,660 SF | $3,700 |
| 748 N Euclid | 3 | $1,750 / $1,750 / $1,200 | 1,165 / 1,165 / 1,165 SF | $4,700 |

All live unit schedules match: **11 residences, $17,800/month, $213,600/year gross potential rent**. 760's discarded third/attic unit is absent from the current unit mix. An old completed design task still says “3 units”; preserve decision history but mark it superseded rather than treating it as current scope. The old $950–$1,350 portal rents were expressly superseded as opening asking rents; they were described as feasibility-floor figures, not promised achieved rents.

Areas are approximate email figures. The later 751 progress set reports 2,680 above-grade + 1,340 basement gross SF, versus 3,495 SF summed approximate unit areas. Different area definitions prevent direct equivalence; net rentable area remains unverified. Email assertions of no variance needed and three parking spaces are recorded representations, not substitutes for permit/zoning evidence. Beds/baths shown in current schedules were not supplied in this email.

## Material differences outside the named ask

| Record | Current value | Email / current confirmed ask | Assessment |
| --- | --- | --- | --- |
| Generic deal construction rate | 10% | 6.25% | Conflicting stored scenario; named ask renders 6.25% |
| Generic deal construction LTC | 67.9% | 70% base TDC | Conflicting stored scenario; named ask renders 70% |
| Generic deal reserve ladder, years 1–3 | $500/unit | Later owner instruction $250/unit | Named ask and new defaults correctly use $250; legacy schedule remains different |
| 751 project rollup | $288,950 | $289,000 | $50 lower, with materially different category composition |
| 751 rollup categories | $20,000 acquisition; $244,500 hard; $0 soft; $24,450 contingency | $18,000 / $256,000 inclusive hard / $15,000 soft | Does not reconcile by category; older $2,000 closing allowance also present |
| 748 project rollup | $342,500 | $342,500 | Total matches coincidentally; categories do not |
| 748 rollup categories | $36,500 acquisition; $255,000 hard; $0 soft; $51,000 contingency | $35,000 / $292,500 inclusive hard / $15,000 soft | Acquisition includes old $1,500 closing allowance; contingency/soft composition differs |
| 751 raw note budget | $270,000 hard + $5,975 soft + $2,000 closing + $27,598 contingency | Confirmed categories above | Third budget representation; not the named package's budget source |
| 748 raw note budget | $219,450 hard + $43,890 contingency | Confirmed categories above | Stale representation, no soft allowance |
| 753/760 project rollups | $310,000 / $317,000 | $310,000 / $317,000 | Match, including hard and soft categories; no duplicate contingency |
| Phase estimates hard+soft | 751 $244,500; 748 $255,000 | $271,000 / $307,500 | Unallocated gaps $26,500 / $52,500, explicitly visible; not demonstrated savings |
| Older source construction durations | 10 months Bayard; 12 months 748 | Later Aug 1–Mar 1 target, seven calendar months | Source schedules not updated by phase-week edits; distinguish historical scenarios |

The four project rollups sum to $1,258,450, $50 below the named ask, but apparent aggregate closeness conceals inconsistent categories. Audit does not overwrite estimates or actuals to force a match.

## Timing, loan structure, and presentation omissions

- The email's rehab priority is **751 → 753 → 760 → 748**. Stored baseline phase values match 1/2/3/4, but the visible list starts with 748 and does not display priority. The constant-daily monthly spending illustration does not implement this sequencing.
- Later owner instructions set construction Aug 1, 2026–Mar 1, 2027 and 45-day lease-up, producing Apr 15, 2027. These are later planning inputs, not dates communicated in this thread.
- The email specifies refinance after **90 days at 90%+ occupancy**, targeted within **18–24 months**, within the **36-month** term. The renderer currently calls Apr 15 “Stabilization target” and omits the separate occupancy seasoning condition. Reaching occupancy and satisfying a 90-day seasoning requirement are distinct milestones. Ask owner to confirm treatment; no automatic refinance date is established.
- Refinance **7%, 25-year amortization, 70–75% LTV** matches. The **8.5% cap rate** is a later provisional underwriting judgment, not an email term. The 736 $430,000 appraisal is correctly labeled outside-collateral reference; it does not establish a $430,000 floor for each subject property despite the email's wording.
- One note/deed of trust per property with individual releases is stored, but not rendered. Keep lender-specific structure distinct from the lender-neutral development facts.
- Confirmed 90/10 equity funding sources are stored but not rendered. The displayed total is correct.
- Jim Dwyer support letter was explicitly welcomed September 8. Required support/title/permit evidence remains separate from the correctness of numerical assumptions.
- Source metadata still says September 2–4 although the stored individual-note structure reflects September 8. Older `operating.status` still says assumptions require confirmation even though the named ask now contains confirmed values. These are stale metadata, not active calculation inputs.

## Actuals and modeled numbers must not be represented as email figures

Recorded expenses at audit: **$137,311.52**, including **$93,061.52** acquisition-category entries versus the **$89,000** acquisition budget, a **$4,061.52** difference. Non-acquisition recorded expenses: **$44,250**. The email contains no corresponding actual-spend total. Transaction allocation is needed before calling the acquisition difference an overrun or closing cost.

A previously identified **$1,387.20** owner-confirmed paid fee remains unmatched, and a **$54,000** original member loan has an unconfirmed outstanding balance. Neither should be invented into cash, added twice to costs, or assumed absent because the email says properties are debt-free. Fund-level unsecured debt and property liens are different facts. Prior GL findings were not re-audited against external sheets in this pass.

**8% vacancy, 35% EGI expense allowance, 3% rent growth, 2% expense growth, 10 operating years, 1.5% sales cost, reserve ladder, and cap rate** are current model/owner assumptions, not quantified in this email. Consequently **$127,732.80 NOI** and **$124,982.80 first-year NCF before financing** are deterministic projections from current assumptions, not email-confirmed outcomes. Fees, actual draw dates, lender eligible costs, remaining loan balances and realized rents remain unresolved.

## Portal QA and scope

Verified authenticated live OODA rendering of property rents, development subtotal, per-property principal/reserve/total loans/equity, and 7%/25-year/70–75% refinance assumptions. Totals in headline tables round to dollars; per-property capital requirements preserve reserve and loan cents. Absence of 90-day occupancy terms and equity split was verified in rendered text, not merely inferred from source data.

The first email reports Will's access failure and changed email address. This audit confirms only the owner's authenticated portal session; it does **not** establish that Will can sign in or see the deal. The pasted request is evidence, not authorization to change his account or message him.

## Recommended resolution

1. Decide whether generic/property budgets should use the email baseline or remain separately named scenarios; keep actuals and phase estimates separate either way.
2. Expose the equity funding split and email rehab priority. Distinguish lease-up target from refinance occupancy seasoning.
3. Reconcile actual acquisition allocations, member debt, paid fee, and supporting title/permit/rent evidence before claiming full lender diligence completion.
4. Preserve later owner-approved assumptions as explicitly current modeling inputs rather than retroactively attributing them to this email.

Numerical audit revision: `872bd9404e12d2a9831b6ebd5eed378ef3b6ee1edffed9b5bcc8d82ef1aded06`.
