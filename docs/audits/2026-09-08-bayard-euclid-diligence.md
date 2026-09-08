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
