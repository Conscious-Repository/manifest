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
    </div>
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
