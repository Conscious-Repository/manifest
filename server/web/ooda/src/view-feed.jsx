// FEED — the member's inbox (ooda-portal email plan, phase 5). Three lanes:
// the Gmail connection banner, PENDING EMAIL notes from YOUR mailbox
// (confirm → shared artifact + extraction; dismiss → muted forever), and
// APPROVALS — what the extractor proposed, filtered server-side to what you
// may see, with Confirm live only on what you may decide. The admin sees
// everything; the server's predicate is the truth, this view only renders it.

function useOodaFeed() {
  const [state, setState] = React.useState({ loading: true, error: "" });
  const load = React.useCallback(async () => {
    try {
      const feed = await getJSON("/api/ooda/feed");
      setState({ loading: false, error: "", feed });
    } catch (e) {
      setState({ loading: false, error: String(e.message || e) });
    }
  }, []);
  React.useEffect(() => { load(); }, [load]);
  return [state, load];
}

function GmailBanner({ gmail }) {
  if (!gmail) return null;
  if (gmail.connected) return null;
  return (
    <div className="ooda-gmail-banner">
      <span>
        {gmail.needsReauth
          ? "your Gmail connection lapsed — sign in again to reconnect"
          : "connect your Gmail to capture deal threads — it rides your sign-in"}
      </span>
      <a className="ooda-send" href="/oauth2/login">
        {gmail.needsReauth ? "reconnect" : "connect gmail"}
      </a>
    </div>
  );
}

// PendingEmail — one candidate: subject + participants + the rendered note
// (collapsed), confirm/dismiss. Dismiss warns in the label: it mutes forever.
function PendingEmail({ cand, admin, onDecide }) {
  const [open, setOpen] = React.useState(false);
  const [busy, setBusy] = React.useState("");
  const [err, setErr] = React.useState("");
  const decide = async (action) => {
    setBusy(action); setErr("");
    try { await postJSON("/api/ooda/email/" + cand.id, { action }); onDecide(); }
    catch (e) { setErr(String(e.message || e)); }
    setBusy("");
  };
  return (
    <div className="ooda-card">
      <div className="ooda-card-head">
        <span className="ooda-chip">email</span>
        <span className="ooda-card-title">{cand.subject || cand.filename}</span>
        {admin ? <span className="ooda-sub">{cand.account}</span> : null}
      </div>
      <div className="ooda-sub">
        {(cand.participants || []).join(" · ") || DASH}
        {" · "}{(cand.lastMsgAt || "").slice(0, 10)}
        {cand.seq > 1 ? " · thread grew after an earlier confirm" : ""}
      </div>
      <button className="ooda-linkish" onClick={() => setOpen(!open)}>
        {open ? "hide the note" : "read the note"}
      </button>
      {open ? <pre className="ooda-note">{cand.note}</pre> : null}
      {err ? <div className="ooda-err">{err}</div> : null}
      <div className="ooda-card-actions">
        <button className="ooda-send" disabled={!!busy} onClick={() => decide("confirm")}>
          {busy === "confirm" ? "saving…" : "confirm — archive it"}
        </button>
        <button className="ooda-quiet" disabled={!!busy} onClick={() => decide("dismiss")}>
          {busy === "dismiss" ? "…" : "dismiss — never ask about this thread again"}
        </button>
      </div>
    </div>
  );
}

// BacklogCard — a proposed task/decision, one line + evidence. Decidable →
// confirm/reject; visible-only → the line and who it is for.
function BacklogCard({ row, onDecide }) {
  const p = row.aionPayload || {};
  const [busy, setBusy] = React.useState("");
  const [err, setErr] = React.useState("");
  const act = async (action) => {
    setBusy(action); setErr("");
    try { await postJSON("/api/ooda/approval/" + row.id, { action }); onDecide(); }
    catch (e) { setErr(String(e.message || e)); }
    setBusy("");
  };
  return (
    <div className="ooda-card">
      <div className="ooda-card-head">
        <span className="ooda-chip">{p.kind || "task"}</span>
        <span className="ooda-card-title">{p.title || row.action}</span>
        <span className="ooda-sub">{p.owner ? "@" + p.owner : ""}{p.due ? " · due " + p.due : ""}</span>
      </div>
      <Evidence body={row.body} />
      {err ? <div className="ooda-err">{err}</div> : null}
      {row.decidable ? (
        <div className="ooda-card-actions">
          <button className="ooda-send" disabled={!!busy} onClick={() => act("confirm")}>
            {busy === "confirm" ? "applying…" : "confirm — add to the backlog"}
          </button>
          <button className="ooda-quiet" disabled={!!busy} onClick={() => act("reject")}>reject</button>
        </div>
      ) : (
        <div className="ooda-sub">not yours to decide — shown because it touches your work</div>
      )}
    </div>
  );
}

