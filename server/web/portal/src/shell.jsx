/* Portal v2 shell — the left rail and the theme modal (design handoff).
   Structural labels are lowercase; labels 10px/.16em; radius 0 everywhere. */

function Rail({ view, counts, go, filterGoal, filterExplain, onClearFilter, teamOn,
  pendingCount, onAdd, onProposals, me, themeName, onThemes, chatUrl }) {
  const nav = [
    { id: 'field', label: 'field', count: '' },
    { id: 'work', label: 'work', count: counts.work },
    { id: 'goals', label: 'goals', count: counts.goals },
    { id: 'archive', label: 'archive', count: '' }
  ];
  return (
    <aside className="v2-rail">
      <div style={{ padding: '0 18px' }}>
        <div role="img" aria-label="AION" className="v2-logo" />
        <div style={{ fontSize: 11, letterSpacing: '.2em', color: 'var(--ink-faint,#888)', marginTop: 6 }}>team-portal</div>
      </div>

      <nav style={{ display: 'flex', flexDirection: 'column' }}>
        {nav.map(n => (
          <button key={n.id} className="v2-navbtn" onClick={() => go(n.id)}
            style={{ borderLeft: '2px solid ' + (view === n.id ? 'var(--accent,#0091ea)' : 'transparent'),
              color: view === n.id ? 'var(--ink,#d4d4d4)' : 'var(--ink-faint,#888)' }}>
            <span style={{ color: 'var(--ink-mute,#555)', fontSize: 11 }}>{view === n.id ? '▸' : '·'}</span>
            <span>{n.label}</span>
            <span style={{ marginLeft: 'auto', fontSize: 11, color: 'var(--ink-mute,#666)' }}>{n.count}</span>
          </button>
        ))}
        <a className="v2-navbtn v2-navlink" href={chatUrl} target="_blank" rel="noopener">
          <span style={{ color: 'var(--ink-mute,#555)', fontSize: 11 }}>·</span>
          <span>chat</span>
          <span style={{ marginLeft: 'auto', fontSize: 11, color: 'var(--ink-mute,#555)' }}>↗</span>
        </a>
      </nav>

      {filterGoal && (
        <div style={{ margin: '0 18px', border: '1px solid var(--line,#3a3a3a)', background: 'var(--bg-1,#1e1e1e)', padding: '9px 10px' }}>
          <div className="v2-label">SHOWING ONLY</div>
          <div style={{ fontSize: 12, color: 'var(--accent,#0091ea)', marginTop: 4 }}>{filterGoal.title}</div>
          <div style={{ fontSize: 11, color: 'var(--ink-faint,#777)', marginTop: 5 }}>{filterExplain}</div>
          <button className="v2-btn v2-hoverline" onClick={onClearFilter}
            style={{ marginTop: 7, color: 'var(--ink-faint,#888)', padding: '2px 7px' }}>show everything ✕</button>
        </div>
      )}

      {teamOn && (
        <div style={{ margin: '0 18px', display: 'flex', flexDirection: 'column', gap: 6 }}>
          <button className="v2-btn v2-hoveraccent" onClick={onAdd}
            style={{ color: 'var(--accent,#0091ea)', padding: '4px 8px', textAlign: 'left' }}>+ add / propose</button>
          <button className="v2-btn v2-hoverline" onClick={onProposals}
            style={{ display: 'flex', justifyContent: 'space-between', color: 'var(--ink-faint,#888)', padding: '4px 8px', textAlign: 'left' }}>
            <span>proposals</span><span style={{ color: 'var(--accent,#0091ea)' }}>{pendingCount}</span>
          </button>
        </div>
      )}

      <div style={{ marginTop: 'auto', padding: '14px 18px 0', borderTop: '1px solid var(--line,#3a3a3a)',
        fontSize: 11, color: 'var(--ink-faint,#777)', display: 'flex', flexDirection: 'column', gap: 5 }}>
        <button className="v2-bare v2-hoverink" onClick={onThemes}
          style={{ display: 'flex', gap: 8, alignItems: 'center', width: '100%', color: 'var(--ink-mute,#666)',
            fontSize: 11, padding: '0 0 9px', textAlign: 'left' }}>
          <span style={{ width: 11, height: 11, background: 'var(--accent,#0091ea)', border: '1px solid var(--line,#3a3a3a)', flex: 'none' }} />
          <span style={{ whiteSpace: 'nowrap', overflow: 'hidden', textOverflow: 'ellipsis' }}>theme · {themeName}</span>
        </button>
        {me && !me.anon && (
          <div style={{ display: 'flex', flexDirection: 'column', gap: 5 }}>
            <div style={{ color: 'var(--ink,#d4d4d4)', overflow: 'hidden', textOverflow: 'ellipsis' }}>{me.email}</div>
            <div style={{ color: 'var(--ink-mute,#666)' }}>{me.admin ? 'admin' : 'member'}</div>
            <div style={{ display: 'flex', gap: 8 }}>
              <a href="#" style={{ color: 'var(--ink-faint,#888)' }}
                onClick={e => { e.preventDefault(); fetch('oauth2/logout', { method: 'POST' }).finally(() => { location.href = '/'; }); }}>sign out</a>
            </div>
          </div>
        )}
        <div style={{ color: 'var(--ink-mute,#555)', fontSize: 10, letterSpacing: '.12em', paddingBottom: 4 }}>confidential · internal</div>
      </div>
    </aside>
  );
}

function ThemeModal({ activeId, onPick, onClose }) {
  const T = window.PORTAL_THEMES;
  return (
    <div onClick={onClose} className="v2-modal-scrim">
      <div onClick={e => e.stopPropagation()} className="v2-modal">
        <div style={{ display: 'flex', alignItems: 'baseline', gap: 10, padding: '14px 16px 10px', borderBottom: '1px solid var(--line,#3a3a3a)' }}>
          <span className="v2-label" style={{ letterSpacing: '.18em' }}>THEME</span>
          <button className="v2-bare v2-hoverink" onClick={onClose}
            style={{ marginLeft: 'auto', color: 'var(--ink-mute,#666)', fontSize: 11 }}>esc ✕</button>
        </div>
        <div style={{ display: 'flex', flexDirection: 'column', padding: '6px 0 10px' }}>
          {T.THEMES.map(t => (
            <button key={t.id} className="v2-bare v2-hoverbg2" onClick={() => onPick(t.id)}
              style={{ display: 'flex', gap: 12, alignItems: 'center', width: '100%', padding: '8px 16px', textAlign: 'left',
                background: t.id === activeId ? 'var(--sel,#242424)' : 'transparent' }}>
              <span style={{ width: 16, height: 16, flex: 'none', background: t.bg[0],
                border: '1px solid ' + (t.id === activeId ? t.ink[0] : T.mix(t.bg[0], t.ink[0], 0.3)),
                boxShadow: 'inset 0 0 0 3px ' + t.accent[0] }} />
              <span style={{ fontSize: 13, whiteSpace: 'nowrap', color: t.id === activeId ? 'var(--ink,#d4d4d4)' : 'var(--ink-dim,#aaa)' }}>{t.name}</span>
              <span style={{ marginLeft: 'auto', fontSize: 10, letterSpacing: '.16em', color: 'var(--ink-mute,#666)' }}>
                {t.mode === 'light' ? 'LIGHT' : (t.id === activeId ? 'ACTIVE' : '')}
              </span>
            </button>
          ))}
        </div>
      </div>
    </div>
  );
}

Object.assign(window, { Rail, ThemeModal });
