// CHAT — ask zeck. The agent's ceiling is the same as kairos's: it reads what
// manifest hands it and PROPOSES; a person approves. It never writes.
//
// Grounding is by ID, never by prose: the client attaches a property and the
// SERVER resolves it into real content for the work order. That is what lets
// zeck answer real questions while having no access to the vault at all.

function ViewChat({ data }) {
  const me = (data && data.me) || {};
  const canAct = !!(me.admin || me.canFire); // the server gates too (OodaChatProposal)
  const [state, setState] = React.useState(null);
  const [sel, setSel] = React.useState("");
  const [text, setText] = React.useState("");
  const [propOpen, setPropOpen] = React.useState(null);
  const [attachMeta, setAttachMeta] = React.useState({});
  const [attaching, setAttaching] = React.useState("");
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

  // send(key) — labelled by OUTPUT; ritualOf maps to the wire value.
  const send = async (key) => {
    const body = text.trim();
    if (!body || !sel || busy) return;
    const ritual = window.CHAT_ACTIONS.ritualOf(key);
    setBusy(true); setErr("");
    try {
      await postJSON("/api/chat/ask", { thread: sel, text: body, ritual, context: ctx });
      setText(""); load();
    } catch (e) { setErr(String(e.message || e)); }
    setBusy(false);
  };

  // ATTACHMENTS — upload, then the hash rides the context array with the
  // property chips, so the send path is unchanged.
  const attachFile = async (file) => {
    if (!file || !sel) return;
    const A = window.CHAT_ACTIONS;
    setAttaching(A.attach.uploading(file.name)); setErr("");
    try {
      const r = await fetch("/api/chat/attach?thread=" + encodeURIComponent(sel) +
        "&name=" + encodeURIComponent(file.name), { method: "POST", body: file });
      if (!r.ok) throw new Error((await r.text()).trim());
      const d = await r.json();
      setAttachMeta((m) => ({ ...m, [d.id]: d.file }));
      setCtx((c) => (c.indexOf(d.id) < 0 ? c.concat([d.id]) : c));
    } catch (e) {
      setErr(A.attach.failed(file.name, String(e.message || e).slice(0, 140)));
    }
    setAttaching("");
  };

  // decide applies or discards one proposal — the endpoint has always been
  // wired for this portal (main.go OodaChatProposal); the page just never used it.
  const decide = (m, idx, apply) =>
    postJSON("/api/chat/proposal", { thread: sel, msg: m.id, index: idx, apply })
      .then(load)
      .catch((e) => setErr(String(e.message || e)));

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
                  <b>{m.author_name || m.author}</b>
                  <span className="ooda-sub">{String(m.at || "").slice(11, 16)}</span>
                </div>
                <div className="ooda-comment-body">{m.text}</div>
                {(m.files || []).length ? (
                  <div className="ooda-msg-files">
                    {m.files.map((f) => (
                      <a key={f.hash} className="ooda-chip"
                        href={"/api/chat/attach/" + encodeURIComponent(f.hash)}
                        target="_blank" rel="noopener">{f.name}</a>
                    ))}
                  </div>
                ) : null}
                {(m.proposals || []).map((p, idx) => {
                  const key = m.id + "#" + idx;
                  const open = propOpen === key;
                  return (
                    <div key={idx} className={"ooda-prop " + p.state}>
                      <div className="ooda-prop-head">
                        <em>PROPOSES</em>
                        <b>{p.verb}</b>
                        <span className="r ooda-sub">{p.state}</span>
                      </div>
                      <div className="ooda-prop-target">{p.target || p.item}</div>
                      {(p.body || p.value) ? (
                        <button className="ooda-prop-toggle"
                          onClick={() => setPropOpen(open ? null : key)}>
                          {open ? "hide" : (p.type === "replace-section" ? "show the plan" : "show the change")}
                        </button>
                      ) : null}
                      {open ? (
                        <div className="ooda-prop-body">
                          {p.section ? <em>## {p.section}</em> : null}
                          <pre>{p.body || (p.field + " → " + p.value)}</pre>
                          {p.was ? <div className="ooda-sub">replaces · {p.was}</div> : null}
                        </div>
                      ) : null}
                      {p.state === "pending" && canAct ? (
                        <div className="ooda-prop-acts">
                          <button className="ooda-send" onClick={() => decide(m, idx, true)}>apply</button>
                          <button className="ooda-send secondary" onClick={() => decide(m, idx, false)}>discard</button>
                        </div>
                      ) : null}
                      {p.state === "pending" && !canAct ? (
                        <div className="ooda-sub">an admin applies this</div>
                      ) : null}
                    </div>
                  );
                })}
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
                  onClick={() => setCtx(ctx.filter((x) => x !== c))}>
                  {window.CHAT_ACTIONS.isAttach(c)
                    ? ((attachMeta[c] || {}).name || "attachment")
                    : c.replace("prop/", "")} ✕
                </button>
              ))}
            </div>
            <div className="ooda-compose">
              <textarea className="ooda-textarea" rows={2} value={text}
                placeholder={window.CHAT_ACTIONS.placeholder}
                onChange={(e) => setText(e.target.value)}
                onKeyDown={(e) => {
                  if (e.key === "Enter" && (e.metaKey || e.ctrlKey)) send(e.shiftKey ? "delegate" : "ask");
                }} />
            </div>
            <div className="ooda-compose-acts">
              <button className="ooda-send" onClick={() => send("ask")} disabled={busy || !text.trim()}>
                {busy ? "…" : window.CHAT_ACTIONS.ask.label}
              </button>
              <button className="ooda-send secondary" onClick={() => send("delegate")} disabled={busy || !text.trim()}>
                {window.CHAT_ACTIONS.propose.label}
              </button>
              <label className="ooda-attach" title={window.CHAT_ACTIONS.attach.hint}>
                {attaching ? "…" : window.CHAT_ACTIONS.attach.label}
                <input type="file" accept={window.CHAT_ACTIONS.attach.accept}
                  disabled={!!attaching || !sel}
                  onChange={(e) => { const f = e.target.files[0]; e.target.value = ""; attachFile(f); }} />
              </label>
            </div>
            <div className="ooda-form-note">
              {window.CHAT_ACTIONS.ask.label} — {window.CHAT_ACTIONS.ask.sub}<br />
              {window.CHAT_ACTIONS.propose.label} — {window.CHAT_ACTIONS.propose.sub}
            </div>
            <div className="ooda-assure">{window.CHAT_ACTIONS.assurance}</div>
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
          vault. <b>send &amp; propose</b> comes back as proposals you apply or
          discard on the message; nothing reaches the records until you do.
        </div>
      </aside>
    </div>
  );
}
