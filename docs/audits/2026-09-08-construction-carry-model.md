# Construction carry and refinance bridge

A reusable, deterministic new-facility scenario is available under Execution → Monthly funding & refinance scenarios. Inputs belong to each deal's named package assumptions; saved values feed private, team and password-protected lender views. No assumptions have been adopted for Bayard pending owner input.

## Required decisions

- Loan closing date, distinct from construction commencement.
- Whether eligible costs paid before closing are reimbursed pro rata.
- Minimum monthly carrying costs per residence (tax, insurance, utilities, maintenance).
- Base and delayed refinance timing measured from loan closing.
- Minimum NCF coverage ratio. Refinance fees can remain unknown, explicitly excluded.

## Method

Each property's email-approved acquisition/hard/soft budget and live unit rents feed a separate daily model, aggregated monthly. Recorded pre-closing expenses are treated as eligible for the draft scenario; eligibility must be confirmed by the lender. Remaining costs are allocated uniformly through completion. Loan and equity fund costs pro rata; optional reimbursement applies to pre-closing costs. No existing loan is presumed refinanced by the construction facility.

Income ramps linearly during lease-up, with stabilized vacancy applied once. Operating costs are the greater of the entered carry floor or the percentage allowance on collected rent. Replacement reserves begin at completion and use the adopted age ladder. No rent or expense growth is assumed during the bridge. Income and costs use calendar-year daily fractions; interest uses actual/365 and includes previously drawn reserve principal. Available retained rental cash pays interest before the reserve. Any unfunded operating or interest deficit requires equity. Reserves cannot cross between properties.

Refinance capacity is the lesser of cap-value × LTV and NCF / minimum coverage / annual mortgage constant. Payoff includes drawn base principal and reserve; undrawn reserve is not debt. Net gap considers retained cash and entered refinance fees. Other debts and unknown construction closing costs remain excluded and disclosed. Stabilized capacity is conditional if lease-up has not completed; actual lender seasoning/occupancy requirements remain separate.

This is a fixed scenario beginning at loan closing, not a bank draw ledger or rolling cost-to-complete certification. Later actuals appear as comparisons and are not added a second time to projected costs. Actual debt balances/disbursements, lender eligibility, trade schedules and verified remaining costs are required to support a future rolling forecast.

## Validation

Automated checks cover cost/debt/cash conservation, reimbursement on/off, reserve exhaustion, delayed refinancing, missing required inputs, and post-closing expense updates without duplicated projected draws. Existing financing and operating regression tests remain applicable.

## Research

OCC Commercial Real Estate Lending handbook: interest reserves should be grounded in feasibility and cash flow; operating cash available for debt service matters, and reserves do not establish repayment ability.
https://www.occ.gov/publications-and-resources/publications/comptrollers-handbook/files/commercial-real-estate-lending/pub-ch-commercial-real-estate.pdf

OCC refinance-risk guidance: assess repayment at maturity under reasonable refinance terms.
https://www.occ.gov/news-issuances/bulletins/2024/bulletin-2024-29.html
