// PORTFOLIO — the same shape the private cockpit's list has, so the two read
// alike: search, cut chips, one flat table, an em-dash for every missing
// value, and no progress bars. Clicking a row opens the property detail.

// OWNED and UNDER CONTRACT lead, because that is the cut a partner actually
// wants: 29 of the 42 rows are one bundle of unclosed Bayard lots, and they
// swamp the list they are read alongside.
const OODA_CUTS = [
  ["owned", "OWNED"], ["under-contract", "UNDER CONTRACT"],
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
    if ((cut === "owned" || cut === "under-contract") && f.acq !== cut) return false;
    if (cut === "open" && (f.phase === "stabilized" || f.phase === "closed")) return false;
    if (cut === "attention" && !(f.over || f.late || f.stalled)) return false;
    if (["construction", "pre-dev", "pipeline", "stabilized"].includes(cut) && f.phase !== cut) return false;
    const needle = q.trim().toLowerCase();
    if (!needle) return true;
    return [f.address, f.short, f.entity, f.deal, f.status, f.rock]
      .join(" ").toLowerCase().includes(needle);
  });

  return (
    // has-sel drives the phone master-detail: with a property open the list is
    // hidden and the detail takes the screen. Stacked, the 42-row list pushed
    // the detail ~2,900px down — three screens below the fold, no scroll, which
    // reads as "tapping does nothing".
    <div className={"ooda-split" + (sel ? " has-sel" : "")}>
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
              <b>
                {f.short}
                {/* the ownership word rides on the row itself: a partner
                    scanning the list must not read "Garden SPE" as "owned" */}
                <i className={"ooda-acqchip acq-" + f.acq}>{acqLabel(f.acq)}</i>
              </b>
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
  const [bidNote, setBidNote] = React.useState("");
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
      {/* phone-only: the ✕ is a fine desktop affordance and a poor thumb
          target, and on a phone this IS the whole screen */}
      <button className="ooda-back" onClick={onClose}>← all properties</button>
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
        <span><em>CONTRACTED</em><b>{money(f.committed)}</b></span>
        <span><em>PAID</em><b className={f.over ? "over" : ""}>{money(f.paid)}</b></span>
        <span><em>TO GO</em><b>{money(f.toGo)}</b></span>
      </div>
      {f.acq === "under-contract" ? (
        <div className="ooda-note">
          {"not closed yet — " + money(f.toClose) + " to close. Its purchase " +
            "price is committed, not spent."}
        </div>
      ) : null}
      {f.recognized > f.paid ? (
        <div className="ooda-note">
          {money(f.recognized - f.paid) + " of work is finished at a firm price " +
            "but has no expense row yet, so it is not counted as paid."}
        </div>
      ) : null}

      <DecisionsLane work={p.work} />

      <Section title="ROCKS" count={(p.work || []).length}>
        {!(p.work || []).length ? <Empty>no work planned yet</Empty> : null}
        {(p.work || []).map((st) => <Rock key={st.id} st={st} />)}
      </Section>

      <Underwriting p={p} source={d.source} assumptions={d.assumptions} />

      {p.operating && (p.operating.months || []).length ? (
        <Section title="OPERATING" count={p.operating.months.length}>
          <div className="ooda-row ooda-head-row cols-op">
            <span>MONTH</span><span className="r">INCOME</span>
            <span className="r">OPEX</span><span className="r">NET</span>
          </div>
          {p.operating.months.slice().reverse().slice(0, 12).map((m) => (
            <div key={m.month} className="ooda-row cols-op">
              <span className="ooda-sub">{m.month}</span>
              <span className="r in">{money(m.income)}</span>
              <span className="r">{money(m.expenses)}</span>
              <span className={"r" + (m.net < 0 ? " over" : "")}>{money(m.net)}</span>
            </div>
          ))}
        </Section>
      ) : null}

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

      <BidForm slug={slug} property={p} onFiled={() => setBidNote("bid filed — it is waiting on Benjamin's approval")} note={bidNote} />

      <Thread itemID={"prop/" + slug} title={p.short || p.address} />

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

      {(p.log || []).length ? (
        <Section title="LOG" count={p.log.length}>
          {p.log.slice(0, 12).map((line, i) => (
            <div key={i} className="ooda-logline">{line}</div>
          ))}
        </Section>
      ) : null}

      <OwnerCard p={p} f={f} />

      {p.drive || p.agc ? (
        <div className="ooda-links">
          {p.drive ? <a href={p.drive} target="_blank" rel="noreferrer">Drive ↗</a> : null}
          {p.agc ? <a href={p.agc} target="_blank" rel="noreferrer">AGC ↗</a> : null}
        </div>
      ) : null}
    </div>
  );
}

