/* Portal v2 — CHAT with kairos (chat-kairos handoff). The 5th destination:
   shared threads, an ask/delegate composer, honest spooled→running→completed
   states (poll-based, no token stream), proposal cards a person approves, and
   the engine sidebar (heartbeat, queue, intents). Backed by /api/chat/*. */

function ChatView({ me, goalsIndex, items, filter, openItem, w, seed, onSeedUsed, onAgentProps, onEngine }) {
  const [threads, setThreads] = React.useState([]);
  const [msgs, setMsgs] = React.useState({});
  const [engine, setEngine] = React.useState(null);
  const [active, setActive] = React.useState(null);      // active thread id
  const [threadEdit, setThreadEdit] = React.useState(false);
  const [showArch, setShowArch] = React.useState(false);
  const [draft, setDraft] = React.useState('');
  const [lastRitual, setLastRitual] = React.useState('ask');
  const [ctx, setCtx] = React.useState([]);
  const [runSel, setRunSel] = React.useState(null);
  const [propOpen, setPropOpen] = React.useState(null);  // "msgId#idx"
  const [intents, setIntents] = React.useState([]);
  const [editName, setEditName] = React.useState('');
  const [editRock, setEditRock] = React.useState('');
  const [err, setErr] = React.useState('');
  const seq = React.useRef(0);

  const load = React.useCallback(() => {
    window.TEAM_API.get('api/chat/threads').then(r => {
      if (!r.ok || !r.value) return;
      setThreads(r.value.threads || []);
      setMsgs(r.value.messages || {});
      setEngine(r.value.engine || null);
      if (onEngine) onEngine(r.value.engine);
    });
  }, [onEngine]);

  React.useEffect(() => { load(); window.loadAgents().then(setIntents); }, [load]);

  // "discuss with kairos" seeded a thread + context — open it once
  React.useEffect(() => {
    if (!seed) return;
    setActive(seed.threadID); setShowArch(false); setCtx(seed.ctx || []);
    load();
    if (onSeedUsed) onSeedUsed();
  }, [seed, load, onSeedUsed]);

  // pending → poll fast; report the pending-proposal count to the rail
  const pendingRun = engine && (engine.active || (engine.pending && engine.pending.length));
  React.useEffect(() => {
    if (!pendingRun) return;
    const t = setInterval(load, 3500);
    return () => clearInterval(t);
  }, [pendingRun, load]);
  React.useEffect(() => {
    if (!onAgentProps) return;
    let n = 0;
    Object.keys(msgs).forEach(k => (msgs[k] || []).forEach(m =>
      (m.proposals || []).forEach(p => { if (p.state === 'pending') n++; })));
    onAgentProps(n);
  }, [msgs, onAgentProps]);

  const pool = threads.filter(t => !!t.archived === showArch);
  const thread = pool.filter(t => t.id === active)[0] || pool[0] || null;
  const threadMsgs = thread ? (msgs[thread.id] || []) : [];
  const admin = !!(me && me.admin);
  const canAct = !!(me && (me.admin || me.canFire));
  const busy = !!(engine && (engine.active || (engine.pending && engine.pending.length)));

  const rockChoices = [{ id: '', label: 'no rock · whole vault' }].concat(
    goalsIndex ? goalsIndex.goals.filter(g => g.horizon === 'rock' && g.status !== 'done')
      .map(g => ({ id: g.id, label: g.title })) : []);
  const rockTitle = id => { const g = goalsIndex && goalsIndex.get(id); return g ? g.title : id; };

  const post = (path, body) => window.TEAM_API.post(path, body).then(r => { if (!r.ok) setErr(r.error); return r; });
  const patchThread = (op, extra) => post('api/chat/thread', Object.assign({ op: op, id: thread.id }, extra || {})).then(load);

  const newThread = () => {
    seq.current += 1;
    const id = 'th/new-' + Date.now();
    post('api/chat/thread', { op: 'create', id: id, title: 'untitled' }).then(() => {
      setShowArch(false); setActive(id); setThreadEdit(true); setEditName(''); setEditRock(''); setCtx([]); load();
    });
  };
  const switchThread = id => {
    if (thread && thread.id !== id) post('api/chat/thread', { op: 'prune', id: '' }); // discard empty untitled
    setActive(id); setThreadEdit(false); setCtx([]); setRunSel(null); setPropOpen(null);
  };

  // send(key) — the composer labels by OUTPUT; ritualOf maps that to the wire
  // value the API takes. lastRitual only records what went out (the waiting row
  // reads it); it never gates the buttons.
  const send = (key) => {
    const rt = window.CHAT_ACTIONS.ritualOf(key);
    if (!thread || !draft.trim() || busy) return;
    setErr(''); setLastRitual(rt);
    post('api/chat/ask', { thread: thread.id, text: draft.trim(), ritual: rt, context: ctx.slice() })
      .then(r => { if (r.ok) { setDraft(''); load(); } });
  };

  const decide = (m, idx, apply) =>
    post('api/chat/proposal', { thread: thread.id, msg: m.id, index: idx, apply: apply }).then(load);

  // context offers: pinned rock / open item / thread rock
  const offers = [];
  if (filter && ctx.indexOf(filter) < 0) offers.push({ id: filter, label: 'pinned rock' });
  if (openItem && ctx.indexOf('aion:' + openItem) < 0) offers.push({ id: 'aion:' + openItem, label: 'open item' });
  if (thread && thread.rock && ctx.indexOf(thread.rock) < 0) offers.push({ id: thread.rock, label: 'thread rock' });

  // mention list from the roster (@kairos + persona variants)
  const mentionOpen = /@[^\s]*$/.test(draft) && draft.slice(-1) !== ' ';
  const mentions = [];
  (intents || []).forEach(a => {
    mentions.push({ token: '@' + a.harness, note: 'no intent tag · it decides how to answer' });
    (a.personas || []).forEach(p => mentions.push({ token: '@' + a.harness + '::' + p, note: p }));
  });
  const pickMention = tok => setDraft(d => d.replace(/@[^\s]*$/, tok + ' '));

  const wide = w >= 1080;
  const ctxLabel = id => {
    if (id.indexOf('aion:') === 0) {
      const it = items.filter(x => x.id === id.slice(5))[0];
      return it ? it.title.slice(0, 32) : id;
    }
    return rockTitle(id);
  };

  return (
    <div style={{ display: 'grid', gridTemplateColumns: wide ? 'minmax(0,1fr) 244px' : 'minmax(0,1fr)',
      gap: '0 26px', alignItems: 'start' }}>
      {/* ── thread column ── */}
      <div style={{ display: 'flex', flexDirection: 'column', minHeight: 'calc(100vh - 150px)', minWidth: 0 }}>
        {/* thread strip */}
        <div style={{ display: 'flex', borderBottom: '1px solid var(--line,#3a3a3a)', padding: '10px 0 0', flexWrap: 'wrap' }}>
          {pool.map(t => (
            <button key={t.id} className="v2-bare v2-hoverink" onClick={() => switchThread(t.id)}
              style={{ borderBottom: '2px solid ' + (thread && t.id === thread.id ? 'var(--accent,#0091ea)' : 'transparent'),
                color: thread && t.id === thread.id ? 'var(--ink,#d4d4d4)' : 'var(--ink-faint,#888)', padding: '5px 14px 7px', fontSize: 12 }}>
              {t.title || 'untitled'} <span style={{ color: 'var(--ink-mute,#666)', fontSize: 11 }}>{(msgs[t.id] || []).length || ''}</span>
            </button>
          ))}
          <div style={{ marginLeft: 'auto', display: 'flex', gap: 12, alignItems: 'center' }}>
            <button className="v2-bare v2-hoverink" onClick={() => { setShowArch(a => !a); setActive(null); }}
              style={{ color: 'var(--ink-mute,#666)', fontSize: 11 }}>
              {showArch ? '← live threads' : 'archived ' + threads.filter(t => t.archived).length}
            </button>
            {!showArch && <button className="v2-bare v2-hoverink" onClick={newThread}
              style={{ color: 'var(--ink-mute,#666)', fontSize: 11 }}>+ new thread</button>}
          </div>
        </div>

        {!thread && showArch && (
          <div style={{ fontSize: 12, color: 'var(--ink-mute,#666)', padding: '16px 0', maxWidth: '70ch' }}>
            Nothing archived yet. Archiving a thread parks it here; reopening puts it back in the live strip.
          </div>
        )}

        {thread && (
          <React.Fragment>
            {/* thread header */}
            <div style={{ display: 'flex', alignItems: 'center', padding: '9px 0', borderBottom: '1px solid var(--line-soft,#2e2e2e)', flexWrap: 'wrap', gap: 10 }}>
              {!threadEdit ? (
                <button className="v2-bare v2-underlink v2-hoverink"
                  onClick={() => { setThreadEdit(true); setEditName(thread.title === 'untitled' ? '' : thread.title); setEditRock(thread.rock || ''); }}
                  style={{ fontSize: 12, color: thread.rock ? 'var(--ink-faint,#888)' : 'var(--accent,#0091ea)' }}>
                  {thread.rock ? 'scoped to ' + rockTitle(thread.rock) : 'no rock scope · name it and pick one'}
                </button>
              ) : (
                <div style={{ display: 'flex', gap: 9, alignItems: 'center', flexWrap: 'wrap' }}>
                  <input className="v2-input" value={editName} placeholder="thread name" style={{ width: 150 }}
                    onChange={e => setEditName(e.target.value.toLowerCase().slice(0, 22))} />
                  <select className="v2-input" value={editRock} style={{ maxWidth: 260 }}
                    onChange={e => setEditRock(e.target.value)}>
                    {rockChoices.map(o => <option key={o.id} value={o.id}>{o.label}</option>)}
                  </select>
                  <button className="v2-btn v2-hoveraccent" style={{ padding: '3px 11px', color: 'var(--accent,#0091ea)' }}
                    onClick={() => {
                      patchThread('rename', { title: editName || 'untitled' });
                      patchThread('rescope', { rock: editRock });
                      setThreadEdit(false);
                    }}>done</button>
                </div>
              )}
              <span title="chat build marker (temporary diagnostic)"
                style={{ marginLeft: 'auto', color: 'var(--ink-mute,#555)', fontSize: 10, letterSpacing: '.12em' }}>BUILD 17</span>
              <button className="v2-bare v2-hoverink" style={{ color: 'var(--ink-mute,#666)', fontSize: 11 }}
                onClick={() => patchThread(thread.archived ? 'reopen' : 'archive')}>
                {thread.archived ? 'reopen thread' : 'archive thread'}
              </button>
            </div>

            {/* messages */}
            <div style={{ display: 'flex', flexDirection: 'column' }}>
              {threadMsgs.map(m => {
                const isK = m.kind === 'kairos';
                const failed = m.outcome === 'failed';
                return (
                  <div key={m.id} style={{ display: 'grid', gridTemplateColumns: '70px minmax(0,1fr)', gap: 12,
                    borderBottom: '1px solid var(--line-soft,#2e2e2e)', padding: '11px 2px' }}>
                    <div style={{ fontSize: 11, color: 'var(--ink-mute,#666)' }}>{fmtWhen(m.at)}</div>
                    <div style={{ minWidth: 0 }}>
                      <div style={{ display: 'flex', gap: 9, alignItems: 'baseline', flexWrap: 'wrap' }}>
                        <span style={{ fontSize: 11.5, color: isK ? 'var(--accent-bright,#5ec8f5)' : 'var(--ink,#d4d4d4)' }}>
                          {isK ? 'kairos' : (m.author_name || m.author)}
                        </span>
                        {isK && <span style={{ fontSize: 11, color: failed ? 'var(--warn,#a44)' : 'var(--ink-mute,#666)' }}>
                          {m.ritual + ' · ' + m.outcome + (m.elapsed ? ' · ' + m.elapsed : '')}
                        </span>}
                        {isK && m.run && (
                          <button className="v2-bare v2-hoverink" style={{ marginLeft: 'auto', color: 'var(--ink-mute,#666)', fontSize: 11 }}
                            onClick={() => setRunSel(runSel === m.run ? null : m.run)}>
                            {runSel === m.run ? 'hide run' : 'run details'}
                          </button>
                        )}
                        {failed && <button className="v2-btn v2-hoverline" style={{ color: 'var(--ink-faint,#888)', padding: '2px 9px' }}
                          onClick={() => setDraft('narrower re-run of ' + m.run + ' — ')}>re-run</button>}
                      </div>
                      {m.text && <div style={{ fontSize: 12.5, lineHeight: 1.65, whiteSpace: 'pre-wrap',
                        overflowWrap: 'anywhere', wordBreak: 'break-word', minWidth: 0, maxWidth: '100%',
                        overflowX: 'auto', marginTop: 3,
                        color: isK ? 'var(--ink,#d4d4d4)' : 'var(--ink,#d4d4d4)' }}>{m.text}</div>}
                      {(m.context && m.context.length > 0) && (
                        <div style={{ fontSize: 11, color: 'var(--ink-mute,#666)', marginTop: 4 }}>
                          ↳ context · {m.context.map(ctxLabel).join(' · ')}
                        </div>
                      )}
                      {isK && runSel === m.run && (
                        <div style={{ borderLeft: '2px solid var(--line,#3a3a3a)', paddingLeft: 10, marginTop: 8, fontSize: 11, color: 'var(--ink-mute,#666)' }}>
                          <div>run · {m.run}</div>
                          {m.report && <div>report · {m.report}</div>}
                          {m.brief && <div>brief · {m.brief}</div>}
                        </div>
                      )}
                      {(m.proposals || []).map((p, idx) => {
                        const key = m.id + '#' + idx;
                        const open = propOpen === key;
                        const sc = p.state === 'applied' ? 'var(--good,#6fb8dd)' : p.state === 'discarded' ? 'var(--warn,#a44)' : 'var(--ink-mute,#666)';
                        return (
                          <div key={idx} style={{ border: '1px solid var(--line,#3a3a3a)', background: 'var(--bg-1,#1e1e1e)', padding: '10px 11px', marginTop: 8 }}>
                            <div style={{ display: 'flex', gap: 9, alignItems: 'baseline' }}>
                              <span className="v2-label">PROPOSES</span>
                              <span style={{ fontSize: 12, color: 'var(--accent,#0091ea)' }}>{p.verb}</span>
                              <span style={{ marginLeft: 'auto', fontSize: 11, color: sc }}>{p.state}</span>
                            </div>
                            <div style={{ fontSize: 12, marginTop: 4 }}>{p.target}</div>
                            {(p.body || p.value) && (
                              <div style={{ fontSize: 11, color: 'var(--ink-mute,#666)', marginTop: 3 }}>
                                <button className="v2-bare v2-hoverink" style={{ color: 'var(--ink-dim,#aaa)', fontSize: 11 }}
                                  onClick={() => setPropOpen(open ? null : key)}>
                                  {open ? 'hide' : (p.type === 'replace-section' ? 'show the plan' : 'show the change')}
                                </button>
                              </div>
                            )}
                            {open && (
                              <div style={{ borderLeft: '2px solid var(--line,#3a3a3a)', paddingLeft: 10, marginTop: 6 }}>
                                {p.section && <div className="v2-label">## {p.section}</div>}
                                <div style={{ fontSize: 12, lineHeight: 1.7, whiteSpace: 'pre-wrap',
                                  overflowWrap: 'anywhere', wordBreak: 'break-word', minWidth: 0, maxWidth: '100%',
                                  overflowX: 'auto', color: 'var(--ink-dim,#aaa)', marginTop: 4 }}>
                                  {p.body || (p.field + ' → ' + p.value)}
                                </div>
                                {p.was && <div style={{ fontSize: 11, color: 'var(--ink-mute,#666)', marginTop: 6 }}>replaces · {p.was}</div>}
                              </div>
                            )}
                            {p.state === 'pending' && canAct && (
                              <div style={{ display: 'flex', gap: 9, marginTop: 9 }}>
                                <button className="v2-btn v2-accentfill" style={{ borderColor: 'var(--accent,#0091ea)', color: 'var(--accent,#0091ea)', padding: '3px 11px' }}
                                  onClick={() => decide(m, idx, true)}>apply</button>
                                <button className="v2-btn v2-hoverwarnb" style={{ color: 'var(--ink-faint,#888)', padding: '3px 11px' }}
                                  onClick={() => decide(m, idx, false)}>discard</button>
                              </div>
                            )}
                          </div>
                        );
                      })}
                    </div>
                  </div>
                );
              })}
            </div>

            {/* waiting row */}
            {busy && engine && (() => {
              const running = engine.active && engine.active.thread === thread.id;
              const spooledHere = (engine.pending || []).some(p => p.thread === thread.id);
              if (!running && !spooledHere) return null;
              const phase = running ? 'running' : 'spooled';
              const pct = running ? '54%' : '12%';
              const text = running ? 'kairos is running · reports land when the turn finishes, there is no token stream'
                : 'order written to vessel/spool · the runner polls every 3s';
              const note = lastRitual === 'delegate' ? 'ceiling 10m · anything it would change comes back as a proposal'
                : 'kairos answers from context · it is sandboxed and changes nothing';
              return (
                <div style={{ display: 'grid', gridTemplateColumns: '78px minmax(0,1fr)', gap: 12, padding: '11px 2px' }}>
                  <div style={{ fontSize: 11, color: 'var(--accent,#0091ea)' }}>{phase}</div>
                  <div style={{ minWidth: 0 }}>
                    <div style={{ fontSize: 12, color: 'var(--ink-dim,#aaa)' }}>{text}</div>
                    <div style={{ height: 3, background: 'var(--bg-2,#1a1a1a)', border: '1px solid var(--line,#3a3a3a)', maxWidth: 280, marginTop: 6 }}>
                      <div style={{ height: '100%', width: pct, background: 'var(--accent,#0091ea)' }} />
                    </div>
                    <div style={{ fontSize: 11, color: 'var(--ink-mute,#666)', marginTop: 4 }}>{note}</div>
                  </div>
                </div>
              );
            })()}

            {/* composer */}
            <div style={{ marginTop: 'auto', paddingTop: 14 }}>
              {(ctx.length > 0 || offers.length > 0) && (
                <div style={{ display: 'flex', gap: 8, alignItems: 'center', flexWrap: 'wrap', marginTop: 9 }}>
                  <span className="v2-label">CONTEXT</span>
                  {ctx.map(id => (
                    <button key={id} className="v2-btn v2-hoverwarnb" style={{ color: 'var(--ink-dim,#aaa)', padding: '2px 8px' }}
                      onClick={() => setCtx(c => c.filter(x => x !== id))}>{ctxLabel(id)} ✕</button>
                  ))}
                  {offers.map(o => (
                    <button key={o.id} className="v2-bare v2-hoveraccent-t" style={{ border: '1px dashed var(--line,#3a3a3a)', color: 'var(--ink-faint,#888)', padding: '2px 8px', fontSize: 11 }}
                      onClick={() => setCtx(c => c.concat([o.id]))}>+ {o.label}</button>
                  ))}
                </div>
              )}
              <div style={{ position: 'relative', marginTop: 9 }}>
                <textarea className="v2-input" value={draft} rows={3}
                  placeholder={window.CHAT_ACTIONS.placeholder + ' · @ to tag an intent'}
                  onChange={e => setDraft(e.target.value)}
                  onKeyDown={e => { if (e.key === 'Enter' && (e.metaKey || e.ctrlKey)) { e.preventDefault(); send(e.shiftKey ? 'delegate' : 'ask'); } }}
                  style={{ width: '100%', fontSize: 12.5, padding: 9, resize: 'vertical' }} />
                {mentionOpen && mentions.length > 0 && (
                  <div style={{ border: '1px solid var(--line,#3a3a3a)', background: 'var(--bg-2,#1a1a1a)', marginTop: -1 }}>
                    {mentions.map(mm => (
                      <button key={mm.token} className="v2-bare v2-hoverbg2" onMouseDown={e => { e.preventDefault(); pickMention(mm.token); }}
                        style={{ display: 'flex', gap: 9, width: '100%', padding: '4px 9px', fontSize: 11, textAlign: 'left' }}>
                        <span style={{ color: 'var(--accent-bright,#5ec8f5)' }}>{mm.token}</span>
                        <span style={{ color: 'var(--ink-mute,#666)' }}>{mm.note}</span>
                      </button>
                    ))}
                  </div>
                )}
              </div>
              {/* two actions, each labelled by what comes BACK. No selected
                  state to misread — you press the outcome you want. */}
              {(() => {
                const canSend = !busy && !!draft.trim();
                const A = window.CHAT_ACTIONS;
                return (
                  <React.Fragment>
                    <div style={{ display: 'flex', gap: 14, alignItems: 'flex-start', marginTop: 9, flexWrap: 'wrap' }}>
                      <div style={{ display: 'flex', flexDirection: 'column', gap: 3 }}>
                        <button className="v2-btn v2-accentfill" disabled={!canSend}
                          style={{ borderColor: 'var(--accent,#0091ea)', color: canSend ? 'var(--accent,#0091ea)' : 'var(--ink-mute,#555)',
                            padding: '4px 13px', cursor: canSend ? 'pointer' : 'not-allowed' }}
                          onClick={() => send('ask')}>{busy ? A.busyLabel : A.ask.label}</button>
                        <span style={{ fontSize: 11, color: 'var(--ink-mute,#666)' }}>{A.ask.sub}</span>
                      </div>
                      <div style={{ display: 'flex', flexDirection: 'column', gap: 3 }}>
                        <button className="v2-btn v2-hoveraccent" disabled={!canSend}
                          style={{ color: canSend ? 'var(--ink,#d4d4d4)' : 'var(--ink-mute,#555)',
                            padding: '4px 13px', cursor: canSend ? 'pointer' : 'not-allowed' }}
                          onClick={() => send('delegate')}>{A.propose.label}</button>
                        <span style={{ fontSize: 11, color: 'var(--ink-mute,#666)' }}>{A.propose.sub}</span>
                      </div>
                    </div>
                    <div style={{ fontSize: 11, color: 'var(--ink-mute,#666)', marginTop: 8 }}>
                      {busy ? A.busyNote(engine) : A.assurance}
                    </div>
                    {err && <div style={{ fontSize: 11, color: 'var(--warn,#a44)', marginTop: 4 }}>{err}</div>}
                  </React.Fragment>
                );
              })()}
            </div>
          </React.Fragment>
        )}
      </div>

      {/* ── engine sidebar ── */}
      {wide && (
        <div style={{ borderLeft: '1px solid var(--line,#3a3a3a)', paddingLeft: 22, display: 'flex', flexDirection: 'column', gap: 20 }}>
          <EngineBlock engine={engine} />
          <QueueBlock engine={engine} me={me} />
          <IntentsBlock intents={intents} onUse={tok => setDraft(d => d + (d && d.slice(-1) !== ' ' ? ' ' : '') + tok + ' ')} />
        </div>
      )}
    </div>
  );
}

function EngineBlock({ engine }) {
  const live = engine && engine.live;
  const beat = engine && engine.beat >= 0 ? engine.beat : null;
  const facts = engine ? [
    ['runtime', engine.runtime], ['host', engine.host], ['model', engine.model],
    ['tree', engine.root], ['ceiling', engine.ceiling]
  ] : [];
  return (
    <div>
      <div style={{ display: 'flex', gap: 8, alignItems: 'baseline' }}>
        <span style={{ color: live ? 'var(--good,#6fb8dd)' : 'var(--warn,#a44)' }}>{live ? '●' : '○'}</span>
        <span style={{ fontSize: 12, color: 'var(--ink,#d4d4d4)' }}>kairos · {live ? 'live' : 'no heartbeat'}</span>
      </div>
      {beat != null && <div style={{ fontSize: 11, color: live ? 'var(--good,#6fb8dd)' : 'var(--warn,#a44)', marginTop: 3 }}>
        {live ? 'heartbeat ' + beat + 's ago' : 'stale ' + beat + 's · runner may be down'}
      </div>}
      <div style={{ display: 'flex', flexDirection: 'column', gap: 3, marginTop: 9 }}>
        {facts.map(f => (
          <div key={f[0]} style={{ display: 'flex', fontSize: 11 }}>
            <span style={{ minWidth: 54, color: 'var(--ink-mute,#666)' }}>{f[0]}</span>
            <span style={{ color: 'var(--ink-faint,#888)' }}>{f[1]}</span>
          </div>
        ))}
      </div>
    </div>
  );
}

function QueueBlock({ engine, me }) {
  const active = engine && engine.active;
  const pending = (engine && engine.pending) || [];
  const note = active ? '1 running' : pending.length ? pending.length + ' waiting' : 'idle';
  return (
    <div>
      <div style={{ display: 'flex', gap: 8, alignItems: 'baseline' }}>
        <span className="v2-label">QUEUE</span>
        <span style={{ fontSize: 11, color: 'var(--ink-mute,#666)' }}>{note}</span>
      </div>
      {active && (
        <div style={{ borderLeft: '2px solid var(--accent,#0091ea)', paddingLeft: 8, marginTop: 8 }}>
          <div style={{ fontSize: 11.5, color: 'var(--ink,#d4d4d4)' }}>{active.text || 'a run'}</div>
          <div style={{ fontSize: 11, color: 'var(--ink-mute,#666)', marginTop: 2 }}>
            {(active.by || 'someone') + ' · ' + (active.ritual || 'ask') + ' · claimed'}
          </div>
        </div>
      )}
      {pending.map((p, i) => (
        <div key={i} style={{ borderLeft: '2px solid var(--line,#3a3a3a)', paddingLeft: 8, marginTop: 8 }}>
          <div style={{ fontSize: 11.5, color: 'var(--ink-dim,#aaa)' }}>{p.text || 'a run'}</div>
          <div style={{ fontSize: 11, color: 'var(--ink-mute,#666)', marginTop: 2 }}>{(p.by || 'someone') + ' · spooled, waiting for the poll'}</div>
        </div>
      ))}
    </div>
  );
}

function IntentsBlock({ intents, onUse }) {
  const rows = [];
  (intents || []).forEach(a => {
    rows.push({ token: '@' + a.harness, scope: 'read', purpose: 'no intent tag · it decides how to answer' });
    (a.personas || []).forEach(p => rows.push({
      token: '@' + a.harness + '::' + p, scope: p === 'index' ? 'write' : 'read',
      purpose: p === 'brief' ? 'short answer, cites the files it read' : p === 'index' ? 'sweeps the backlog and files what it finds' : p
    }));
  });
  return (
    <div>
      <div className="v2-label">INTENTS</div>
      <div style={{ display: 'flex', flexDirection: 'column', gap: 10, marginTop: 8 }}>
        {rows.map(r => (
          <div key={r.token} style={{ borderLeft: '2px solid ' + (r.scope === 'write' ? 'var(--accent,#0091ea)' : 'var(--line,#3a3a3a)'), paddingLeft: 8 }}>
            <button className="v2-bare v2-hoverbright" style={{ color: 'var(--accent,#0091ea)', fontSize: 12 }}
              onClick={() => onUse(r.token)}>{r.token}</button>
            <span style={{ fontSize: 10, color: 'var(--ink-mute,#666)', marginLeft: 6 }}>{r.scope === 'write' ? 'can propose' : 'read-only'}</span>
            <div style={{ fontSize: 11, color: 'var(--ink-faint,#888)', marginTop: 2 }}>{r.purpose}</div>
          </div>
        ))}
      </div>
    </div>
  );
}

function fmtWhen(at) {
  if (!at) return '';
  const s = String(at);
  const d = new Date(s);
  if (isNaN(d)) return s.slice(0, 10);
  const names = ['jan', 'feb', 'mar', 'apr', 'may', 'jun', 'jul', 'aug', 'sep', 'oct', 'nov', 'dec'];
  return names[d.getMonth()] + ' ' + d.getDate();
}

Object.assign(window, { ChatView, EngineBlock, QueueBlock, IntentsBlock });
