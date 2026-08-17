/* Portal v2 — WORK: the one merged list (yours first, then rock groups), the
   add / propose form, and the proposals inbox. Markup mirrors the prototype. */

function WorkAddForm({ me, goalsIndex, onDone, reloadTeam }) {
  const [mode, setMode] = React.useState('me');
  const [email, setEmail] = React.useState('');
  const [kind, setKind] = React.useState('task');
  const [title, setTitle] = React.useState('');
  const [due, setDue] = React.useState('');
  const [rock, setRock] = React.useState('');
  const [note, setNote] = React.useState('');
  const chip = on => ({
    background: on ? 'var(--ink,#d4d4d4)' : 'transparent',
    border: '1px solid ' + (on ? 'var(--ink,#d4d4d4)' : 'var(--line,#3a3a3a)'),
    color: on ? 'var(--bg-0,#262626)' : 'var(--ink-faint,#888)',
    padding: '2px 9px', fontSize: 11, cursor: 'pointer'
  });
  const rocks = goalsIndex ? goalsIndex.goals.filter(g => g.horizon === 'rock' && g.status !== 'done') : [];
  const submit = () => {
    if (!title.trim()) { setNote('title required'); return; }
    const done = r => {
      if (!r.ok) { setNote(r.error); return; }
      setTitle(''); setDue(''); setNote('');
      reloadTeam();
      onDone();
    };
    if (mode === 'me') {
      TEAM_API.post('api/team/items', { kind: kind, title: title.trim(), due: due, rock: rock }).then(done);
    } else {
      TEAM_API.post('api/team/proposals', { kind: kind, title: title.trim(), due: due, rock: rock, target: email }).then(done);
    }
  };
  return (
    <section style={{ border: '1px solid var(--line,#3a3a3a)', background: 'var(--bg-1,#1e1e1e)', padding: 14, marginTop: 14, maxWidth: 760 }}>
      <div style={{ display: 'flex', gap: 8, flexWrap: 'wrap', alignItems: 'center' }}>
        <span className="v2-label">FOR</span>
        <button style={chip(mode === 'me')} onClick={() => setMode('me')}>for me</button>
        <button style={chip(mode === 'other')} onClick={() => setMode('other')}>propose for…</button>
        <input className="v2-input" value={mode === 'me' ? (me ? me.email : '') : email}
          onChange={e => { setEmail(e.target.value); setMode('other'); }}
          placeholder="name@aion.bio" style={{ flex: 1, minWidth: 180 }} />
      </div>
      <div style={{ display: 'flex', gap: 8, flexWrap: 'wrap', alignItems: 'center', marginTop: 10 }}>
        <span className="v2-label">KIND</span>
        <button style={chip(kind === 'task')} onClick={() => setKind('task')}>task</button>
        <button style={chip(kind === 'decision')} onClick={() => setKind('decision')}>decision</button>
        <span className="v2-label" style={{ marginLeft: 10 }}>DUE</span>
        <input className="v2-input" type="date" value={due} onChange={e => setDue(e.target.value)} />
        <select className="v2-input" value={rock} onChange={e => setRock(e.target.value)} style={{ maxWidth: 240 }}>
          <option value="">no rock</option>
          {rocks.map(g => <option key={g.id} value={g.id}>{g.title}</option>)}
        </select>
      </div>
      <textarea className="v2-input" value={title} onChange={e => setTitle(e.target.value)} rows={2}
        placeholder="what needs doing / deciding" style={{ width: '100%', marginTop: 10, fontSize: 12.5, padding: 8, resize: 'vertical' }} />
      <div style={{ display: 'flex', gap: 10, alignItems: 'center', marginTop: 9, flexWrap: 'wrap' }}>
        <button className="v2-btn v2-accentfill" onClick={submit}
          style={{ borderColor: 'var(--accent,#0091ea)', color: 'var(--accent,#0091ea)', padding: '4px 13px' }}>
          {mode === 'me' ? 'add → POST /items' : 'propose → POST /proposals'}
        </button>
        <span style={{ fontSize: 11, color: 'var(--ink-mute,#666)' }}>
          {note || (mode === 'me' ? 'owned by you, lands immediately' : 'pending until the target or an admin decides')}
        </span>
      </div>
    </section>
  );
}