// Rock — one stage of the tree with its money and its nested nodes.
//
// The cockpit deliberately has NO "current stage" marker (owner call
// 2026-08-12: rocks are not sequential, they run in parallel and any of them
// takes tasks). The schedule is each rock's own [done-by::], so that is what
// carries the late ink here too.
function Rock({ st }) {
  const today = todayISO();
  const late = !st.checked && st.doneBy && st.doneBy < today;
  return (
    <div className={"ooda-rock" + (st.checked ? " done" : "") + (late ? " late" : "")}>
      <div className="ooda-rock-head">
        <span className="ooda-rock-mark">{st.checked ? "✓" : "○"}</span>
        <span className="ooda-rock-text">{st.text}</span>
        {st.estTotal ? <span className="ooda-est">{money(st.estTotal)}</span> : null}
        <span className={"r" + (late ? " over" : "")}>
          {st.doneBy ? (late ? "late · " : "by ") + st.doneBy.slice(5) : DASH}
        </span>
      </div>
      {/* per-rock money: what is contracted against it and what has been paid.
          The cockpit shows this and the portal used to drop it entirely. */}
      {st.committed || st.paid || st.unreconciled ? (
        <div className="ooda-rock-money">
          {st.committed ? <span>contracted {money(st.committed)}</span> : null}
          {st.paid ? <span>paid {money(st.paid)}</span> : null}
          {st.unreconciled ? (
            <span className="over" title="done at a firm price with no expense row behind it">
              ⚑ {money(st.unreconciled)} unreconciled
            </span>
          ) : null}
        </div>
      ) : null}
      {(st.tasks || []).map((n) => <WorkNode key={n.id} n={n} depth={0} />)}
    </div>
  );
}

// WorkNode recurses. The old inspector rendered depth-1 only, so every nested
// task under a milestone was invisible — the tree looked emptier than it was.
function WorkNode({ n, depth }) {
  if (n.decision) return null; // decisions are hoisted into their own lane
  // WorkNode marshals FLAT (realestate/work.go MarshalJSON): text/checked/
  // owner sit at the top level, there is no nested task object.
  const done = n.checked;
  return (
    <>
      <div className={"ooda-node" + (done ? " done" : "") + (n.milestone ? " milestone" : "")}
        style={{ paddingLeft: 14 + depth * 14 }}>
        <span className="ooda-node-mark">{done ? "✓" : n.milestone ? "◆" : "○"}</span>
        <span className="ooda-node-text">{n.text}</span>
        {n.estTotal ? <span className="ooda-est">{money(n.estTotal)}</span> : null}
        <span className="ooda-sub">{orDash(n.owner)}</span>
      </div>
      {(n.children || []).map((c) => <WorkNode key={c.id} n={c} depth={depth + 1} />)}
    </>
  );
}

// DecisionsLane — open decisions hoisted out of the rock tree, because a
// decision nobody has made is blocking somebody, and buried three levels deep
// in a stage nobody expands it may as well not exist.
function DecisionsLane({ work }) {
  const [showClosed, setShowClosed] = React.useState(false);
  const open = [], closed = [];
  const walk = (nodes, rock) => (nodes || []).forEach((n) => {
    if (n.decision) {
      (n.resolution || n.checked ? closed : open).push(
        { id: n.id, text: n.text, rock, resolution: n.resolution, owner: n.owner });
    }
    walk(n.children, rock);
  });
  (work || []).forEach((st) => walk(st.tasks, st.text));
  if (!open.length && !closed.length) return null;

  return (
    <Section title="DECISIONS" count={open.length}>
      {!open.length ? <Empty>nothing open to decide here</Empty> : null}
      {open.map((dn) => (
        <div key={dn.id} className="ooda-row cols-dec">
          <span className="ooda-stack">
            <b>◇ {dn.text}</b>
            <em>{orDash(dn.rock)}</em>
          </span>
          <span className="ooda-sub">{orDash(dn.owner)}</span>
        </div>
      ))}
      {closed.length ? (
        <div className="ooda-more" role="button" onClick={() => setShowClosed(!showClosed)}>
          {(showClosed ? "hide" : "show") + " decided · " + closed.length}
        </div>
      ) : null}
      {showClosed ? closed.map((dn) => (
        <div key={dn.id} className="ooda-row cols-dec ooda-decided">
          <span className="ooda-stack">
            <b>{dn.text}</b>
            <em>{[dn.rock, dn.resolution].filter(Boolean).join(" · ") || DASH}</em>
          </span>
          <span className="ooda-sub">{orDash(dn.owner)}</span>
        </div>
      )) : null}
    </Section>
  );
}


