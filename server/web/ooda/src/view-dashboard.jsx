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
          sub="owned budgets + cost to close" onClick={() => go("portfolio")} />
        <Tile label="PAID" value={money(k.paid)}
          sub="cash out the door" onClick={() => go("portfolio")} />
        <Tile label="PLAN TO GO" value={money(k.planToGo)} sub="still to spend" />
        <Tile label="OVER PLAN" value={k.overPlan ? String(k.overPlan) : DASH}
          sub={k.overPlan ? "projects past estimate" : "nothing over"}
          tone={k.overPlan ? "over" : ""} onClick={k.overPlan ? () => go("portfolio") : null} />
      </div>
      {/* The tiles are only trustworthy if the reader knows what each counts.
          Partners were reading COMMITTED as signed contracts and PAID as
          verified cash, and only one of those was true. */}
      <div className="ooda-defs">
        <span><em>COMMITTED</em> the full budget of every project we own, plus what
          it costs to close the {k.underContract || 0} still under contract
          ({money(k.toClose)}).</span>
        <span><em>PAID</em> money we can verify has left a bank account: logged
          bank transactions and expensed ledger rows, plus the purchase price of
          deals that have closed. A deal under contract has not spent its
          purchase price.
          {k.recognized > k.paid
            ? " " + money(k.recognized - k.paid) + " more is work finished at a firm price with no expense row yet."
            : ""}</span>
        <span><em>PLAN TO GO</em> what remains on the projects we own, plus the
          whole budget of the ones we are closing on.</span>
      </div>
      <div className="ooda-cadence">
        {"as of " + orDash(k.asOf) + " · " + (k.owned || 0) + " owned · " +
          (k.underContract || 0) + " under contract" +
          (k.pipeline ? " · " + k.pipeline + " in negotiation" : "") +
          " · " + (k.liveProjects || 0) + " live projects · " + (k.entities || 0) + " entities"}
      </div>

      <Section title="BY ENTITY" count={(d.entities || []).length}>
        <div className="ooda-row ooda-head-row cols-entity">
          <span>ENTITY</span><span className="r">OWNED</span><span className="r">UNDER CONTRACT</span>
          <span className="r">TO CLOSE</span><span className="r">COMMITTED</span>
          <span className="r">PAID</span><span className="r">OPEN</span>
        </div>
        {(d.entities || []).map((e, i) => (
          <div className="ooda-row cols-entity" key={i}>
            <span className={e.entity ? "" : "ooda-unassigned"}>
              {e.entity || "— unassigned —"}
              {e.pipeline ? <em className="ooda-sub"> · {e.pipeline} in negotiation</em> : null}
            </span>
            <span className="r">{e.owned || DASH}</span>
            <span className="r">{e.acquiring || DASH}</span>
            <span className="r">{money(e.toClose)}</span>
            <span className="r">{money(e.committed)}</span>
            <span className="r">{money(e.paid)}</span>
            <span className="r">{e.openWork || DASH}</span>
          </div>
        ))}
        {/* owned and under-contract are separate counts and must never be
            summed — "the Garden SPE owns 32" was exactly that mistake */}
        <div className="ooda-note">
          owned and under contract are counted separately, never summed: a deal
          under contract is not yet on the entity's books.
        </div>
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
            <span className="r">
              {f.over ? money(Math.max(f.recognized || 0, f.committed || 0)) + " of " + money(f.plan)
                : orDash(f.rock)}
            </span>
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