function WorkProposals({ me, team, reloadTeam }) {
  const U = window.PORTAL_UTIL;
  const pending = ((team && team.proposals) || []).filter(p => p.status === 'pending');
  const canDecide = p => me && (me.admin || (p.target || '').toLowerCase() === (me.email || '').toLowerCase());
  const decide = (p, approve) =>
    TEAM_API.post('api/team/proposals/decide', { id: p.id, approve: approve }).then(() => reloadTeam());
  return (
    <section id="sec-proposals" style={{ padding: '18px 0 4px' }}>
      <div style={{ display: 'flex', alignItems: 'baseline', gap: 10, borderBottom: '1px solid var(--line,#3a3a3a)', paddingBottom: 5 }}>
        <span className="v2-label" style={{ letterSpacing: '.18em' }}>PROPOSED TO YOU</span>
        <span style={{ marginLeft: 'auto', fontSize: 11, color: 'var(--ink-mute,#666)' }}>{pending.length}</span>
      </div>
      {pending.map(p => (
        <div key={p.id} style={{ display: 'grid', gridTemplateColumns: 'minmax(0,1fr) auto', gap: 12, alignItems: 'center',
          borderBottom: '1px solid var(--line-soft,#2a2a2a)', padding: '9px 2px' }}>
          <div style={{ minWidth: 0 }}>
            <div style={{ fontSize: 12.5 }}>
              {p.title} {me && p.target === me.email && <span style={{ fontSize: 10, color: 'var(--accent,#0091ea)' }}>for you</span>}
            </div>
            <div style={{ fontSize: 11, color: 'var(--ink-mute,#666)', marginTop: 2 }}>
              {'for ' + (p.target_owner || p.target) + ' · by ' + (p.proposed_name || p.proposed_by) +
                ' · ' + (p.due ? 'due ' + U.fmtDateShort(p.due) : 'no due') + ' · ' + p.kind}
            </div>
          </div>
          <div style={{ display: 'flex', gap: 7 }}>
            <button className="v2-btn v2-hoveraccent" onClick={() => { if (canDecide(p)) decide(p, true); }}
              style={{ color: canDecide(p) ? 'var(--ink,#d4d4d4)' : 'var(--ink-mute,#555)', padding: '2px 9px',
                cursor: canDecide(p) ? 'pointer' : 'not-allowed' }}>approve</button>
            <button className="v2-btn v2-hoverwarnb" onClick={() => { if (canDecide(p)) decide(p, false); }}
              style={{ color: canDecide(p) ? 'var(--ink,#d4d4d4)' : 'var(--ink-mute,#555)', padding: '2px 9px',
                cursor: canDecide(p) ? 'pointer' : 'not-allowed' }}>reject</button>
          </div>
        </div>
      ))}
      {pending.length === 0 && <div style={{ color: 'var(--ink-mute,#666)', fontSize: 12, padding: '8px 0' }}>none pending</div>}
    </section>
  );
}

function WorkView({ data, items, goalsIndex, me, team, filter, addOpen, onToggleAdd, onSelect, pin, reloadTeam }) {
  const D = window.PORTAL_DERIVE;
  const U = window.PORTAL_UTIL;
  const teamOn = !!(team && me && !me.anon);
  const work = D.workSections(items, goalsIndex, filter, me ? me.initials : '',
    teamOn ? team : null, U.todayISO(), onSelect, pin);
  return (
    <div>
      <div style={{ display: 'flex', alignItems: 'center', gap: 14, padding: '12px 0 10px',
        borderBottom: '1px solid var(--line,#3a3a3a)', flexWrap: 'wrap' }}>
        <div style={{ fontSize: 11, color: 'var(--ink-mute,#666)' }}>{work.countLine}</div>
        {teamOn && (
          <button className="v2-btn v2-hoveraccent" onClick={onToggleAdd}
            style={{ marginLeft: 'auto', color: 'var(--accent,#0091ea)', padding: '3px 11px' }}>
            {addOpen ? 'close' : '+ add'}
          </button>
        )}
      </div>

      {addOpen && teamOn && (
        <WorkAddForm me={me} goalsIndex={goalsIndex} onDone={onToggleAdd} reloadTeam={reloadTeam} />
      )}

      {teamOn && <WorkProposals me={me} team={team} reloadTeam={reloadTeam} />}

      {work.sections.map(g => (
        <section key={g.key} style={{ padding: '18px 0 4px' }}>
          <div style={{ display: 'flex', alignItems: 'baseline', gap: 10, borderBottom: '1px solid var(--line,#3a3a3a)', paddingBottom: 5 }}>
            <button className="v2-bare v2-hoveraccent-t" onClick={g.pin}
              style={{ color: g.color, fontSize: 13, textAlign: 'left' }}>{g.title}</button>
            <span style={{ fontSize: 10, letterSpacing: '.14em', color: 'var(--ink-mute,#666)' }}>{g.horizon}</span>
            <span style={{ marginLeft: 'auto', fontSize: 11, color: 'var(--ink-mute,#666)' }}>{g.count}</span>
          </div>
          <div style={{ display: 'flex', flexDirection: 'column' }}>
            {g.rows.map((r, i) => (
              <button key={r.id || i} className="v2-bare v2-hoverbg" onClick={r.open}
                style={{ display: 'grid', gridTemplateColumns: '18px minmax(0,1fr) 108px 46px 84px', gap: 12,
                  alignItems: 'baseline', textAlign: 'left', borderBottom: '1px solid var(--line-soft,#2a2a2a)',
                  padding: '7px 4px', color: 'var(--ink,#d4d4d4)' }}>
                <span style={{ color: r.markColor, fontSize: 12 }}>{r.mark}</span>
                <span style={{ fontSize: 12.5 }}>
                  {r.title} {r.badge && <span style={{ fontSize: 10, color: 'var(--accent,#0091ea)' }}>{r.badge}</span>}
                </span>
                <span style={{ fontSize: 11, color: 'var(--ink-faint,#888)' }}>{r.ownerName}</span>
                <span style={{ fontSize: 11, color: 'var(--ink-mute,#666)' }}>{r.comments}</span>
                <span style={{ fontSize: 11, color: r.dueColor, textAlign: 'right' }}>{r.due}</span>
              </button>
            ))}
          </div>
        </section>
      ))}
      {work.empty && <div style={{ color: 'var(--ink-mute,#666)', fontSize: 12, padding: '14px 0' }}>no open work matches this scope</div>}
    </div>
  );
}

Object.assign(window, { WorkView, WorkAddForm, WorkProposals });