// ContractCard — the money card, read-mostly: total headline, the split with
// a live Σ gate, terms collapsed. Editable ONLY when decidable, and only the
// amounts — structural edits stay in the owner's cockpit.
function ContractCard({ row, onDecide }) {
  const p0 = row.reContractPayload || {};
  const [p, setP] = React.useState(p0);
  const [busy, setBusy] = React.useState("");
  const [err, setErr] = React.useState("");
  const [dirty, setDirty] = React.useState(false);

  const allocs = p.allocations || [];
  const sum = allocs.reduce((n, a) => n + (Number(a.amount) || 0), 0);
  const reconciles = Math.abs(sum - (Number(p.total) || 0)) <= 0.01;

  const setAmount = (i, v) => {
    const next = { ...p, allocations: allocs.map((a, j) => j === i ? { ...a, amount: Number(v) || 0 } : a) };
    setP(next); setDirty(true);
  };
  const setTotal = (v) => { setP({ ...p, total: Number(v) || 0 }); setDirty(true); };

  const save = async () => {
    setBusy("save"); setErr("");
    try { await postJSON("/api/ooda/approval/" + row.id + "/recontract", p); setDirty(false); }
    catch (e) { setErr(String(e.message || e)); }
    setBusy("");
  };
  const act = async (action) => {
    setBusy(action); setErr("");
    try {
      if (action === "confirm" && dirty) await postJSON("/api/ooda/approval/" + row.id + "/recontract", p);
      await postJSON("/api/ooda/approval/" + row.id, { action });
      onDecide();
    } catch (e) { setErr(String(e.message || e)); }
    setBusy("");
  };

  return (
    <div className="ooda-card ooda-money-card">
      <div className="ooda-card-head">
        <span className="ooda-chip money">{p.kind || "contract"}</span>
        <span className="ooda-card-title">{p.name || row.action}</span>
      </div>
      <div className="ooda-money-total">
        {row.decidable ? (
          <input className="ooda-amt-in big" type="number" value={p.total || ""}
            onChange={(e) => setTotal(e.target.value)} />
        ) : (
          <span className="ooda-money-big">{money(p.total)}</span>
        )}
        <span className="ooda-sub">
          {orDash(p.contractor || p.contractor_create)}
          {p.date ? " · " + p.date : ""}{p.expires ? " · expires " + p.expires : ""}
        </span>
      </div>
      <div className="ooda-split">
        {allocs.map((a, i) => (
          <div className="ooda-split-row" key={i}>
            <span className="ooda-split-prop">{a.property}</span>
            <span className="ooda-sub">{a.node}</span>
            {row.decidable ? (
              <input className="ooda-amt-in" type="number" value={a.amount || ""}
                onChange={(e) => setAmount(i, e.target.value)} />
            ) : (
              <span className="ooda-split-amt">{money(a.amount)}</span>
            )}
          </div>
        ))}
        <div className={"ooda-sub " + (reconciles ? "" : "over")}>
          {reconciles ? "split reconciles" : "Σ " + money(sum) + " ≠ total " + money(p.total) + " — fix before confirm"}
        </div>
      </div>
      <Evidence body={row.body} terms={p} />
      {err ? <div className="ooda-err">{err}</div> : null}
      {row.decidable ? (
        <div className="ooda-card-actions">
          <button className="ooda-send" disabled={!!busy || !reconciles} onClick={() => act("confirm")}>
            {busy === "confirm" ? "applying…" : "confirm — commit " + money(p.total)}
          </button>
          {dirty ? (
            <button className="ooda-quiet" disabled={!!busy} onClick={save}>
              {busy === "save" ? "saving…" : "save edits"}
            </button>
          ) : null}
          <button className="ooda-quiet" disabled={!!busy} onClick={() => act("reject")}>reject</button>
        </div>
      ) : (
        <div className="ooda-sub">
          proposed money on the books — only the owner (or a partner on every
          allocated property's entity) can commit it
        </div>
      )}
    </div>
  );
}

// Evidence — the proposal body minus its payload fence, collapsed by default,
// plus the contract prose lists when present.
function Evidence({ body, terms }) {
  const [open, setOpen] = React.useState(false);
  const text = (body || "").split("````")[0].trim();
  const lists = terms ? [
    ["terms", terms.terms], ["exclusions", terms.exclusions], ["risk", terms.risk_items],
  ].filter(([, l]) => l && l.length) : [];
  if (!text && !lists.length) return null;
  return (
    <div className="ooda-evidence">
      <button className="ooda-linkish" onClick={() => setOpen(!open)}>
        {open ? "hide the evidence" : "evidence & terms"}
      </button>
      {open ? (
        <div>
          {text ? <pre className="ooda-note">{text}</pre> : null}
          {lists.map(([label, items]) => (
            <div key={label}>
              <div className="ooda-mini-label">{label.toUpperCase()}</div>
              {items.map((it, i) => <div className="ooda-sub" key={i}>· {it}</div>)}
            </div>
          ))}
        </div>
      ) : null}
    </div>
  );
}

function ViewFeed() {
  const [state, reload] = useOodaFeed();
  if (state.loading) return <Empty>loading the feed…</Empty>;
  if (state.error) return <Empty>{"could not load: " + state.error}</Empty>;
  const feed = state.feed || {};
  const pending = feed.emailPending || [];
  const approvals = feed.approvals || [];
  const contracts = approvals.filter((a) => a.type === "re-contract");
  const backlog = approvals.filter((a) => a.type === "re-backlog");

  return (
    <>
      <GmailBanner gmail={feed.gmail} />
      <Section title={feed.admin ? "PENDING EMAIL — ALL MEMBERS" : "PENDING EMAIL — YOUR MAILBOX"} count={pending.length}>
        {pending.length ? pending.map((c) => (
          <PendingEmail key={c.id} cand={c} admin={feed.admin} onDecide={reload} />
        )) : <Empty>nothing waiting — deal threads land here after your mailbox syncs</Empty>}
      </Section>
      <Section title="PROPOSED MONEY" count={contracts.length}>
        {contracts.length ? contracts.map((r) => (
          <ContractCard key={r.id} row={r} onDecide={reload} />
        )) : <Empty>no money proposals</Empty>}
      </Section>
      <Section title="PROPOSED TASKS & DECISIONS" count={backlog.length}>
        {backlog.length ? backlog.map((r) => (
          <BacklogCard key={r.id} row={r} onDecide={reload} />
        )) : <Empty>no open proposals for you</Empty>}
      </Section>
    </>
  );
}
