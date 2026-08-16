/* PortalApp — tab + rock-filter + drawer-selection state, cross-tab jumps,
   hash routing. Config knobs live in window.PORTAL_CONFIG (index.html) so
   plain scripts can read them at load.

   Phase 2 (2026-08-14): the static password gate is gone. The portal now
   requires Google sign-in (@aion.bio) to VIEW — enforced server-side
   (server/portal.go requireSignIn); team writes ride the same session. */

function PortalApp() {
  const CONFIG = window.PORTAL_CONFIG || {};
  return <Portal config={CONFIG} />;
}

/* Team overlay: published backlog + team/ items, with team field overrides
   applied on top (status changes, done marks, due edits). */
function mergedBacklog(backlog, team) {
  if (!backlog) return backlog;
  let items = backlog.items.slice();
  if (team && Array.isArray(team.items)) items = items.concat(team.items);
  if (team && team.overrides) {
    items = items.map(it => {
      const ov = team.overrides[it.id];
      return ov && ov.fields ? Object.assign({}, it, ov.fields) : it;
    });
  }
  return Object.assign({}, backlog, { items });
}

// #hash → landing tab + anchor (used by the /roadmap redirect stub)
function parseHash() {
  const h = (location.hash || '').replace('#', '');
  if (h === 'gantt' || h === 'timeline') return { tab: 'goals', anchor: 'sec-timeline' };
  if (h === 'decisions') return { tab: 'task', anchor: 'sec-decisions' };
  if (h === 'todo') return { tab: 'task', anchor: null }; // legacy #todo → the renamed tab
  if (h === 'goals' || h === 'task' || h === 'overview') return { tab: h, anchor: null };
  return null;
}

function Portal({ config }) {
  const U = window.PORTAL_UTIL;
  const initial = parseHash();
  const [tab, setTab] = React.useState((initial && initial.tab) || config.defaultTab || 'overview');
  const [filter, setFilter] = React.useState(null); // pinned rock (goal id) — survives tab switches
  const [selection, setSelection] = React.useState(null); // timeline drawer {kind, id}
  const [teamSel, setTeamSel] = React.useState(null); // team item drawer (item id)
  const [data, setData] = React.useState(null);

  React.useEffect(() => {
    window.loadPortalData().then(setData);
  }, []);

  // team writes → refetch only the server-side overlay (team state + me)
  const reloadTeam = React.useCallback(() => {
    const U = window.PORTAL_UTIL;
    Promise.all([U.fetchJSON('api/team/state'), U.fetchJSON('api/me')]).then(rs => {
      setData(d => d ? Object.assign({}, d, {
        team: rs[0].ok ? rs[0].value : d.team,
        me: rs[1].ok ? rs[1].value : d.me
      }) : d);
    });
  }, []);

  // Esc closes the drawers
  React.useEffect(() => {
    const onKey = (e) => { if (e.key === 'Escape') { setSelection(null); setTeamSel(null); } };
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

  // team overlay merged over the published backlog (memo keyed on both)
  const viewData = React.useMemo(() => {
    if (!data) return data;
    return Object.assign({}, data, { backlog: mergedBacklog(data.backlog, data.team) });
  }, [data]);

  if (!data) {
    return (
      <div className="portal-shell">
        <Masthead meta={null} />
        <div className="no-data" style={{ padding: '40px 0' }}>loading…</div>
      </div>
    );
  }

  const filterGoal = filter && goalsIndex ? goalsIndex.get(filter) : null;

  const teamItem = teamSel && viewData.backlog
    ? viewData.backlog.items.find(it => it.id === teamSel)
    : null;

  return (
    <div className="portal-shell">
      <Masthead meta={data.meta} me={data.me} onAuthChange={reloadTeam} />
      <TabNav tab={tab} onTab={onTab} />

      <div className="portal-body">
        {tab !== 'overview' && (
          <FilterChip filterGoal={filterGoal} onClear={() => setFilter(null)} />
        )}

        {tab === 'overview' && (
          <Overview
            data={viewData}
            goalsIndex={goalsIndex}
            filter={filter}
            onFilter={setFilter}
            jump={jump}
          />
        )}

        {tab === 'goals' && (
          <div>
            {goalsIndex || data.vto
              ? <Vto data={viewData} goalsIndex={goalsIndex} filter={filter} onFilter={setFilter} jump={jump} />
              : <div className="no-data">no data yet</div>}
            <Heuristics heuristics={data.heuristics} errors={data.errors} />
            <div id="sec-timeline" className="timeline-block">
              <div className="ov-head"><span className="ov-head-title">TIMELINE</span></div>
              <Timeline
                data={viewData}
                filter={filter}
                goalsIndex={goalsIndex}
                selection={selection}
                onSelect={setSelection}
              />
            </div>
            <DriftNote errors={data.errors} keys={['vto', 'goals', 'heuristics', 'backlog']} />
          </div>
        )}

        {tab === 'task' && (
          <Tasks
            data={viewData}
            goalsIndex={goalsIndex}
            filter={filter}
            jump={jump}
            me={data.me}
            team={data.team}
            onOpenItem={setTeamSel}
            onTeamChange={reloadTeam}
          />
        )}
      </div>

      <Footer meta={data.meta} />

      {selection && (
        <TimelineDrawer
          selection={selection}
          data={viewData}
          goalsIndex={goalsIndex}
          onClose={() => setSelection(null)}
        />
      )}

      {teamItem && (
        <TeamItemDrawer
          item={teamItem}
          me={data.me}
          team={data.team}
          onClose={() => setTeamSel(null)}
          onChange={reloadTeam}
        />
      )}
    </div>
  );
}

ReactDOM.createRoot(document.getElementById('root')).render(<PortalApp />);
