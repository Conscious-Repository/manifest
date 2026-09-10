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
  // per-card decide feedback, keyed msgId#idx — the shared bottom error node
  // sits a full screen below a mid-thread card, which is how a refused apply
  // read as "nothing happens" (owner report 2026-08-25)
  const [decideErr, setDecideErr] = React.useState({});
  const [deciding, setDeciding] = React.useState("");
  const [attachMeta, setAttachMeta] = React.useState({});
  const [attaching, setAttaching] = React.useState("");
  const [ctx, setCtx] = React.useState([]);
  const [err, setErr] = React.useState("");
  const [loadError, setLoadError] = React.useState("");
  const [busy, setBusy] = React.useState(false);
  const loadSeq = React.useRef(0);
  const sending = React.useRef(false);
  const drafts = React.useRef({});
  // the surface's agent roster ({harness, personas}) feeds the @-mention list
  // — @zeck::brief is parsed server-side (chatIntent); this is the UI the
  // AION portal always had and this one lacked (agent-chat plan §1.1)
  const [agents, setAgents] = React.useState([]);
  React.useEffect(() => {
    getJSON("/api/team/agents").then((d) => setAgents((d && d.agents) || [])).catch(() => {});
  }, []);

  const load = React.useCallback(() => {
    const request = ++loadSeq.current;
    return getJSON("/api/chat/threads")
      .then((d) => {
        if (request !== loadSeq.current) return;
        setLoadError(""); setState(d);
        setSel((cur) => (d.threads || []).some(t => t.id === cur) ? cur : ((d.threads || [])[0] || {}).id || "");
      })
      .catch((e) => { if (request === loadSeq.current) setLoadError(String(e.message || e)); });
  }, []);
  React.useEffect(() => { load(); }, [load]);
  // a spooled run finishes out of band — poll fast (5s) while a turn is in
  // flight. Idle, the store still moves without you: a teammate's message, or
  // the reply to a run that started after your snapshot. Chat has its own
  // store, so it never rides the global revision poll — a slow 20s reload on
  // the same cadence (and the same visibility discipline) covers the idle case.
  React.useEffect(() => {
    const eng = (state && state.engine) || {};
    const busy = !!eng.active || !!(eng.pending || []).length;
    const h = setInterval(() => {
      if (document.visibilityState === "visible") load();
    }, busy ? 5000 : 20000);
    return () => clearInterval(h);
  }, [state, load]);
  // grounding is PER-MESSAGE: whatever was grounded in one thread must never
  // ride into another (brian, 2026-08-22 — 751 Bayard, grounded in a budget
  // thread, silently followed him into "monday sync topics"). Thread-local by
  // construction: any thread switch or creation starts from nothing grounded.
  React.useEffect(() => { setCtx([]); }, [sel]);

  const activeThread=((state && state.threads)||[]).find(t=>t.id===sel);
  const shared=window.SHARED_CHAT.useThread(activeThread,((state && state.messages)||{})[sel]||[],me);

  if (loadError && !state) return <div role="alert" className="ooda-err">{loadError} <button className="ooda-send" onClick={load}>Retry conversations</button></div>;
  if (!state) return <Empty>loading…</Empty>;

  const threads = (state.threads || []).filter((t) => !t.archived);
  const msgs = shared.messages;
  const engine = state.engine || {};
  const noAgent = !engine.harness;

  const newThread = async () => {
    if (sending.current) return;
    const title = (prompt("thread title") || "").trim();
    if (!title) return;
    try {
      const id = "t" + Date.now();
      const d = await postJSON("/api/chat/thread", { op: "create", id, title });
      drafts.current[sel] = text; setText("");
      setState(d); setSel(id);
    } catch (e) { setErr(String(e.message || e)); }
  };

  // send(key) — labelled by OUTPUT; ritualOf maps to the wire value.
  const send = async (key) => {
    const body = text.trim();
    if (!body || !sel || busy || sending.current) return;
    sending.current = true;
    const ritual = window.CHAT_ACTIONS.ritualOf(key);
    setBusy(true); setErr("");
    try {
      if(shared.shared && (!shared.ready || !shared.recipient)) throw Error('Choose the agent for this message.');
      if(shared.native) {
        if(ctx.length)throw Error('Remove the context chips before sending to this terminal.');
        await shared.send(body);
      } else await postJSON("/api/chat/ask", { thread: sel, text: body, ritual, context: ctx });
      // the grounding chips belonged to THAT message — the next one starts
      // clean, or asking again silently re-attaches a property the user no
      // longer sees themselves holding
      setText(current => current.trim() === body ? "" : current); setCtx([]); load(); shared.refresh();
    } catch (e) { setErr(String(e.message || e)); }
    sending.current = false; setBusy(false);
  };

  // mention list from the roster (@zeck + persona variants) — the AION
  // portal's rule verbatim: open while the draft ends in an @-word
  const mentionOpen = /@[^\s]*$/.test(text) && text.slice(-1) !== " ";
  const mentions = [];
  agents.forEach((a) => {
    mentions.push({ token: "@" + a.harness, note: "no intent tag · it decides how to answer" });
    (a.personas || []).forEach((p) => mentions.push({ token: "@" + a.harness + "::" + p, note: p }));
  });
  const pickMention = (tok) => setText((d) => d.replace(/@[^\s]*$/, tok + " "));

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
  const decide = (m, idx, apply) => {
    const key = m.id + "#" + idx;
    setDecideErr((d) => ({ ...d, [key]: "" }));
    setDeciding(key);
    postJSON("/api/chat/proposal", { thread: sel, msg: m.id, index: idx, apply })
      .then(() => { setDeciding(""); load(); })
      .catch((e) => {
        setDeciding("");
        setDecideErr((d) => ({ ...d, [key]: String(e.message || e) }));
      });
  };

  const props = (data.portfolio && data.portfolio.properties) || [];

  return (
    <div className="ooda-split">
      <div className="ooda-list">
        {noAgent && !shared.native ? (
          <div className="ooda-stale">zeck is not configured on this box yet — threads still work</div>
        ) : null}
        <div className="ooda-sec-head">
          <span className="ooda-sec-title">CONVERSATIONS</span>
          {sel && <button className="ooda-ghost" onClick={() => { const input = document.getElementById('ooda-chat-message'); if (input) { input.scrollIntoView({block:'center'}); input.focus(); } }}>Write a message ↓</button>}
          {loadError && <div role="alert" className="ooda-err">{loadError} <button onClick={load}>Retry conversations</button></div>}
          <button className="ooda-ghost" onClick={newThread}>＋ thread</button>
        </div>
        {!threads.length ? <Empty>no threads yet</Empty> : null}
        {threads.map((t) => (
          <div key={t.id} className={"ooda-row cols-thread click" + (sel === t.id ? " sel" : "")}
            onClick={() => { if (sending.current) return; drafts.current[sel] = text; setText(drafts.current[t.id] || ""); setSel(t.id); }} role="button" tabIndex={0} onKeyDown={event => { if (event.key === "Enter") event.currentTarget.click(); }}>
            <span>{t.title || t.id}</span>
            <span className="r ooda-sub">{((state.messages || {})[t.id] || []).length}</span>
          </div>
        ))}

        {sel ? (
          <Section title="CONVERSATION" count={msgs.length}>
            {!msgs.length ? <Empty>ask zeck something about the portfolio</Empty> : null}
            {shared.controls('Zeck','ooda-in',sent=>{setText(current=>current.trim()===sent?'':current);load();})}
            {msgs.map((m, i) => (
              <div key={i} className={"ooda-msg " + (m.kind === "ask" ? "mine" : "agent")}>
                <div className="ooda-comment-head">
                  <b>{m.author_name || m.author}</b>
                  <span className="ooda-sub">{m.source && !m.source.timestampKnown ? 'Time not recorded' : String(m.at || "").slice(11, 16)}</span>
                </div>
                <div className="ooda-comment-body">
                  {m.kind === "ask" ? m.text : window.CHAT_MD.render(m.text, React)}
                </div>
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
                          <button className="ooda-send" disabled={deciding === key}
                            onClick={() => decide(m, idx, true)}>{deciding === key ? "…" : "apply"}</button>
                          <button className="ooda-send secondary" disabled={deciding === key}
                            onClick={() => decide(m, idx, false)}>discard</button>
                        </div>
                      ) : null}
                      {decideErr[key] ? <div className="ooda-prop-err">{decideErr[key]}</div> : null}
                      {p.state === "pending" && !canAct ? (
                        <div className="ooda-sub">an admin applies this</div>
                      ) : null}
                    </div>
                  );
                })}
              </div>
            ))}
            {/* the composer is ONE unit: a grounding chip sits inside it,
                directly above the textarea, so it visibly belongs to the one
                message being written — it rides that send, clears with it,
                and never carries into another message or thread */}
            <div className="ooda-composer">
              <div className="ooda-chat-ctx">
                <span className="ooda-sub">ground this message in:</span>
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
                <textarea id="ooda-chat-message" aria-label="Message Zeck" className="ooda-textarea" rows={2} value={text}
                  placeholder={window.CHAT_ACTIONS.placeholder + (mentions.length ? " · @ to tag an intent" : "")}
                  onChange={(e) => setText(e.target.value)}
                  onKeyDown={(e) => {
                    if (mentionOpen && mentions.length && (e.key === "Tab" || e.key === "Enter") && !e.metaKey && !e.ctrlKey) {
                      e.preventDefault(); pickMention(mentions[0].token); return;
                    }
                    if (e.key === "Enter" && (e.metaKey || e.ctrlKey)) send(e.shiftKey ? "delegate" : "ask");
                  }} />
                {mentionOpen && mentions.length > 0 ? (
                  <div className="ooda-mention">
                    {mentions.map((mm) => (
                      <button key={mm.token} className="ooda-mention-row"
                        onMouseDown={(e) => { e.preventDefault(); pickMention(mm.token); }}>
                        <span className="ooda-mention-tok">{mm.token}</span>
                        <span className="ooda-sub">{mm.note}</span>
                      </button>
                    ))}
                  </div>
                ) : null}
              </div>
              <div className="ooda-compose-acts">
                <button className="ooda-send" onClick={() => send("ask")} disabled={busy || !text.trim()}>
                  {busy ? "…" : shared.native ? "Send to "+shared.native.agent : window.CHAT_ACTIONS.ask.label}
                </button>
                <button style={{display:shared.native?"none":undefined}} className="ooda-send secondary" onClick={() => send("delegate")} disabled={busy || !text.trim()}>
                  {window.CHAT_ACTIONS.propose.label}
                </button>
              </div>
            </div>
            {/* one row per action, the name in mono and its consequence beside
                it. Run together on one line these two read as a single
                sentence, and the sub-labels already carry their own em-dash. */}
            <div className="ooda-actdefs" style={{display:shared.native?"none":undefined}}>
              <span><em>{window.CHAT_ACTIONS.ask.label}</em>{window.CHAT_ACTIONS.ask.sub}</span>
              <span><em>{window.CHAT_ACTIONS.propose.label}</em>{window.CHAT_ACTIONS.propose.sub}</span>
            </div>
            {/* attach is quieter and separate: the file becomes a context chip
                above and rides the send you were going to make anyway */}
            <div className="ooda-attach-row" style={{display:shared.native?"none":undefined}}>
              <label className="ooda-attach" title={window.CHAT_ACTIONS.attach.hint}>
                {attaching ? "…" : window.CHAT_ACTIONS.attach.label}
                <input type="file" accept={window.CHAT_ACTIONS.attach.accept}
                  disabled={!!attaching || !sel}
                  onChange={(e) => { const f = e.target.files[0]; e.target.value = ""; attachFile(f); }} />
              </label>
              <span className="ooda-sub">{window.CHAT_ACTIONS.attach.hint}</span>
            </div>
            <div className="ooda-assure">{shared.native ? 'Messages and terminal replies are visible to this team.' : window.CHAT_ACTIONS.assurance}</div>
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