// Thread — comments on any item. ANY signed-in member may comment (the shared
// layer's rule, unchanged); the trail is append-only in the team store and
// never touches the vault.
function Thread({ itemID, title }) {
  const [items, setItems] = React.useState(null);
  const [text, setText] = React.useState("");
  const [err, setErr] = React.useState("");
  const [busy, setBusy] = React.useState(false);

  const load = React.useCallback(() => {
    teamAPI.state()
      .then((st) => setItems(teamComments(st, itemID)))
      .catch((e) => setErr(String(e.message || e)));
  }, [itemID]);
  React.useEffect(() => { setItems(null); setErr(""); load(); }, [load]);

  const send = async () => {
    const body = text.trim();
    if (!body || busy) return;
    setBusy(true); setErr("");
    try {
      await teamAPI.comment(itemID, body);
      setText("");
      load();
    } catch (e) { setErr(String(e.message || e)); }
    setBusy(false);
  };

  return (
    <Section title="THREAD" count={items ? items.length : null}>
      {err ? <div className="ooda-err">{err}</div> : null}
      {items && !items.length ? <Empty>no comments yet</Empty> : null}
      {(items || []).map((c) => (
        <div key={c.id} className="ooda-comment">
          <div className="ooda-comment-head">
            <b>{c.author_name || c.author}</b>
            <span className="ooda-sub">{String(c.at || "").slice(0, 10)}</span>
          </div>
          <div className="ooda-comment-body">{c.text}</div>
        </div>
      ))}
      <div className="ooda-compose">
        <textarea className="ooda-textarea" rows={2} value={text}
          placeholder={"comment on " + (title || "this")}
          onChange={(e) => setText(e.target.value)}
          onKeyDown={(e) => { if (e.key === "Enter" && (e.metaKey || e.ctrlKey)) send(); }} />
        <button className="ooda-send" onClick={send} disabled={busy || !text.trim()}>
          {busy ? "…" : "comment"}
        </button>
      </div>
    </Section>
  );
}

// BidForm — a partner files a bid. It lands as a PROPOSAL, not a contract:
// only Benjamin can materialize a contract record, so the form says so plainly
// rather than implying the money is committed.
function BidForm({ slug, property, onFiled, note }) {
  const [open, setOpen] = React.useState(false);
  const [f, setF] = React.useState({ contractor: "", amount: "", scope: "", workId: "", expires: "" });
  const [err, setErr] = React.useState("");
  const [busy, setBusy] = React.useState(false);
  const set = (k) => (e) => setF({ ...f, [k]: e.target.value });

  const rocks = [];
  (property.work || []).forEach((st) => {
    rocks.push([st.id, st.text]);
    (st.tasks || []).forEach((n) => rocks.push([n.id, "· " + n.text]));
  });

  const file = async () => {
    const amount = parseFloat(f.amount);
    if (!f.contractor.trim() || !amount) { setErr("a contractor and an amount are required"); return; }
    setBusy(true); setErr("");
    try {
      await teamAPI.bid({
        property: slug, contractor: f.contractor.trim(), amount,
        workId: f.workId, scope: f.scope.trim(), expires: f.expires,
      });
      setF({ contractor: "", amount: "", scope: "", workId: "", expires: "" });
      setOpen(false);
      if (onFiled) onFiled();
    } catch (e) { setErr(String(e.message || e)); }
    setBusy(false);
  };

  if (!open) {
    return (
      <div className="ooda-bid-open">
        <button className="ooda-ghost" onClick={() => setOpen(true)}>＋ bid</button>
        {note ? <span className="ooda-sub ooda-bid-note">{note}</span> : null}
      </div>
    );
  }
  return (
    <Section title="FILE A BID">
      <div className="ooda-form">
        <label><em>CONTRACTOR</em>
          <input className="ooda-in" value={f.contractor} onChange={set("contractor")} placeholder="who is bidding" /></label>
        <label><em>AMOUNT</em>
          <input className="ooda-in" type="number" step="0.01" value={f.amount} onChange={set("amount")} placeholder="$" /></label>
        <label><em>AGAINST</em>
          <select className="ooda-in" value={f.workId} onChange={set("workId")}>
            <option value="">— no rock —</option>
            {rocks.map(([id, text]) => <option key={id} value={id}>{text}</option>)}
          </select></label>
        <label><em>EXPIRES</em>
          <input className="ooda-in" type="date" value={f.expires} onChange={set("expires")} /></label>
        <label className="wide"><em>SCOPE</em>
          <textarea className="ooda-textarea" rows={2} value={f.scope} onChange={set("scope")}
            placeholder="what the bid covers" /></label>
      </div>
      {err ? <div className="ooda-err">{err}</div> : null}
      <div className="ooda-form-note">
        Filing sends this to Benjamin for approval — it does not commit money.
        Approved bids become real contracts and appear here on the next refresh.
      </div>
      <div className="ooda-form-acts">
        <button className="ooda-ghost" onClick={() => setOpen(false)}>cancel</button>
        <button className="ooda-send" onClick={file} disabled={busy}>{busy ? "…" : "file bid"}</button>
      </div>
    </Section>
  );
}

