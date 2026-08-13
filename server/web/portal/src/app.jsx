/* PortalApp — gate wrapper, tab + rock-filter + drawer-selection state,
   cross-tab jumps, hash routing. Config knobs live in window.PORTAL_CONFIG
   (index.html) so plain scripts can read them at load. */

function PortalApp() {
  const CONFIG = window.PORTAL_CONFIG || {};
  const [authed, setAuthed] = React.useState(
    sessionStorage.getItem('portal_auth') === 'true');

  if (!authed) return <Gate onUnlock={() => setAuthed(true)} />;
  return <Portal config={CONFIG} />;
}

// #hash → landing tab + anchor (used by the /roadmap redirect stub)
function parseHash() {
  const h = (location.hash || '').replace('#', '');
  if (h === 'gantt' || h === 'timeline') return { tab: 'goals', anchor: 'sec-timeline' };
  if (h === 'decisions') return { tab: 'todo', anchor: 'sec-decisions' };
  if (h === 'goals' || h === 'todo' || h === 'overview') return { tab: h, anchor: null };
  return null;
}

function Portal({ config }) {
  const U = window.PORTAL_UTIL;
  const initial = parseHash();
  const [tab, setTab] = React.useState((initial && initial.tab) || config.defaultTab || 'overview');
  const [filter, setFilter] = React.useState(null); // pinned rock (goal id) — survives tab switches
  const [selection, setSelection] = React.useState(null); // timeline drawer {kind, id}
  const [data, setData] = React.useState(null);

  React.useEffect(() => {
    window.loadPortalData().then(setData);
  }, []);

  // Esc closes the timeline drawer
  React.useEffect(() => {
    const onKey = (e) => { if (e.key === 'Escape') setSelection(null); };
    window.addEventListener('keydown', onKey);
    return () => window.removeEventListener('keydown', onKey);
  }, []);

  const jump = React.useCallback((toTab, anchorId) => {
    setTab(toTab);
    const hash = anchorId === 'sec-timeline' ? '#gantt'
      : anchorId === 'sec-decisions' ? '#decisions'
      : '#' + toTab;
    history.replaceState(null, '', hash);
    requestAnimationFrame(() => requestAnimationFrame(() => {
      if (!anchorId) { window.scrollTo({ top: 0, behavior: 'smooth' }); return; }
      const el = document.getElementById(anchorId);
      if (el) window.scrollTo({
        top: window.scrollY + el.getBoundingClientRect().top - 80,
        behavior: 'smooth'
      });
    }));
  }, []);

  // land on the hash anchor once data is in (bars need data to exist)
  const landedRef = React.useRef(false);
  React.useEffect(() => {
    if (!data || landedRef.current) return;
    landedRef.current = true;
    const h = parseHash();
    if (h && h.anchor) jump(h.tab, h.anchor);
  }, [data, jump]);

  const onTab = (id) => {
    setTab(id);
    history.replaceState(null, '', '#' + id);
    window.scrollTo({ top: 0 });
  };

  const goalsIndex = React.useMemo(
    () => (data && data.goals ? U.buildGoalIndex(data.goals) : null),
    [data]);

  if (!data) {
    return (
      <div className="portal-shell">
        <Masthead meta={null} />
        <div className="no-data" style={{ padding: '40px 0' }}>loading…</div>
      </div>
    );
  }

  const filterGoal = filter && goalsIndex ? goalsIndex.get(filter) : null;

  return (
    <div className="portal-shell">
      <Masthead meta={data.meta} />
      <TabNav tab={tab} onTab={onTab} />

      <div className="portal-body">
        {tab !== 'overview' && (
          <FilterChip filterGoal={filterGoal} onClear={() => setFilter(null)} />
        )}

        {tab === 'overview' && (
          <Overview
            data={data}
            goalsIndex={goalsIndex}
            filter={filter}
            onFilter={setFilter}
            jump={jump}
          />
        )}

        {tab === 'goals' && (
          <div>
            {goalsIndex || data.vto
              ? <Vto data={data} goalsIndex={goalsIndex} filter={filter} onFilter={setFilter} jump={jump} />
              : <div className="no-data">no data yet</div>}
            <Heuristics heuristics={data.heuristics} errors={data.errors} />
            <div id="sec-timeline" className="timeline-block">
              <div className="ov-head"><span className="ov-head-title">TIMELINE</span></div>
              <Timeline
                data={data}
                filter={filter}
                goalsIndex={goalsIndex}
                selection={selection}
                onSelect={setSelection}
              />
            </div>
            <DriftNote errors={data.errors} keys={['vto', 'goals', 'heuristics', 'backlog']} />
          </div>
        )}

        {tab === 'todo' && (
          <Todo data={data} goalsIndex={goalsIndex} filter={filter} jump={jump} />
        )}
      </div>

      <Footer meta={data.meta} />

      {selection && (
        <TimelineDrawer
          selection={selection}
          data={data}
          goalsIndex={goalsIndex}
          onClose={() => setSelection(null)}
        />
      )}
    </div>
  );
}

ReactDOM.createRoot(document.getElementById('root')).render(<PortalApp />);
