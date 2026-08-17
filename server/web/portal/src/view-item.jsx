/* Portal v2 — ITEM: one object, one stream. Header + rock pin, FIELDS
   (assignee-only), the PLAN record editor (section-scoped saves), the merged
   stream (comments · changes · agent turns + the plan card), and the composer
   with the @Kairos mention lane. Mirrors the prototype; agent semantics map
   to the live backend (plans auto-materialize; "run the plan" = fire). */

function ItemView({ item, me, team, teamOn, goalsIndex, filter, onBack, pin, reloadTeam, onDiscuss }) {
  const U = window.PORTAL_UTIL;
  const D = window.PORTAL_DERIVE;
  const PT = window.PORTAL_TEAM;
  const today = U.todayISO();

  const [panel, setPanel] = React.useState(null);
  const [activity, setActivity] = React.useState([]);
  const [saveNote, setSaveNote] = React.useState('');
  const [planOpen, setPlanOpen] = React.useState(false);
  const [planDraft, setPlanDraft] = React.useState(null);
  const [status, setStatus] = React.useState(item.status || 'open');
  const [due, setDue] = React.useState(item.due || item.needed_by || '');

  const loadAll = React.useCallback(() => {
    PT.loadPanel(item.id).then(r => setPanel(r.ok ? r.value : null));
    PT.loadActivity(item.id).then(r => setActivity(r.ok && r.value ? (r.value.activity || []) : []));
  }, [item.id]);
  React.useEffect(() => { loadAll(); }, [loadAll]);

  const deleg = (panel && panel.delegation) || null;
  const delegState = deleg ? deleg.state : '';
  React.useEffect(() => { // poll only while the loop is moving
    if (!/queued|running/.test(delegState)) return;
    const t = setInterval(() => { loadAll(); reloadTeam(); }, 20000);
    return () => clearInterval(t);
  }, [delegState, loadAll, reloadTeam]);

  const gid = D.rockOf(goalsIndex, item);
  const rockGoal = gid ? goalsIndex.get(gid) : null;
  const isAssignee = !!(me && item.owner === me.initials);
  const canEdit = isAssignee && teamOn;
  const planEditable = teamOn && (isAssignee || (me && me.admin));

  const rec = panel || {};
  const recExists = !!rec.exists;
  const draft = planDraft || { description: rec.description || '', plan: rec.plan || '' };
  const planLines = rec.plan ? rec.plan.split('\n').filter(l => l.trim()).length : 0;
  const planSummary = recExists
    ? (planLines + (planLines === 1 ? ' line' : ' lines') + ' · ' +
      (rec.held ? 'held by ' + rec.assignee : 'last edit ' + (rec.planAt ? U.fmtDateShort(rec.planAt.slice(0, 10)) : '—')))
    : 'no record yet · created on first save';

  const patch = fields => {
    TEAM_API.post('api/team/item/' + item.id, fields, 'PATCH').then(r => {
      setSaveNote(r.ok ? 'PATCH /api/team/item/' + item.id.split('/').pop() + ' · applied' : r.error);
      if (r.ok) { reloadTeam(); loadAll(); }
    });
  };
  const saveFields = () => patch(item.kind === 'decision' ? { status: status, needed_by: due } : { status: status, due: due });
  const markDone = () => patch({ status: 'done', done_on: today });

  const savePlan = () => {
    const sections = {};
    if (draft.description.trim() !== (rec.description || '').trim()) sections.description = draft.description;
    if (draft.plan.trim() !== (rec.plan || '').trim()) sections.plan = draft.plan;
    const names = Object.keys(sections);
    if (!names.length) { setSaveNote('no section changed'); return; }
    PT.savePlanSections(item.id, sections).then(r => {
      if (r.ok) {
        setSaveNote('POST /api/team/plan · ' + names.join(' + ') + ' · section swap applied');
        setPlanDraft(null);
        loadAll();
      } else setSaveNote(r.error);
    });
  };

  // ---- stream: comments + activity + the plan card (current semantics) ----
  const deleteComment = c => {
    if (!window.confirm('Delete this comment?')) return;
    TEAM_API.post('api/team/comment', { item: item.id, id: c.id }, 'DELETE').then(() => reloadTeam());
  };
  const stream = teamOn && me
    ? D.mergeStream(item, team, activity, me, today, { deleteComment: deleteComment })
    : [];

  const agentHeld = !!(rec.assignee && String(rec.assignee).indexOf('agent:') === 0);
  const agentName = agentHeld ? '@' + String(rec.assignee).slice(6) : '';
  const goDone = !!(deleg && deleg.state === 'done' && deleg.phase === 'go');
  const running = /queued|running/.test(delegState);
  if (agentHeld && rec.plan) {
    const steps = rec.plan.split('\n').map(s => s.trim()).filter(Boolean)
      .map(s => s.replace(/^\d+[.)]\s*/, ''));
    stream.push({
      key: 'plancard',
      sort: '9999-9999',
      at: rec.planAt ? U.fmtDateShort(rec.planAt.slice(0, 10)) : '',
      label: 'agent', glyphColor: 'var(--accent-bright,#5ec8f5)',
      who: agentName, whoColor: 'var(--accent-bright,#5ec8f5)',
      state: running ? delegState + ' · polling 20s' : (deleg ? delegState : ''),
      stateColor: running ? 'var(--accent,#0091ea)' : 'var(--ink-mute,#666)',
      textColor: 'var(--ink,#d4d4d4)',
      text: '', meta: '', files: [],
      hasSteps: true,
      planLabel: goDone ? 'PLAN · executed' : 'PLAN · saved to the plan file',
      steps: steps.map((s, i) => ({ n: String(i + 1) + '.', text: s })),
      runShown: !!(me && me.canFire && !goDone && !running),
      runLabel: running ? 'running…' : 'run the plan',
      runNote: 'writes and attachments need one click',
      run: () => {
        TEAM_API.post('api/team/fire', { item: item.id }).then(r => {
          setSaveNote(r.ok ? 'POST /api/team/fire · queued' : r.error);
          loadAll(); reloadTeam();
        });
      },
      hasEdit: false, delLabel: '', delCursor: 'default', del: function () {}
    });
  }

  // ---- composer ----
  const [composer, setComposer] = React.useState('');
  const [agents, setAgents] = React.useState([]);
  React.useEffect(() => {
    let on = true;
    loadAgents().then(a => { if (on) setAgents(a); });
    return () => { on = false; };
  }, []);
  const mentionOpen = /@[^\s]*$/.test(composer) && agents.length > 0;
  const mentionOptions = [];
  agents.forEach(a => {
    const short = '@' + a.harness;
    mentionOptions.push({ token: short, structural: a.id, harness: 'team agent · hermes-agent' });
    (a.personas || []).forEach(p => {
      mentionOptions.push({ token: short + '::' + p, structural: a.id + '::' + p, harness: 'team agent · hermes-agent' });
    });
  });
  const agentRe = agents.length ? new RegExp('@(' + agents.map(a => a.harness).join('|') + ')(::[a-z0-9-]+)?', 'ig') : null;
  const asksAgent = !!(agentRe && composer.match(agentRe));
  const send = () => {
    const text = composer.trim();
    if (!text) return;
    const mentions = [];
    if (agentRe) {
      const seen = {};
      (text.match(agentRe) || []).forEach(m => {
        const parts = m.slice(1).split('::');
        const tok = 'agent:' + parts[0].toLowerCase() + (parts[1] ? '::' + parts[1] : '');
        if (!seen[tok]) { seen[tok] = true; mentions.push(tok); }
      });
    }
    TEAM_API.post('api/team/comment', { item: item.id, text: text, mentions: mentions }).then(r => {
      if (r.ok) { setComposer(''); setSaveNote(''); reloadTeam(); loadAll(); }
      else setSaveNote(r.error);
    });
  };
  const primaryAgent = agents.length ? '@' + agents[0].harness : '@…';
  const composerHint = asksAgent
    ? (/\?/.test(composer) ? 'a question is answered from the context it has — no run needed' : 'it will post a plan; running it is one click')
    : 'plain comment lands on the record';

  const itemSub = [
    item.owner ? U.personName(item.owner) : 'unowned',
    item.due ? 'due ' + U.fmtDateShort(item.due) : null,
    item.needed_by ? 'needed by ' + U.fmtDateShort(item.needed_by) : null,
    item.done_on ? 'done ' + U.fmtDateShort(item.done_on) : null,
    item.added_by ? 'added by ' + item.added_by : null
  ].filter(Boolean).join(' · ');

  return (
    <div style={{ maxWidth: 920 }}>
      <button className="v2-bare v2-hoverink" onClick={onBack}
        style={{ color: 'var(--ink-faint,#888)', fontSize: 11, padding: '10px 0 0' }}>← work</button>

      <div style={{ display: 'flex', gap: 9, alignItems: 'baseline', marginTop: 10 }}>
        <span style={{ color: item.kind === 'decision' ? 'var(--ink-dim,#b0b0b0)' : D.markColor(item.status), fontSize: 14 }}>
          {item.kind === 'decision' ? '◇' : D.mark(item.status)}
        </span>
        <span className="v2-label">{item.kind}</span>
        {item.team && <span style={{ fontSize: 10, color: 'var(--accent,#0091ea)' }}>team/</span>}
        <button className="v2-bare v2-underlink v2-hoveraccent-t" onClick={() => { if (gid) pin(gid); }}
          style={{ marginLeft: 'auto', color: 'var(--ink-faint,#888)', fontSize: 11 }}>
          {rockGoal ? rockGoal.title : 'unanchored'}
        </button>
      </div>
      <div style={{ fontSize: 20, lineHeight: 1.35, marginTop: 8 }}>{item.title}</div>
      <div style={{ fontSize: 11, color: 'var(--ink-faint,#888)', marginTop: 6 }}>{itemSub}</div>
      <div style={{ display: 'flex', gap: 12, alignItems: 'baseline', flexWrap: 'wrap' }}>
        <span style={{ fontSize: 11, color: 'var(--ink-mute,#666)' }}>{item.source || ''}</span>
        {teamOn && onDiscuss && (
          <button className="v2-bare v2-underlink v2-hoveraccent-t" style={{ color: 'var(--ink-faint,#888)', fontSize: 11 }}
            onClick={() => onDiscuss(item)}>discuss with kairos →</button>
        )}
      </div>

      {canEdit && (
        <div style={{ display: 'flex', gap: 10, alignItems: 'center', flexWrap: 'wrap',
          borderTop: '1px solid var(--line,#3a3a3a)', borderBottom: '1px solid var(--line,#3a3a3a)',
          padding: '10px 0', marginTop: 14 }}>
          <span className="v2-label">FIELDS</span>
          <select className="v2-input" value={status} onChange={e => setStatus(e.target.value)}>
            <option value="open">open</option>
            <option value="in_progress">in_progress</option>
            <option value="done">done</option>
          </select>
          <span style={{ fontSize: 11, color: 'var(--ink-faint,#888)' }}>{item.kind === 'decision' ? 'needed_by' : 'due'}</span>
          <input className="v2-input" type="date" value={due} onChange={e => setDue(e.target.value)} />
          <button className="v2-btn v2-accentfill" onClick={saveFields}
            style={{ borderColor: 'var(--accent,#0091ea)', color: 'var(--accent,#0091ea)', padding: '3px 11px' }}>save</button>
          <button className="v2-btn v2-hoverline" onClick={markDone}
            style={{ color: 'var(--ink-faint,#888)', padding: '3px 11px' }}>mark done</button>
          <span style={{ fontSize: 11, color: 'var(--ink-mute,#666)' }}>{saveNote}</span>
        </div>
      )}
      {!canEdit && teamOn && (
        <div style={{ fontSize: 11, color: 'var(--ink-mute,#666)', borderTop: '1px solid var(--line,#3a3a3a)',
          borderBottom: '1px solid var(--line,#3a3a3a)', padding: '10px 0', marginTop: 14 }}>
          fields are assignee-only · {U.personName(item.owner)} holds this one
        </div>
      )}

      <div style={{ borderBottom: '1px solid var(--line,#3a3a3a)', padding: '10px 0' }}>
        <div style={{ display: 'flex', gap: 10, alignItems: 'baseline', flexWrap: 'wrap' }}>
          <span className="v2-label">PLAN</span>
          <button className="v2-bare v2-hoverink" onClick={() => { setPlanOpen(!planOpen); setPlanDraft(null); }}
            style={{ color: 'var(--ink-dim,#aaa)', fontSize: 11.5 }}>
            {planOpen ? '[-] collapse' : (recExists ? '[+] open plan file' : '[+] start a plan file')}
          </button>
          <span style={{ fontSize: 11, color: 'var(--ink-mute,#666)' }}>{planSummary}</span>
          <span style={{ marginLeft: 'auto', fontSize: 11, color: 'var(--ink-mute,#666)' }}>
            {'system/todo-plans/' + PT.planSlug('aion:' + item.id) + '.md'}
          </span>
        </div>
        {planOpen && (
          <div style={{ border: '1px solid var(--line,#3a3a3a)', background: 'var(--bg-1,#1e1e1e)', padding: 12, marginTop: 10 }}>
            <div style={{ display: 'flex', gap: 10, alignItems: 'baseline', fontSize: 11, color: 'var(--ink-mute,#666)',
              borderBottom: '1px solid var(--line-soft,#2a2a2a)', paddingBottom: 7, flexWrap: 'wrap' }}>
              <span>todo: aion:{item.id}</span>
              <span>assignee: {rec.assignee || item.owner || '—'}</span>
              <span>state: {rec.state || item.status || 'open'}</span>
            </div>
            <div className="v2-label" style={{ marginTop: 11 }}>## description</div>
            <textarea className="v2-input" value={draft.description} readOnly={!planEditable} rows={3}
              placeholder="why this matters"
              onChange={e => setPlanDraft({ description: e.target.value, plan: draft.plan })}
              style={{ width: '100%', marginTop: 5, fontSize: 12, lineHeight: 1.6, padding: 8, resize: 'vertical' }} />
            <div className="v2-label" style={{ marginTop: 12 }}>## plan</div>
            <textarea className="v2-input" value={draft.plan} readOnly={!planEditable} rows={10}
              placeholder={'1. first step\n2. second step'}
              onChange={e => setPlanDraft({ description: draft.description, plan: e.target.value })}
              style={{ width: '100%', marginTop: 5, fontSize: 12, lineHeight: 1.7, padding: 8, resize: 'vertical' }} />
            {planEditable ? (
              <div style={{ display: 'flex', gap: 9, alignItems: 'center', flexWrap: 'wrap', marginTop: 10 }}>
                <button className="v2-btn v2-accentfill" onClick={savePlan}
                  style={{ borderColor: 'var(--accent,#0091ea)', color: 'var(--accent,#0091ea)', padding: '3px 11px' }}>save sections</button>
                <button className="v2-btn v2-hoverline" onClick={() => setPlanDraft(null)}
                  style={{ color: 'var(--ink-faint,#888)', padding: '3px 11px' }}>revert</button>
                <span style={{ fontSize: 11, color: 'var(--ink-mute,#666)' }}>
                  markdown · sections save separately, so an Obsidian edit to one never collides with the other
                </span>
              </div>
            ) : (
              <div style={{ fontSize: 11, color: 'var(--ink-mute,#666)', marginTop: 10 }}>
                read-only · {U.personName(item.owner)} holds this record
              </div>
            )}
          </div>
        )}
      </div>

      <div style={{ display: 'flex', flexDirection: 'column', marginTop: 18 }}>
        {stream.sort((a, b) => a.sort < b.sort ? -1 : 1).map(e => (
          <div key={e.key} style={{ display: 'grid', gridTemplateColumns: '78px minmax(0,1fr)', gap: 14,
            borderBottom: '1px solid var(--line-soft,#2a2a2a)', padding: '10px 2px' }}>
            <div style={{ fontSize: 11, color: 'var(--ink-mute,#666)' }}>
              <div>{e.at}</div>
              <div style={{ color: e.glyphColor, letterSpacing: '.1em', fontSize: 10, marginTop: 2 }}>{e.label}</div>
            </div>
            <div style={{ minWidth: 0 }}>
              <div style={{ display: 'flex', gap: 9, alignItems: 'baseline' }}>
                <span style={{ fontSize: 11.5, color: e.whoColor }}>{e.who}</span>
                <span style={{ fontSize: 11, color: e.stateColor }}>{e.state}</span>
                {e.delLabel && (
                  <button className="v2-bare v2-hoverwarn" onClick={e.del}
                    style={{ marginLeft: 'auto', color: 'var(--ink-mute,#666)', fontSize: 11 }}>{e.delLabel}</button>
                )}
              </div>
              {e.text && <div style={{ fontSize: 12.5, marginTop: 3, color: e.textColor, whiteSpace: 'pre-wrap' }}>{e.text}</div>}
              {e.hasSteps && (
                <div style={{ border: '1px solid var(--line,#3a3a3a)', background: 'var(--bg-1,#1e1e1e)', padding: '9px 11px', marginTop: 8 }}>
                  <div className="v2-label">{e.planLabel}</div>
                  <div style={{ display: 'flex', flexDirection: 'column', gap: 3, marginTop: 6 }}>
                    {e.steps.map((s, i) => (
                      <div key={i} style={{ display: 'flex', gap: 9, fontSize: 11.5, color: 'var(--ink-dim,#aaa)' }}>
                        <span style={{ color: 'var(--ink-mute,#555)' }}>{s.n}</span>
                        <span>{s.text}</span>
                      </div>
                    ))}
                  </div>
                  <div style={{ display: 'flex', gap: 9, alignItems: 'center', flexWrap: 'wrap', marginTop: 10 }}>
                    {e.runShown && (
                      <button className="v2-btn v2-accentfill" onClick={e.run}
                        style={{ borderColor: 'var(--accent,#0091ea)', color: 'var(--accent-bright,#5ec8f5)', padding: '3px 11px' }}>
                        {e.runLabel}
                      </button>
                    )}
                    {e.runShown && <span style={{ fontSize: 11, color: 'var(--ink-mute,#666)' }}>{e.runNote}</span>}
                  </div>
                </div>
              )}
              {(e.files || []).length > 0 && (
                <div style={{ display: 'flex', gap: 10, flexWrap: 'wrap', marginTop: 4 }}>
                  {e.files.map((f, i) => (
                    <a key={i} href={'api/team/file/' + f.hash} target="_blank" rel="noopener"
                      style={{ fontSize: 11 }}>⤓ {f.name}</a>
                  ))}
                </div>
              )}
              {e.meta && !(e.files || []).length && (
                <div style={{ fontSize: 11, color: 'var(--ink-mute,#666)', marginTop: 4 }}>{e.meta}</div>
              )}
            </div>
          </div>
        ))}
      </div>
      {teamOn && stream.length === 0 && (
        <div style={{ fontSize: 12, color: 'var(--ink-mute,#666)', padding: '12px 0' }}>
          nothing on the record yet — comment, or ask the agent
        </div>
      )}

      {teamOn && (
        <div style={{ marginTop: 18, maxWidth: 720 }}>
          <textarea className="v2-input" value={composer} rows={3}
            placeholder={'comment, or ' + primaryAgent + ' + what you need'}
            onChange={e => setComposer(e.target.value)}
            style={{ width: '100%', fontSize: 12.5, padding: 9, resize: 'vertical' }} />
          {mentionOpen && (
            <div style={{ border: '1px solid var(--line,#3a3a3a)', background: 'var(--bg-2,#1a1a1a)', marginTop: -1 }}>
              {mentionOptions.map(m => (
                <button key={m.token} className="v2-bare v2-hoverbg2" onMouseDown={e => {
                  e.preventDefault();
                  setComposer(c => c.replace(/@[^\s]*$/, m.token + ' '));
                }} style={{ display: 'flex', gap: 9, width: '100%', color: 'var(--ink,#d4d4d4)', padding: '4px 9px',
                  fontSize: 11, textAlign: 'left' }}>
                  <span style={{ color: 'var(--accent-bright,#5ec8f5)' }}>{m.token}</span>
                  <span style={{ color: 'var(--ink-mute,#666)' }}>{m.harness}</span>
                </button>
              ))}
            </div>
          )}
          <div style={{ display: 'flex', gap: 10, alignItems: 'center', marginTop: 9, flexWrap: 'wrap' }}>
            <button className="v2-btn v2-hoveraccent" onClick={send}
              style={{ color: 'var(--ink,#d4d4d4)', padding: '4px 13px' }}>
              {asksAgent ? 'ask the agent' : 'comment'}
            </button>
            <span style={{ fontSize: 11, color: 'var(--ink-mute,#666)' }}>{composerHint}</span>
          </div>
        </div>
      )}
    </div>
  );
}

Object.assign(window, { ItemView });
