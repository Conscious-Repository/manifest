// DASHBOARD — the portfolio at a glance. Every tile answers a question and
// every figure is derived server-side from the same rollup the private
// cockpit reads, so the two never disagree.

// Yours — what is on the READER, before any portfolio number.
//
// The reads stay FLAT (TestOodaReadsAreFlat): every member gets identical bytes
// and the sign-in gate is the boundary. So the scoping happens here, in the
// browser, off `me.initials` and the work groups the page already fetched.
// Nothing new crosses the wire and no doctrine moves.
function YoursBlock({ data, me, go }) {
  const groups = (data.work && data.work.groups) || [];
  const mine = ((me && me.initials) || "").toUpperCase();

  // A signed-in member with no initials owns nothing the vault can name, so
  // every list below would be empty and look like "you have no work" rather
  // than "we could not resolve you". Say which it is.
  if (!mine) {
    return (
      <Section title="YOURS">
        <div className="ooda-empty">
          {(me && me.email ? me.email : "your account") + " is not mapped to a " +
            "set of initials yet, so work assigned to you cannot be matched. " +
            "Ask Benjamin to add you to the portal roster."}
        </div>
      </Section>
    );
  }

  const g = groups.find((x) => (x.owner || "").toUpperCase() === mine);
  const lanes = g || { overdue: [], dueThisWeek: [], open: [], decisions: [], waiting: [] };
  const total = lanes.overdue.length + lanes.dueThisWeek.length +
    lanes.open.length + lanes.decisions.length;

  // Decisions lead: a decision you owe blocks other people's work, which an
  // open task of your own does not. Then overdue, then this week, then the
  // rest — urgency order, not source order.
  const rows = [].concat(
    lanes.decisions.map((it) => ({ ...it, lane: "DECIDE" })),
    lanes.overdue.map((it) => ({ ...it, lane: "DO", late: true })),
    lanes.dueThisWeek.map((it) => ({ ...it, lane: "DO" })),
    lanes.open.map((it) => ({ ...it, lane: "DO" })),
  );
  const shown = rows.slice(0, 6);

  return (
    <Section title={"YOURS · " + mine}>
      <div className="ooda-yours-counts">
        {/* the section title is uppercased by the shell; a person's NAME
            shouting is a different thing from an initials token doing it */}
        {g && g.name ? <span className="ooda-yours-name">{g.name}</span> : null}
        <span className={lanes.overdue.length ? "over" : ""}>
          {lanes.overdue.length ? "● " : ""}{lanes.overdue.length || DASH} overdue
        </span>
        <span>{lanes.dueThisWeek.length || DASH} due this week</span>
        <span>{lanes.open.length || DASH} open</span>
        <span>{lanes.decisions.length || DASH} decisions</span>
        {lanes.waiting.length ? <span>{lanes.waiting.length} waiting</span> : null}
      </div>
      {!rows.length ? <Empty>nothing is waiting on you</Empty> : null}
      {shown.map((it) => (
        <div className="ooda-row cols-yours click" key={it.id}
          onClick={() => go("work")} role="button">
          <span className={"ooda-lane-tag" + (it.lane === "DECIDE" ? " decide" : "")}>{it.lane}</span>
          <span className="ooda-stack">
            <b>{it.title}</b>
            <em>{[it.container, it.rock].filter(Boolean).join(" · ") || DASH}</em>
          </span>
          <span className={"r ooda-sub" + (it.late ? " over" : "")}>
            {it.late ? "overdue · " + it.due.slice(5) : it.due ? "due " + it.due.slice(5) : DASH}
          </span>
        </div>
      ))}
      {rows.length > shown.length ? (
        <div className="ooda-more" onClick={() => go("work")} role="button">
          {"+ " + (rows.length - shown.length) + " more yours →"}
        </div>
      ) : null}
    </Section>
  );
}

function ViewDashboard({ data, me, go }) {
  const d = data.dashboard || {};
  const k = d.kpis || {};
  return (
    <>
      <YoursBlock data={data} me={me} go={go} />
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

      {/* what lands in seven days, across everyone. Overdue is part of this
          week's problem, so it rides along and sorts first. */}
      <Section title="THIS WEEK" count={(d.week || []).length}>
        {!(d.week || []).length ? <Empty>nothing due in the next seven days</Empty> : null}
        {(d.week || []).slice(0, 12).map((it) => (
          <div className="ooda-row cols-week click" key={it.id}
            onClick={() => go("work")} role="button">
            <span className="ooda-stack">
              <b>{it.title}</b>
              <em>{[it.container, it.rock].filter(Boolean).join(" · ") || DASH}</em>
            </span>
            <span className={it.owner ? "ooda-sub" : "ooda-unassigned"}>
              {it.owner || "— unassigned —"}
            </span>
            <span className={"r ooda-sub" + (it.due && it.due < todayISO() ? " over" : "")}>
              {it.due ? (it.due < todayISO() ? "overdue · " : "") + it.due.slice(5) : DASH}
            </span>
          </div>
        ))}
        {(d.week || []).length > 12 ? (
          <div className="ooda-more" onClick={() => go("work")} role="button">
            {"+ " + ((d.week || []).length - 12) + " more this week →"}
          </div>
        ) : null}
      </Section>

      {/* who is blocking whom — [waiting:: who], parsed since the WORK tab
          shipped and never shown anywhere until now */}
      {(d.waiting || []).length ? (
        <Section title="WAITING ON" count={d.waiting.length}>
          {d.waiting.map((it) => (
            <div className="ooda-row cols-waiting click" key={it.id}
              onClick={() => go("work")} role="button">
              <span className="ooda-stack">
                <b>{it.title}</b>
                <em>{[it.container, it.rock].filter(Boolean).join(" · ") || DASH}</em>
              </span>
              <span className="ooda-sub">{orDash(it.owner)}</span>
              <span className="r ooda-sub">
                {it.waitingOn ? "waiting on " + it.waitingOn : "waiting"}
              </span>
            </div>
          ))}
        </Section>
      ) : null}

      <Section title="OPEN WORK BY PERSON" count={(d.owners || []).length}>
        <div className="ooda-row ooda-head-row cols-owner">
          <span>PERSON</span><span /><span className="r">OPEN</span>
          <span className="r">OVERDUE</span><span className="r">DECISIONS</span>
        </div>
        {(d.owners || []).map((o, i) => (
          <div className="ooda-row cols-owner click" key={i}
            onClick={() => go("work")} role="button">
            <span className={o.owner ? "" : "ooda-unassigned"}>
              {o.owner || "— unassigned —"}{o.name ? " · " + o.name : ""}
            </span>
            <span className="ooda-bar"><i style={{ width: Math.min(100, o.open * 6) + "%" }} /></span>
            <span className="r">{o.open || DASH}</span>
            {/* the server has computed this since the tab shipped and the page
                never rendered it */}
            <span className={"r" + (o.overdue ? " over" : "")}>{o.overdue || DASH}</span>
            <span className="r">{o.decisions || DASH}</span>
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
