# Bayard & Euclid lender diligence audit

Basis: owner-provided “OODA Group - Check In” email thread, September 2–4,
2026, including Will Essner’s final arithmetic correction and response on
individual notes. Owner confirmed September 8 that contingency is included
in the quoted hard costs and that the email is the proposed baseline,
with live actuals shown separately. This is not an executed loan commitment.

## Financing baseline

| Property | Acquisition | Hard incl. contingency | Soft allowance | Development cost | Base loan | 12-month reserve | Equity |
| --- | ---: | ---: | ---: | ---: | ---: | ---: | ---: |
| 751 Bayard | 18,000 | 256,000 | 15,000 | 289,000 | 202,300 | 12,643.75 | 86,700 |
| 753 Bayard | 18,000 | 277,000 | 15,000 | 310,000 | 217,000 | 13,562.50 | 93,000 |
| 760 Bayard | 18,000 | 284,000 | 15,000 | 317,000 | 221,900 | 13,868.75 | 95,100 |
| 748 N Euclid | 35,000 | 292,500 | 15,000 | 342,500 | 239,750 | 14,984.38 | 102,750 |
| Total | 89,000 | 1,109,500 | 60,000 | 1,258,500 | 880,950 | 55,059.38 | 377,550 |

Total request including reserve: $936,009.38. Reserve is rounded to cents;
748’s unrounded reserve is $14,984.375. Principal is 70% of development cost
before reserves. Equity: Fund I $339,795; partners $37,755. The $8,760
partner figure in the email example for 751 is a typo: $86,700 × 10% = $8,670.

Terms proposed: four individual 36-month, 6.25% interest-only notes and
property-specific deeds of trust with individual releases. Revolver deferred.
Reserve estimates fully deployed loans for 12 months; actual interest is
expected only on drawn balances. Fees for A2P closing/legal remain unquantified.
Refinance target: stabilization at 90 days of 90%+ occupancy, within 18–24
months, local bank/CU conventional loan; 7% rate, 25-year amortization,
70–75% LTV. The $430k appraisal on 736 is a reference, not proof that these
properties have that value or a guaranteed floor.

## Records examined and reconciled

All production writes used Manifest’s writer-backed APIs, including
revision-checked note changes. Records remain in system/realestate.

- Added 748 N Euclid to both the property deal link and deal member list;
  updated source property_slugs and display addresses. Internal stable slug
  bayard-rehab-trio is retained.
- Stored the dated baseline as lender_diligence in the deal source sidecar.
  Existing work budgets and actual expense entries remain distinct.
- 751: current frontmatter rents were $1,450/$1,450/$900 and 1,100 SF per unit;
  source rents were $1,350/$1,300/$950. Both current representations now use
  $1,750/$1,750/$1,200 and approximately 1,165 SF. August 19 locked
  underwriting preserved as historical evidence, not overwritten.
- 753: missing frontmatter unit mix; source still used the old low rents.
  Added three proposed units matching 751; updated source unit mix.
- 760: current frontmatter correctly had two units at $1,750/$1,950 and
  1,400/1,660 SF. Source still described three units/3,300 SF. Source corrected
  to two/3,060 SF. August three-unit decision remains historical; dated lender
  basis explicitly supersedes it with the abandoned attic/loft option.
- 748: frontmatter rents already matched the email, but source used $1,700
  for the upper units and 3,600 SF. Source now matches email units and areas.
- 753, 760 and 748: parent demo stages were open despite reported completion.
  Marked complete without inventing an exact completion date. 748 moved from
  pre-development to construction consistent with reported stabilization.
- All four retain the claimed ownership as owned and The Garden SPE entity.
  Assessor owner fields still name sellers; do not overwrite without title
  evidence or present those assessor names as confirmed current ownership.

## Remaining diligence gaps

1. Current stage estimates/source budgets differ from the confirmed lender
   baseline. 751 work-stage hard estimate was $244,500 plus 10% contingency;
   753/760 source hard costs $270k plus 10%; 748 source $255k plus 20%.
   These are operational estimates, not the September proposed capital stack.
   Reconcile detailed line allocations before claiming a fully tied-out budget.
