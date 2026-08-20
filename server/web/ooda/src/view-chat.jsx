// CHAT — ask zeck. The agent's ceiling is the same as kairos's: it reads what
// manifest hands it and PROPOSES; a person approves. It never writes.
//
// Grounding is by ID, never by prose: the client attaches a property and the
// SERVER resolves it into real content for the work order. That is what lets
// zeck answer real questions while having no access to the vault at all.

function ViewChat({ data }) {
  const [state, setState] = React.useState(null);
  const [sel, setSel] = React.useState("");
  const [text, setText] = React.useState("");
  const [ritual, setRitual] = React.useState("ask");
  const [ctx, setCtx] = React.useState([]);
  const [err, setErr] = React.useState("");
  const [busy, setBusy] = React.useState(false);

  const load = React.useCallback(() => {
    getJSON("/api/chat/threads")
      .then((d) => {
        setState(d);
        setSel((cur) => cur || ((d.threads || [])[0] || {}).id || "");
      })
      .catch((e) => setErr(String(e.message || e)));
  }, []);
  React.useEffect(() => { load(); }, [load]);
  // a spooled run finishes out of band — poll while a turn is in flight
  React.useEffect(() => {
    const eng = (state && state.engine) || {};
    if (!eng.active && !(eng.pending || []).length) return;
    const h = setTimeout(load, 5000);
    return () => clearTimeout(h);
  }, [state, load]);

  if (err && !state) return <Empty>{err}</Empty>;
  if (!state) return <Empty>loading…</Empty>;

  const threads = (state.threads || []).filter((t) => !t.archived);
  const msgs = (state.messages || {})[sel] || [];
  const engine = state.engine || {};
  const noAgent = !engine.harness;

  const newThread = async () => {
    const title = (prompt("thread title") || "").trim();
    if (!title) return;
    try {
      const d = await postJSON("/api/chat/thread", { op: "create", id: "t" + Date.now(), title });
      setState(d); setSel(((d.threads || [])[0] || {}).id || "");
    } catch (e) { setErr(String(e.message || e)); }
  };

  const send = async () => {
    const body = text.trim();
    if (!body || !sel || busy) return;
    setBusy(true); setErr("");
    try {
      await postJSON("/api/chat/ask", { thread: sel, text: body, ritual, context: ctx });
      setText("");
      load();
    } catch (e) { setErr(String(e.message || e)); }
    setBusy(false);
  };

  const props = (data.portfolio && data.portfolio.properties) || [];

  return (
    <div className="ooda-split">
      <div className="ooda-list">
        {noAgent ? (
          <div className="ooda-stale">zeck is not configured on this box yet — threads still work</div>
        ) : null}
        <div className="ooda-sec-head">
          <span className="ooda-sec-title">THREADS</span>
          <button className="ooda-ghost" onClick={newThread}>＋ thread</button>
        </div>
        {!threads.length ? <Empty>no threads yet</Empty> : null}
        {threads.map((t) => (
          <div key={t.id} className={"ooda-row cols-thread click" + (sel === t.id ? " sel" : "")}
            onClick={() => setSel(t.id)} role="button">
            <span>{t.title || t.id}</span>
            <span className="r ooda-sub">{((state.messages || {})[t.id] || []).length}</span>
          </div>
        ))}

        {sel ? (
          <Section title="CONVERSATION" count={msgs.length}>
            {!msgs.length ? <Empty>ask zeck something about the portfolio</Empty> : null}
            {msgs.map((m, i) => (
              <div key={i} className={"ooda-msg " + (m.kind === "ask" ? "mine" : "agent")}>
                <div className="ooda-comment-head">
                  <b>{m.authName || m.author}</b>
                  <span className="ooda-sub">{String(m.at || "").slice(11, 16)}</span>
                </div>
                <div className="ooda-comment-body">{m.text}</div>
                {(m.props || []).length ? (
                  <div className="ooda-sub">{m.props.length} proposal(s) — approve in the cockpit</div>
                ) : null}
              </div>
            ))}
            <div className="ooda-chat-ctx">
              <span className="ooda-sub">ground it in:</span>
              <select className="ooda-in" value=""
                onChange={(e) => { if (e.target.value) setCtx([...new Set([...ctx, e.target.value])]); }}>
                <option value="">＋ a property</option>
                {props.map((p) => <option key={p.slug} value={"prop/" + p.slug}>{p.short}</option>)}
              </select>
              {ctx.map((c) => (
                <button key={c} className="ooda-chip on"
                  onClick={() => setCtx(ctx.filter((x) => x !== c))}>{c.replace("prop/", "")} ✕</button>
              ))}
            </div>
            <div className="ooda-compose">
              <textarea className="ooda-textarea" rows={2} value={text}
                placeholder={ritual === "ask" ? "ask zeck (read-only)" : "ask zeck to draft changes for approval"}
                onChange={(e) => setText(e.target.value)}
                onKeyDown={(e) => { if (e.key === "Enter" && (e.metaKey || e.ctrlKey)) send(); }} />
              <select className="ooda-in" value={ritual} onChange={(e) => setRitual(e.target.value)}>
                <option value="ask">ask</option>
                <option value="delegate">delegate</option>
              </select>
              <button className="ooda-send" onClick={send} disabled={busy || !text.trim()}>
                {busy ? "…" : "send"}
              </button>
            </div>
            {err ? <div className="ooda-err">{err}</div> : null}
          </Section>
        ) : null}
      </div>

      <aside className="ooda-insp">
        <div className="ooda-sec-head"><span className="ooda-sec-title">ENGINE</span></div>
        <div className="ooda-engine">
          <div><em>AGENT</em><b>{orDash(engine.harness)}</b></div>
          <div><em>HOST</em><b>{orDash(engine.host)}</b></div>
          <div><em>MODEL</em><b>{orDash(engine.model)}</b></div>
          <div><em>HEARTBEAT</em>
            <b className={engine.live ? "" : "over"}>
              {engine.live ? (engine.beat >= 0 ? engine.beat + "s ago" : "live") : "not running"}
            </b></div>
          <div><em>IN FLIGHT</em><b>{engine.active ? (engine.active.ritual || "run") : DASH}</b></div>
          <div><em>QUEUED</em><b>{(engine.pending || []).length || DASH}</b></div>
        </div>
        <div className="ooda-form-note">
          zeck reads only what the server hands it — it has no access to the
          vault. On <b>delegate</b> it returns proposals; a person applies them.
        </div>
      </aside>
    </div>
  );
}