// Underwriting — read-only parity with the cockpit's panel.
//
// READ-ONLY is doctrine, not an omission: ARCHITECTURE §12 says a portal write
// lands in the derived team store, never the vault. The cockpit's unit-mix and
// measurables editors POST straight through vaultwriter, so they cannot come
// along. A partner sees every input and every output and changes none of them.
//
// The screening math is the SHARED reScreen() (src/re-screening.js, byte-
// identical to the cockpit's copy), so these numbers cannot drift from the
// ones Benjamin reads.
function Underwriting({ p, source, assumptions }) {
  const a = (assumptions && assumptions.values) || {};
  const labels = (assumptions && assumptions.labels) || {};
  const keys = (assumptions && assumptions.keys) || [];
  const src = source || {};
  const uw = (typeof reScreen === "function") ? reScreen(p, src, a) : { complete: false };
  const mix = p.unitMix || [];
  const meas = p.measurables || {};
  const measKeys = Object.keys(meas);

  // which assumptions this property overrides, from the derived index
  const overridden = {};
  ((assumptions && assumptions.overrides) || []).forEach((o) => {
    if (o && o.kind === "property" && o.slug === p.slug) overridden[o.key] = o.value;
  });

  const pct = (v) => (v == null ? DASH : Math.round(v * 1000) / 10 + "%");

  return (
    <Section title="UNDERWRITING">
      {p.locked ? (
        <div className="ooda-sub" style={{ paddingBottom: 6 }}>
          {"estimates locked " + p.locked + " — the figures below are today's; " +
            "the frozen set is what the deal was underwritten against"}
        </div>
      ) : null}

      {/* what it IS */}
      <div className="ooda-uw-grid">
        <span><em>UNITS</em><b>{(mix.length || p.units) || DASH}</b></span>
        <span><em>RENT / MO</em><b>{money(p.rentMonthly)}</b></span>
        <span><em>KIND</em><b>{orDash(p.kind)}</b></span>
        <span><em>STATUS</em><b>{statusLabel(p.status)}</b></span>
      </div>

      {mix.length ? (
        <>
          <div className="ooda-row ooda-head-row cols-mix">
            <span>UNIT</span><span className="r">BD</span><span className="r">BA</span>
            <span className="r">SQFT</span><span className="r">RENT / MO</span>
          </div>
          {mix.map((u, i) => (
            <div key={i} className="ooda-row cols-mix">
              <span>{orDash(u.label)}</span>
              <span className="r">{u.beds || DASH}</span>
              <span className="r">{u.baths || DASH}</span>
              <span className="r">{u.sqft || DASH}</span>
              <span className="r">{money(u.rent)}</span>
            </div>
          ))}
        </>
      ) : <div className="ooda-sub">no measured unit mix — the source sidecar's totals are used</div>}

      {measKeys.length ? (
        <div className="ooda-chips" style={{ paddingTop: 8 }}>
          {measKeys.map((k) => (
            <span key={k} className="ooda-meas">{k.replace(/_/g, " ")} <b>{meas[k]}</b></span>
          ))}
        </div>
      ) : null}

      {/* what it COSTS — the same plan figures behind the BUDGET tile */}
      <div className="ooda-uw-grid" style={{ marginTop: 12 }}>
        <span><em>PURCHASE</em><b>{money(uw.purchase)}</b></span>
        <span><em>CLOSING</em><b>{money(uw.closing)}</b></span>
        <span>
          <em>HARD{uw.hardFromWork ? " · Σ ROCKS" : ""}</em>
          <b>{money(uw.hard)}</b>
        </span>
        <span><em>SOFT / CARRY</em><b>{money(uw.soft)}</b></span>
        <span><em>CONTINGENCY</em><b>{money(uw.contingency)}</b></span>
        <span><em>TOTAL COST</em><b>{money(uw.tdc)}</b></span>
      </div>

      {/* what it RETURNS */}
      {uw.complete ? (
        <>
          <div className="ooda-uw-grid" style={{ marginTop: 12 }}>
            <span><em>NOI</em><b>{money(uw.noi)}</b></span>
            <span><em>ARV</em><b>{money(uw.arv)}</b></span>
            <span><em>DSCR</em><b className={uw.dscr && uw.dscr < 1.2 ? "over" : ""}>
              {uw.dscr ? Math.round(uw.dscr * 100) / 100 : DASH}</b></span>
            <span><em>TAKEOUT LOAN</em><b>{money(uw.loan)}</b></span>
          </div>
          {uw.refiGap > 0 ? (
            <div className="ooda-note">
              {money(uw.refiGap) + " refi gap — the construction loan is bigger than " +
                "the appraisal supports at " + pct(a.perm_ltv) + " LTV, so that much " +
                "equity stays in the deal."}
            </div>
          ) : null}
        </>
      ) : (
        <div className="ooda-sub" style={{ paddingTop: 10 }}>
          returns need a unit count and a rent — neither the unit mix nor the
          source sidecar has both yet
        </div>
      )}

      {/* the policy the returns were computed against */}
      {keys.length ? (
        <>
          <div className="ooda-sub" style={{ paddingTop: 12 }}>ASSUMPTIONS</div>
          <div className="ooda-chips">
            {keys.map((k) => (
              <span key={k} className={"ooda-assum" + (overridden[k] != null ? " override" : "")}
                title={(labels[k] || k) + (overridden[k] != null ? " — overridden on this property" : " — portfolio default")}>
                {(labels[k] || k.replace(/_/g, " "))} <b>
                  {overridden[k] != null ? overridden[k] : (a[k] != null ? a[k] : DASH)}
                </b>
              </span>
            ))}
          </div>
        </>
      ) : null}
    </Section>
  );
}

// OwnerCard — who holds title. `acq === "owned"` is the CLOSED test; control
// is set at signing, so reading it here would name the entity as owner of
// parcels it has only agreed to buy.
function OwnerCard({ p, f }) {
  const intel = p.intel || {};
  const deed = p.owner || intel.owner || "";
  return (
    <Section title="OWNER">
      {f.acq === "owned" && p.entity ? (
        <>
          <div className="ooda-owner-name">{p.entity}</div>
          {deed && deed.toLowerCase() !== String(p.entity).toLowerCase() ? (
            <div className="ooda-sub over">{"deed: " + deed}</div>
          ) : null}
          {p.ownerSince ? <div className="ooda-sub">{"since " + p.ownerSince}</div> : null}
        </>
      ) : (
        <>
          {f.acq === "under-contract" && p.entity ? (
            <div className="ooda-sub">{"under contract to " + p.entity}</div>
          ) : null}
          <div className="ooda-owner-name">{deed || "no owner on record"}</div>
          {(p.ownerAddr || intel.ownerAddr) ? (
            <div className="ooda-sub">{p.ownerAddr || intel.ownerAddr}</div>
          ) : null}
          {(p.ownerSince || intel.saleDate) ? (
            <div className="ooda-sub">{"theirs since " + (p.ownerSince || intel.saleDate)}</div>
          ) : null}
        </>
      )}
      {intel.taxStatus ? (
        <div className={"ooda-sub" + (intel.taxStatus === "delinquent" ? " over" : "")}>
          {intel.taxStatus === "delinquent"
            ? "taxes DELINQUENT · " + money(intel.taxBalDue) + " owed"
            : intel.taxStatus === "lra" ? "LRA land-bank" : "taxes current"}
          {intel.assessed ? " · assessed " + money(intel.assessed) : ""}
        </div>
      ) : null}
    </Section>
  );
}