2. 751 demolition has a $9k paid ledger entry lacking the contract/work link,
   while the accepted demo contract also appears as completed but unreconciled.
   Do not sum recognized work and cash expense amounts as if independent.
3. Property docs folders are empty for all four. Contract attachments exist
   separately in the content-addressed file store and must be included.
   Request actual draft/permit plans, permit evidence, appraisal and title docs.
4. The email says no parking variance is required and three parking spaces
   are feasible. This is owner-reported; no municipal approval was supplied.
5. Jim Dwyer support letter is requested, not yet evidenced.
6. Execution phasing 751 → 753 → 760 → 748 is the email plan; existing
   automatic stage dates do not verify the actual construction sequence.

## Current screening budget reconciliation

Verified from production records and the shared `reScreen` calculation on
September 8. Source-side historical contingencies above do not control the
current screening calculation: its global contingency is 5%. Soft costs use
15% of hard costs because these properties have no entered carrying budget.

| Property | Acquisition | Closing | Hard before contingency | Screening contingency | Screening soft | Screening TDC | Difference from email |
| --- | ---: | ---: | ---: | ---: | ---: | ---: | ---: |
| 751 | 18,000 | 2,000 | 244,500 | 12,225 | 36,675 | 313,400 | +24,400 |
| 753 | 18,000 | 2,000 | 270,000 | 13,500 | 40,500 | 344,000 | +34,000 |
| 760 | 18,000 | 2,000 | 270,000 | 13,500 | 40,500 | 344,000 | +27,000 |
| 748 | 35,000 | 1,500 | 255,000 | 12,750 | 38,250 | 342,500 | 0 |

751 and 748 use work-stage estimates; 753 and 760 fall back to source budgets.
748's equal total masks different cost allocations and is not proof of a
reconciled line-item budget. Aggregate screening is $85,400 above the proposed
email baseline. These differences are model assumptions, not documented
change orders or extra cash spent. The expandable budget comparison in the
preview explains each component without overwriting operational estimates.

Verified recorded expenses: 9 entries totaling $137,311.52. Five distinct
contract file links responded successfully. Plans/title/appraisal evidence is
still missing from the member-property document folders; the owner has been
asked for their location. No missing document is represented as complete.

## Lender view and future gated links

Private preview lives on the deal workspace under “Lender view”. Sections:
request, properties/unit economics, itemized cash expenses, plans/contracts,
underwriting inputs, and open diligence items. Baseline is visibly dated;
actuals are loaded from the current deal-member records. Missing and failed
loads are distinct. No public link or false password control is introduced.

When publication is implemented, use a dedicated read-only, deal-scoped
projection, not the private property API. A link needs an opaque token,
server-side password verification, hashed passwords in the secrets tier,
expiry/revocation, rate-limited sign-in, scoped sessions, and no indexing/cache
of gated content. Every document and download must enforce the same deal
membership/explicit inclusion policy. Never send the entire portfolio to the
browser and filter there. Preview the exact included documents and shared
contract allocations before enabling a link. Passwords never enter URLs.
Publishing infrastructure and live external links are deferred by the owner.

## September 8 document follow-up

Located and inspected the August 24 751 draft options V1–V3 in Downloads;
uploaded the unchanged PDF through the property document API. Its cover
specifies three units and is marked DRAFT. It lists 2,680 SF plus 1,340 SF
basement, which must be reconciled to the email's approximate net unit areas.
The email selects option 2; this is not evidence of permit approval.

Located Premier Appraisal Group report 26R-249 for 736 N Euclid. Inspected
the signed letter: $430,000 as of May 26, 2026, signed May 29 for West
Community Credit Union. Uploaded unchanged to 736's document folder and
explicitly included as a deal reference document, not collateral appraisal.
This resolves the earlier missing comparable appraisal finding. The 751
folder now contains its draft; other member-property plan folders remain empty.

