/* Portal v2 top-level app (design handoff). Two-column shell (rail + main);
   the field inspector lives inline, an item replaces main full-width. State
   owner + hash router + theme + Esc/resize. Views resolve through safe() so
   one broken Babel file degrades to a labeled box, never a blank page. */

function safe(name) {
  return window[name] || function Missing() {
    return <div className="no-data" style={{ padding: '20px 0' }}>{name} failed to load — check the console</div>;
  };
}

function parseHash() {
  const h = (location.hash || '').replace('#', '');
  if (h === 'gantt' || h === 'timeline') return { view: 'goals', anchor: 'sec-timeline' };
  if (h === 'decisions' || h === 'proposals') return { view: 'work', anchor: 'sec-proposals' };
  if (h === 'todo' || h === 'task' || h === 'tasks' || h === 'work') return { view: 'work', anchor: null };
  if (h === 'overview' || h === 'field') return { view: 'field', anchor: null };
  if (h === 'library' || h === 'archive') return { view: 'archive', anchor: null };
  if (h === 'goals') return { view: 'goals', anchor: null };
  return null;
}

function PortalApp() {
  const U = window.PORTAL_UTIL;
  const D = window.PORTAL_DERIVE;
  const T = window.PORTAL_THEMES;
  const CONFIG = window.PORTAL_CONFIG || {};

  const initial = parseHash();
  const [view, setView] = React.useState((initial && initial.view) || CONFIG.defaultView || 'field');
  const [filter, setFilter] = React.useState(null);
  const [sel, setSel] = React.useState(null);       // {kind:'item'|'person'|'goal'|'decision'} | 'add' | null
  const [libTab, setLibTab] = React.useState('all');
  const [addOpen, setAddOpen] = React.useState(false);
  const [themeId, setThemeId] = React.useState(T.load);
  const [themeModal, setThemeModal] = React.useState(false);
  const [data, setData] = React.useState(null);
  const [w, setW] = React.useState(typeof window !== 'undefined' ? window.innerWidth : 1440);

  React.useEffect(() => { window.loadPortalData().then(setData); }, []);
  React.useEffect(() => { T.applyBody(themeId); }, [themeId]);

  const reloadTeam = React.useCallback(() => {
    Promise.all([U.fetchJSON('api/team/state'), U.fetchJSON('api/me')]).then(rs => {
      setData(d => d ? Object.assign({}, d, {
        team: rs[0].ok ? rs[0].value : d.team,
        me: rs[1].ok ? rs[1].value : d.me
      }) : d);
    });
  }, []);

  // resize (8px threshold to avoid thrash)
  React.useEffect(() => {
    let last = window.innerWidth;
    const onResize = () => { if (Math.abs(window.innerWidth - last) > 8) { last = window.innerWidth; setW(last); } };
    window.addEventListener('resize', onResize);
    return () => window.removeEventListener('resize', onResize);
  }, []);

  // Esc: close modal first, else clear selection
  React.useEffect(() => {
    const onKey = e => {
      if (e.key !== 'Escape') return;
      setThemeModal(m => { if (m) return false; setSel(null); return false; });
    };
    window.addEventListener('keydown', onKey);
    return () => window.removeEventListener('keydown', onKey);
  }, []);

  const go = React.useCallback((toView, anchorId, tab) => {
    setView(toView);
    setSel(null);
    if (tab) setLibTab(tab);
    const hash = anchorId === 'sec-timeline' ? '#timeline'
      : anchorId === 'sec-proposals' ? '#proposals' : '#' + toView;
    history.replaceState(null, '', hash);
    requestAnimationFrame(() => requestAnimationFrame(() => {
      if (!anchorId) { window.scrollTo({ top: 0 }); return; }
      const el = document.getElementById(anchorId);
      if (el) window.scrollTo({ top: window.scrollY + el.getBoundingClientRect().top - 70, behavior: 'smooth' });
    }));
  }, []);

  const pin = React.useCallback((id) => { setFilter(f => f === id ? null : id); }, []);
  const select = React.useCallback((kind, id) => { setSel({ kind: kind, id: id }); }, []);

  const goalsIndex = React.useMemo(
    () => (data && data.goals ? U.buildGoalIndex(data.goals) : null), [data]);
  const items = React.useMemo(() => {
    if (!data || !data.backlog) return [];
    const merged = D.mergedBacklog(data.backlog, data.team);
    return (merged && merged.items) || [];
  }, [data]);

  const setTheme = id => { setThemeId(id); T.save(id); setThemeModal(false); };

  const Rail = safe('Rail');
  const ThemeModal = safe('ThemeModal');

  if (!data) {
    return (
      <div style={T.tokenObject(themeId)} className="v2-shell v2-shell-1">
        <div className="no-data" style={{ padding: '40px 30px' }}>loading…</div>
      </div>
    );
  }

  const me = data.me || { anon: true };
  const teamOn = !!(data.team && me && !me.anon);
  const filterGoal = filter && goalsIndex ? goalsIndex.get(filter) : null;
  const counts = D.navCounts(items, goalsIndex, filter);
  const pendingCount = String(((data.team && data.team.proposals) || []).filter(p => p.status === 'pending').length);
  const mid = w >= 940;
  const shellCols = mid ? (w >= 1180 ? '214px minmax(0,1fr)' : '190px minmax(0,1fr)') : '1fr';

  const selItem = sel && sel.kind === 'item' ? items.filter(i => i.id === sel.id)[0] : null;

  let main;
  if (selItem) {
    const ItemView = safe('ItemView');
    main = <ItemView item={selItem} me={me} team={data.team} teamOn={teamOn}
      goalsIndex={goalsIndex} filter={filter} onBack={() => { setSel(null); setView('work'); }}
      pin={pin} reloadTeam={reloadTeam} />;
  } else if (view === 'field') {
    const FieldView = safe('FieldView');
    main = <FieldView data={data} items={items} goalsIndex={goalsIndex} me={me} filter={filter}
      sel={sel} w={w} onSelect={select} onClearSel={() => setSel(null)} pin={pin} go={go}
      openFile={hash => window.open('api/team/file/' + hash, '_blank')} />;
  } else if (view === 'work') {
    const WorkView = safe('WorkView');
    main = <WorkView data={data} items={items} goalsIndex={goalsIndex} me={me} team={data.team}
      filter={filter} addOpen={addOpen} onToggleAdd={() => setAddOpen(a => !a)}
      onSelect={select} pin={pin} reloadTeam={reloadTeam} />;
  } else if (view === 'goals') {
    const GoalsView = safe('GoalsView');
    main = <GoalsView data={data} items={items} goalsIndex={goalsIndex} filter={filter} pin={pin} go={go} />;
  } else {
    const ArchiveView = safe('ArchiveView');
    main = <ArchiveView data={data} items={items} goalsIndex={goalsIndex} filter={filter}
      tab={libTab} onTab={setLibTab} onSelect={select} pin={pin} go={go}
      openFile={hash => window.open('api/team/file/' + hash, '_blank')} />;
  }

  const viewLabel = selItem ? 'work / item' : view;
  const metaLine = data.meta && data.meta.published_at
    ? 'last published ' + data.meta.published_at.slice(0, 10) + ' · confidential' : 'confidential';

  return (
    <div className={'v2-shell' + (mid ? '' : ' v2-shell-1')}
      style={Object.assign({}, T.tokenObject(themeId), { gridTemplateColumns: shellCols })}>
      <Rail view={view} counts={counts} go={go} filterGoal={filterGoal}
        filterExplain={D.filterExplain(items, goalsIndex, filter)}
        onClearFilter={() => setFilter(null)} teamOn={teamOn} pendingCount={pendingCount}
        onAdd={() => { setView('work'); setSel(null); setAddOpen(true); }}
        onProposals={() => go('work', 'sec-proposals')} me={me} themeName={T.byId(themeId).name}
        onThemes={() => setThemeModal(true)} chatUrl="https://100.95.45.62:8443/" />

      <main className="v2-main">
        <div className="v2-breadcrumb">
          <div style={{ fontSize: 12, color: 'var(--ink-faint,#777)' }}>portal / <span style={{ color: 'var(--ink,#d4d4d4)' }}>{viewLabel}</span></div>
          <div style={{ fontSize: 11, color: 'var(--ink-mute,#666)', textAlign: 'right' }}>{metaLine}</div>
        </div>
        {main}
      </main>

      {themeModal && <ThemeModal activeId={themeId} onPick={setTheme} onClose={() => setThemeModal(false)} />}
    </div>
  );
}

ReactDOM.createRoot(document.getElementById('root')).render(<PortalApp />);
