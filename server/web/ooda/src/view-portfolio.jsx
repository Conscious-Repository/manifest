// PORTFOLIO — the same shape the private cockpit's list has, so the two read
// alike: search, cut chips, one flat table, an em-dash for every missing
// value, and no progress bars. Clicking a row opens the property detail.

const OODA_CUTS = [
  ["open", "OPEN"], ["all", "ALL"], ["attention", "ATTENTION"],
  ["construction", "CONSTRUCTION"], ["pre-dev", "PRE-DEV"],
  ["pipeline", "PIPELINE"], ["stabilized", "HELD"],
];

function ViewPortfolio({ data }) {
  const [q, setQ] = React.useState("");
  const [cut, setCut] = React.useState("open");
  const [sel, setSel] = React.useState(null);
  const rows = (data.portfolio && data.portfolio.properties) || [];

  const visible = rows.filter((f) => {
    if (cut === "open" && (f.phase === "stabilized" || f.phase === "closed")) return false;
    if (cut === "attention" && !(f.over || f.late || f.stalled)) return false;
    if (["construction", "pre-dev", "pipeline", "stabilized"].includes(cut) && f.phase !== cut) return false;
    const needle = q.trim().toLowerCase();
    if (!needle) return true;
    return [f.address, f.short, f.entity, f.deal, f.status, f.rock]
      .join(" ").toLowerCase().includes(needle);
  });

  return (
    <div className="ooda-split">
      <div className="ooda-list">
        <div className="ooda-toolbar">
          <input className="ooda-search" type="search" value={q}
            placeholder="Search address, entity, deal, rock…"
            onChange={(e) => setQ(e.target.value)} />
          {OODA_CUTS.map(([key, label]) => (
            <button key={key} className={"ooda-chip" + (cut === key ? " on" : "")}
              onClick={() => setCut(key)}>{label}</button>
          ))}
        </div>
        <div className="ooda-row ooda-head-row cols-prop">
          <span>PROPERTY</span><span>ROCK</span><span className="r">OPEN</span><span className="r">SPENT</span>
        </div>
        {!visible.length ? <Empty>No properties match.</Empty> : null}
        {visible.map((f) => (
          <div key={f.slug} className={"ooda-row cols-prop click" + (sel === f.slug ? " sel" : "")}
            onClick={() => setSel(sel === f.slug ? null : f.slug)} role="button">
            <span className="ooda-stack">
              <b>{f.short}</b>
              <em>{orDash(f.entity) + " · " + statusLabel(f.status)}</em>
            </span>
            <span className="ooda-stack">
              <b>{orDash(f.rock)}</b>
              <em className={f.late ? "over" : ""}>
                {f.late ? "● late — was due " + f.doneBy.slice(5)
                  : f.stalled ? "● nothing queued" : f.doneBy ? "by " + f.doneBy.slice(5) : DASH}
              </em>
            </span>
            <span className="r">{f.open || DASH}</span>
            <span className="ooda-stack r">
              <b className={f.over ? "over" : ""}>{money(f.paid)}</b>
              <em>{f.plan ? "of " + money(f.plan) : "no plan"}</em>
            </span>
          </div>
        ))}
        <div className="ooda-foot">{visible.length + " of " + rows.length}</div>
      </div>
      <aside className="ooda-insp">
        {sel ? <PropertyDetail slug={sel} onClose={() => setSel(null)} />
          : <div className="ooda-insp-empty">select a property</div>}
      </aside>
    </div>
  );
}

// PropertyDetail — money, rocks, contracts, and the FULL ledger. Every member
// sees the same bytes here, vendor names and amounts included: the partners
// are co-investors (owner decision 2026-08-20). No admin branch exists.
function PropertyDetail({ slug, onClose }) {
  const [d, setD] = React.useState(null);
  const [err, setErr] = React.useState("");
  React.useEffect(() => {
    let live = true;
    setD(null); setErr("");
    getJSON("/api/ooda/property/" + encodeURIComponent(slug))
      .then((x) => { if (live) setD(x); })
      .catch((e) => { if (live) setErr(String(e.message || e)); });
    return () => { live = false; };
  }, [slug]);

  if (err) return <div className="ooda-insp-empty">{err}</div>;
  if (!d) return <div className="ooda-insp-empty">loading…</div>;
  const p = d.property || {};
  const f = d.facts || {};
  const ledger = d.ledger || [];

  // group the ledger by the rock its rows are tethered to, so the per-rock
  // money above reconciles visibly to the rows below; untethered rows fall to
  // their own block rather than being hidden.
  const groups = {};
  ledger.forEach((r) => {
    const key = r.workId ? String(r.workId).split("/")[0] : "";
    (groups[key] = groups[key] || []).push(r);
  });

  return (
    <div className="ooda-detail">
      <div className="ooda-detail-head">
        <b>{p.short || p.address}</b>
        <button className="ooda-x" onClick={onClose}>✕</button>
      </div>
      <div className="ooda-detail-meta">
        {[orDash(p.entity), statusLabel(p.status), orDash(p.kind), p.deal ? "deal: " + p.deal : null]
          .filter(Boolean).join(" · ")}
      </div>
      <div className="ooda-money">
        <span><em>PLAN</em><b>{money(f.plan)}</b></span>
        <span><em>COMMITTED</em><b>{money(f.committed)}</b></span>
        <span><em>PAID</em><b className={f.over ? "over" : ""}>{money(f.paid)}</b></span>
        <span><em>TO GO</em><b>{money(f.toGo)}</b></span>
      </div>

      <Section title="ROCKS" count={(p.work || []).length}>
        {(p.work || []).map((st) => (
          <div key={st.id} className="ooda-rock">
            <div className="ooda-rock-head">
              <span>{st.checked ? "✓ " : ""}{st.text}</span>
              <span className="r">{st.doneBy ? "by " + st.doneBy : DASH}</span>
            </div>
            {(st.tasks || []).filter((n) => !n.checked).map((n) => (
              <div key={n.id} className="ooda-node">
                <span>{n.text}</span>
                <span className="ooda-sub">{orDash(n.owner)}</span>
              </div>
            ))}
          </div>
        ))}
      </Section>

      <Section title="CONTRACTS & BIDS" count={(d.contracts || []).length}>
        {!(d.contracts || []).length ? <Empty>none yet</Empty> : null}
        {(d.contracts || []).map((c) => (
          <div key={c.slug} className="ooda-row cols-contract">
            <span>{c.name}</span>
            <span className="ooda-sub">{c.status}</span>
            <span className="r">{money(c.amount)}</span>
          </div>
        ))}
      </Section>

      <Section title="LEDGER" count={ledger.length}>
        {!ledger.length ? <Empty>no money facts yet</Empty> : null}
        {Object.keys(groups).sort().map((key) => (
          <div key={key || "_"} className="ooda-ledger-group">
            <div className="ooda-ledger-rock">{key || "— unattributed —"}</div>
            {groups[key].map((r, i) => (
              <div key={i} className="ooda-row cols-ledger">
                <span className="ooda-sub">{r.date}</span>
                <span>{orDash(r.vendor || r.contractor)}</span>
                <span className="ooda-sub">{orDash(r.category)}</span>
                <span className={"r" + (r.type === "income" ? " in" : "")}>
                  {(r.type === "income" ? "+" : "") + moneyExact(r.amount)}
                </span>
              </div>
            ))}
          </div>
        ))}
      </Section>
    </div>
  );
}