The similarly named 736 Euclid.pdf is a 2024 settlement statement, not the
appraisal. Building Permit.pdf concerns 4848 Fountain, not these properties;
neither was attached as proof for this deal.

## Acquisition authorization and member funding follow-up

Uploaded the unchanged 748 member consent PDF, with signatures dated July 28
from Benjamin Anderson, Brian Fromal and Stephen Matic. It authorizes $35,000
to acquire from H & H; it is not proof of closing or debt-free title. The local
Bayard special warranty deed is an unsigned template with blank legal exhibit.
The inspected ALTA commitment is for 736 and was not attached as deal title.

Connected Drive revealed the July 10 $54,000 Fromal Organization member loan
to OODA Development Fund I LP, split $18,000 per Bayard acquisition. The note
states it is unsecured, not capital, and repaid at project capitalization less
a GP commitment holdback. No current payoff evidence has been established.
This does not by itself contradict the owner's property-lien statement, but
it requires reconciliation of available equity and capitalization proceeds.
Owner asked whether repaid or still outstanding. No extra expense or financing
request has been booked based on the original note alone.

Source: https://drive.google.com/file/d/1y_ZYwNnNJrvcP7sk53-ZBmyGfr8VMd5M/view

Drive also contains the 751/753/760 project budget trackers and Garden SPE GL
ledger; these are the next sources for checking recorded expenses and funding.

## External spreadsheet reconciliation

Read the three project trackers (Budget Tracker A1:N55 and A56:J85), Garden
SPE Ledger A1:N55 and A56:H150, and Fund I Ledger A1:J70 on September 8.
No spreadsheet edits or inferred cash transactions were made.

- The three trackers each calculate $231,200 plus $46,240 contingency =
  $277,440. Their footer still cites a different $263,340 template total.
- 760's workbook title identifies 760 but its content says 753 and three units.
  It is stale template material, not evidence for changing the two-unit plan.
- Tracker demo actuals for 753/760 are $10,500/$12,000; Manifest, Garden GL,
  and Fund GL agree on $12,000/$10,500. Preserve recorded cash allocations.
- 751's tracker and GL confirm $9,000 demo, but the GL check reference is 1008
  while Manifest's statement reference is also 1008 and posting dates differ.
  Receipt evidence remains useful for definitive contract reconciliation.
- Garden GL row 43 lists a $1,387.20 August 31 construction-management fee
  to OpCo for the three Bayard properties. No matching Manifest payment was
  found. Owner asked whether paid or accrued and how allocated; not booked.
- The Garden GL reports $33,602.80 out of balance; Fund GL reports $47,296.70.
  These are spreadsheet accounting imbalances, not inferred missing cash.
- Fund GL records the July $54,000 member loan and includes a question about
  its relationship to a June $50,000 Garden deposit. No repayment is shown
  in the inspected rows; outstanding status still requires confirmation.
- July 23 Bayard acquisition total $54,893.53 agrees with Manifest's three
  allocations, including the remainder penny. Intercompany transfers are
  funding, not additional expenses, and were not imported as costs.

Sources:
- https://docs.google.com/spreadsheets/d/1S8--OhLiUzRMlVWU0iTXt8oA7CQ-O4sj1mkztU_QG_A/edit
- https://docs.google.com/spreadsheets/d/1QI1Y02j9Qb3SVDNCgMP1rBaD2SHiTgD4DqXCZukygT4/edit
- https://docs.google.com/spreadsheets/d/1iTDkkhZvudlp-AtO0tapbJHKZlro0NwYnmR__eSIwVQ/edit
- https://docs.google.com/spreadsheets/d/1zceijOcAMRJyj6orACDV6Jw8XqdhFsVQPRP19oMBEJc/edit
- https://docs.google.com/spreadsheets/d/1rjntEygljJIYJk2U5UOuSgAFEqDMbISNk8o8Mx2PFmA/edit

Owner subsequently confirmed the $1,387.20 management fee was paid and will
provide transaction details. Lender preview now identifies this as a confirmed
payment awaiting matching/allocation, separately from recorded ledger totals.
No invented posting date or equal property split has been entered.
