// OODA portal — shared helpers. Plain script (no JSX), loaded before the views.
// Manifest-quiet conventions (ARCHITECTURE §1): mono labels, hairlines, and
// an em-dash for EVERY missing value — never a blank, never a zero standing in
// for "unknown".

const DASH = "—";

function money(v) {
  if (v == null || v === "" || isNaN(v)) return DASH;
  const n = Number(v);
  if (!n) return DASH;
  return "$" + n.toLocaleString("en-US", { maximumFractionDigits: 0 });
}

// moneyExact — for ledger rows: accounting, so never rounded.
function moneyExact(v) {
  if (v == null || v === "" || isNaN(v)) return DASH;
  return "$" + Number(v).toLocaleString("en-US", { minimumFractionDigits: 2, maximumFractionDigits: 2 });
}

function orDash(v) {
  const s = (v == null ? "" : String(v)).trim();
  return s === "" ? DASH : s;
}

function pct(a, b) {
  if (!b) return DASH;
  return Math.round((a / b) * 100) + "%";
}

// phaseLabel maps the 8 record statuses through the 5 phases the filters use.
function statusLabel(s) { return orDash((s || "").replace(/_/g, " ")); }

function todayISO() { return new Date().toISOString().slice(0, 10); }
