// RE SCREENING — the tier-1 underwriting math: NOI · ARV · TDC · DSCR.
//
// TWO BYTE-IDENTICAL COPIES, like chat-actions.js: the cockpit and the OODA
// portal are served from disjoint trees (server.go embeds web/, portal.go does
// fs.Sub over web/ooda), so a shared file has to be two physical files.
// server/web_chat_assets_test.go fails the build if they drift.
//
// It exists because the portal needed these numbers and the math lived only
// here. A second implementation in Go would have been a guaranteed drift: the
// DSCR identity below is subtle enough that two versions WILL disagree, and
// the disagreement would show up as a partner and the owner reading different
// returns off the same property.
//
// Pure by construction — the source sidecar and the assumptions are passed in
// rather than read from a global cache, because the portal has no such cache.
(function () {
  'use strict';

  // reSrcNum reads one number from a source sidecar, tolerating both shapes:
  // the property slice (flat keys) and the single-parcel full-deal object
  // (properties[0]) — the same fallback the Go sourceMoney applies.
  function reSrcNum(src, key) {
    if (!src) return 0;
    if (src[key] != null) return src[key] || 0;
    return (((src.properties || [])[0] || {})[key]) || 0;
  }

  // annual debt service on a fully-amortizing loan
  function reDebtService(loan, a) {
    const r = (a.perm_interest_rate || 0) / 12;
    const n = (a.perm_amort_years || 25) * 12;
    if (!loan || !r || !n) return 0;
    const m = loan * (r * Math.pow(1 + r, n)) / (Math.pow(1 + r, n) - 1);
    return m * 12;
  }

  function reScreen(p, src, a) {
    src = src || {};
    a = a || {};
    // measurables win (overhaul §3.1): the frontmatter unit mix carries the
    // unit count + per-unit rents; the source sidecar is the fallback
    const units = ((p.unitMix || []).length) || p.units || reSrcNum(src, 'total_units') || 0;
    const rent = (p.rentMonthly && units) ? p.rentMonthly / units : reSrcNum(src, 'avg_rent_per_unit');
    // cost inputs = THE BUDGET's plan figures (owner call 2026-08-19 — the
    // section had its own purchase/hard inputs that disagreed with the budget):
    // hard = Σ rock ests once estimated (else the underwrite figure), plus
    // closing, soft = carry when entered (else the screening approximation)
    const purchase = reSrcNum(src, 'purchase_price');
    const closing = reSrcNum(src, 'closing_costs');
    const workEst = (p.work || []).reduce((n, st) => n + (st.estTotal || 0), 0);
    const hard = workEst > 0 ? workEst : reSrcNum(src, 'hard_costs');
    if (!units || !rent) return { complete: false };
    const gross = units * rent * 12;
    const egi = gross * (1 - (a.vacancy_rate || 0));
    const noi = egi * (1 - (a.opex_rate || 0));
    const carry = reSrcNum(src, 'carry_cost');
    const soft = carry > 0 ? carry : hard * 0.15; // budget's soft plan, else the screening approximation
    const contingency = hard * (a.contingency_pct || 0);
    const tdc = purchase + closing + hard + soft + contingency;
    const arv = a.exit_cap_rate ? noi / a.exit_cap_rate : 0;
    // Loan sizing (owner report 2026-08-18 "DSCR is ALWAYS 1.22"): at the
    // LTV-max loan DSCR is a CONSTANT of the assumptions — loan = LTV·ARV and
    // ARV = NOI/cap, so NOI cancels: dscr = cap/(ltv·payment). The engine has
    // the same identity; the deal-varying number is DSCR at the TAKEOUT loan —
    // what the perm actually refinances (the construction loan, LTC·TDC),
    // capped by what the appraisal supports (LTV·ARV). When the LTV cap binds
    // the takeout, the shortfall is a refi gap — equity stays in the deal.
    const permMax = arv * (a.perm_ltv || 0);
    const takeout = tdc * (a.construction_loan_ltc || 0);
    const loan = takeout > 0 ? Math.min(permMax, takeout) : permMax;
    const refiGap = takeout > permMax ? takeout - permMax : 0;
    const ads = reDebtService(loan, a);
    return {
      complete: true, gross, egi, noi, tdc, arv, loan, permMax, refiGap,
      purchase, closing, hard, soft, contingency, hardFromWork: workEst > 0,
      dscr: ads ? noi / ads : 0,
    };
  }

  window.reSrcNum = reSrcNum;
  window.reDebtService = reDebtService;
  window.reScreen = reScreen;
})();
