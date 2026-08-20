// DASHBOARD — the portfolio at a glance. Every tile answers a question and
// every figure is derived server-side from the same rollup the private
// cockpit reads, so the two never disagree.

function ViewDashboard({ data, go }) {
  const d = data.dashboard || {};
  const k = d.kpis || {};
  return (
    <>
      <div className="ooda-tiles">
        <Tile label="COMMITTED" value={money(k.committed)}
          sub="promised across the portfolio" onClick={() => go("portfolio")} />
        <Tile label="PAID" value={money(k.paid)}
          sub={k.committed ? pct(k.paid, k.committed) + " of committed" : null}
          onClick={() => go("portfolio")} />
        <Tile label="PLAN TO GO" value={money(k.planToGo)} sub="left against plan" />
        <Tile label="OVER PLAN" value={k.overPlan ? String(k.overPlan) : DASH}
          sub={k.overPlan ? "projects past estimate" : "nothing over"}
          tone={k.overPlan ? "over" : ""} onClick={k.overPlan ? () => go("portfolio") : null} />
      </div>
      <div className="ooda-cadence">
        {"as of " + orDash(k.asOf) + " · " + (k.liveProjects || 0) + " live projects · " +
          (k.portfolioSize || 0) + " properties · " + (k.entities || 0) + " entities"}
      </div>

      <Section title="BY ENTITY" count={(d.entities || []).length}>
        <div className="ooda-row ooda-head-row cols-entity">
          <span>ENTITY</span><span className="r">OWNED</span><span className="r">ACQUIRING</span>
          <span className="r">COMMITTED</span><span className="r">PAID</span><span className="r">OPEN</span>
        </div>
        {(d.entities || []).map((e, i) => (
          <div className="ooda-row cols-entity" key={i}>
            <span className={e.entity ? "" : "ooda-unassigned"}>{e.entity || "— unassigned —"}</span>
            <span className="r">{e.owned || DASH}</span>
            <span className="r">{e.acquiring || DASH}</span>
            <span className="r">{money(e.committed)}</span>
            <span className="r">{money(e.paid)}</span>
            <span className="r">{e.openWork || DASH}</span>
          </div>
        ))}
      </Section>

      <Section title="ATTENTION" count={(d.attention || []).length}>
        {!(d.attention || []).length ? <Empty>nothing outside plan</Empty> : null}
        {(d.attention || []).map((f) => (
          <div className="ooda-row cols-attn" key={f.slug}>
            <span>{f.short}</span>
            <span className="ooda-flags">
              {f.over ? <em className="over">over plan</em> : null}
              {f.late ? <em className="over">late</em> : null}
              {f.stalled ? <em>nothing queued</em> : null}
            </span>
            <span className="r">{f.over ? money(f.paid) + " of " + money(f.plan) : orDash(f.rock)}</span>
          </div>
        ))}
      </Section>

      <Section title="OPEN WORK BY PERSON" count={(d.owners || []).length}>
        {(d.owners || []).map((o, i) => (
          <div className="ooda-row cols-owner" key={i}
            onClick={() => go("work")} role="button">
            <span className={o.owner ? "" : "ooda-unassigned"}>
              {o.owner || "— unassigned —"}{o.name ? " · " + o.name : ""}
            </span>
            <span className="ooda-bar"><i style={{ width: Math.min(100, o.open * 6) + "%" }} /></span>
            <span className="r">{o.open || DASH}</span>
            <span className="r">{o.decisions ? o.decisions + " dec" : DASH}</span>
          </div>
        ))}
      </Section>

      <Section title="DEALS" count={(d.deals || []).length}>
        {(d.deals || []).map((x) => (
          <div className="ooda-row cols-deal" key={x.slug}>
            <span>{x.name}</span>
            <span className="ooda-sub">{statusLabel(x.status)}</span>
            <span className="r">{x.members} propert{x.members === 1 ? "y" : "ies"}</span>
            <span className="r">{money(x.committed)}</span>
            <span className="r">{money(x.paid)}</span>
          </div>
        ))}
      </Section>
    </>
  );
}
