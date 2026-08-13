/* UNUSED — replaced on the overview by the agency field (agency.jsx);
   kept on disk in case the light cone returns. Not loaded by index.html.

   Cognitive light cone — geometry ported verbatim from the approved design.
   Fixed 1240×480 inner space scaled with transform to fit the container, so
   the apex is on screen at any laptop width. Hover lights a rock's chain,
   hovering an @owner lights that person's rocks, click pins the filter. */

const CONE_W = 1240, CONE_H = 480;
const CONE_BANDS = [
  { x: 300, label: '30 DAY', horizon: '30' },
  { x: 560, label: '90 DAY ROCKS', horizon: 'rock' },
  { x: 830, label: '1 YEAR', horizon: '1yr' }
];
const hcone = x => 200 * (1200 - x) / 1020;

function Cone({ goalsIndex, backlog, vto, filter, onFilter, jump, externalPerson, externalDecision, onClearExternal }) {
  const U = window.PORTAL_UTIL;
  const [hover, setHover] = React.useState(null);
  const [hoverPerson, setHoverPerson] = React.useState(null);
  const [coneW, setConeW] = React.useState(CONE_W);
  const wrapRef = React.useRef(null);

  const measure = React.useCallback(() => {
    if (wrapRef.current) {
      const w = wrapRef.current.clientWidth;
      if (w) setConeW(w);
    }
  }, []);
  React.useEffect(() => {
    measure();
    window.addEventListener('resize', measure);
    return () => window.removeEventListener('resize', measure);
  }, [measure]);

  if (!goalsIndex || !goalsIndex.goals.length) {
    return <div className="cone-empty">no data yet</div>;
  }

  const scale = Math.min(1, coneW / CONE_W);
  const active = hover || filter || null;
  const chain = active ? goalsIndex.chainOf(active) : {};

  // person focus: transient owner-hover wins; otherwise a persistent
  // agency-field selection (which may carry alias initials, e.g. Y/YS)
  const extPerson = (!hover && !hoverPerson && externalPerson) ? externalPerson : null;
  const personAliases = hoverPerson ? [hoverPerson] : (extPerson ? (extPerson.aliases || [extPerson.id]) : null);
  const personLabel = hoverPerson || (extPerson ? extPerson.id : null);

  // open tasks under a goal's subtree (matched rock walks up via serves)
  const openTasks = (backlog && backlog.items || []).filter(
    it => it.kind === 'task' && (it.status === 'open' || it.status === 'in_progress'));
  const taskCount = id => openTasks.filter(t => {
    const start = goalsIndex.matchRock(t.rock);
    if (!start) return false;
    const seen = new Set();
    const queue = [start];
    while (queue.length) {
      const cur = queue.shift();
      if (seen.has(cur)) continue;
      seen.add(cur);
      if (cur === id) return true;
      goalsIndex.parentsOf(cur).forEach(p => queue.push(p));
    }
    return false;
  }).length;

  // positions: goals by horizon, spread vertically inside each band's ellipse
  const pos = {};
  const rocks = [];
  CONE_BANDS.forEach(b => {
    const ids = goalsIndex.goals.filter(g => g.horizon === b.horizon).map(g => g.id);
    const h = hcone(b.x), k = ids.length;
    const step = k > 1 ? Math.min(40, 2 * (h - 26) / (k - 1)) : 0;
    ids.forEach((id, i) => {
      const g = goalsIndex.get(id);
      const y = 240 + (i - (k - 1) / 2) * step;
      pos[id] = { x: b.x, y };
      const lit = personAliases ? personAliases.includes(g.owner) : (!active || chain[id]);
      const sel = filter === id;
      rocks.push({ id, g, x: b.x, y, lit, sel });
    });
  });

  // one link per (goal, parent) pair — a multi-serving rock fans out to
  // every 1-year goal it serves; parentless 1-year goals link to the apex
  const links = [];
  goalsIndex.goals.forEach(g => {
    if (!pos[g.id]) return;
    const parents = goalsIndex.parentsOf(g.id);
    const drawn = parents.filter(p => pos[p]);
    if (drawn.length) {
      drawn.forEach(p => {
        const d = `M${pos[g.id].x} ${pos[g.id].y} L${pos[p].x} ${pos[p].y}`;
        const onChain = active && !personAliases && chain[g.id] && chain[p];
        links.push({ id: g.id + '→' + p, d, stroke: onChain ? '#d4d4d4' : (active || personAliases ? '#2f2f2f' : '#3a3a3a') });
      });
    } else if (!parents.length && g.horizon === '1yr') {
      const d = `M${pos[g.id].x} ${pos[g.id].y} L1200 240`;
      const onChain = active && !personAliases && chain[g.id];
      links.push({ id: g.id + '→apex', d, stroke: onChain ? '#d4d4d4' : (active || personAliases ? '#2f2f2f' : '#3a3a3a') });
    }
  });

  // inspector line under the cone — precedence: rock hover > person >
  // agency-field decision selection > pinned filter
  const iDecision = !hover && !personAliases ? externalDecision : null;
  const iGoal = !personAliases && !iDecision && active ? goalsIndex.get(active) : null;
  const personRocks = personAliases ? goalsIndex.goals.filter(g => personAliases.includes(g.owner)) : [];
  const personTodos = personAliases ? openTasks.filter(t => personAliases.includes(t.owner)).length : 0;
  const horizonLabel = h => h === 'rock' ? '90 day' : h === '30' ? '30 day' : '12 month';
  const inspector = personAliases ? {
    kind: 'person',
    mark: '',
    title: '@' + personLabel,
    meta: personRocks.length + ' rocks · ' + personTodos + ' to-dos · '
      + personRocks.filter(g => g.horizon === '30').length + ' this month'
  } : iGoal ? {
    kind: 'goal',
    mark: U.statusMark(iGoal.status),
    title: iGoal.title,
    meta: horizonLabel(iGoal.horizon)
      + (iGoal.owner ? ' · accountable @' + iGoal.owner : '')
      + ' · ' + taskCount(iGoal.id) + ' open to-dos'
      + ' · ' + (iGoal.status === 'done' ? 'done' : iGoal.status === 'in_progress' ? 'in progress' : 'open')
  } : iDecision ? {
    kind: 'decision',
    mark: '◆',
    title: iDecision.title.length > 80 ? iDecision.title.slice(0, 78) + '…' : iDecision.title,
    meta: (iDecision.decided ? 'decided ' + iDecision.decided : 'open')
      + (iDecision.owner ? ' · @' + iDecision.owner : '')
      + (iDecision.outcome ? ' · → ' + (iDecision.outcome.length > 60 ? iDecision.outcome.slice(0, 58) + '…' : iDecision.outcome) : '')
  } : null;

  const target = (vto && vto.ten_year_target) || null;
  const threeYr = vto && vto.three_year_picture && (vto.three_year_picture.measurables || [])[0];

  return (
    <div className="cone-section">
      <div
        ref={wrapRef}
        className="cone-wrap"
        style={{ height: (CONE_H * scale) + 'px' }}
        onMouseLeave={() => { setHover(null); setHoverPerson(null); }}
      >
        <div className="cone-inner" style={{ transform: `scale(${scale})` }}>
          <svg viewBox="0 0 1240 480" preserveAspectRatio="none" className="cone-svg">
            <g fill="none" stroke="#3a3a3a" strokeWidth="1" vectorEffect="non-scaling-stroke">
              <path d="M180 40 L1200 240 L180 440" />
              <path d="M180 40 L60 240 L180 440" stroke="#2f2f2f" />
              <ellipse cx="300" cy="240" rx="15" ry="176" />
              <ellipse cx="560" cy="240" rx="12" ry="125" />
              <ellipse cx="830" cy="240" rx="8" ry="72" />
              <ellipse cx="1030" cy="240" rx="5" ry="33" />
              <ellipse cx="180" cy="240" rx="17" ry="200" stroke="#0022ff" />
            </g>
          </svg>
          <svg viewBox="0 0 1240 480" preserveAspectRatio="none" className="cone-svg cone-svg-links">
            <g fill="none" strokeWidth="1" vectorEffect="non-scaling-stroke">
              {links.map(l => <path key={l.id} d={l.d} stroke={l.stroke} />)}
            </g>
          </svg>

          <div className="cone-now">now</div>
          <div className="cone-pastlog" onClick={() => jump('todo', 'sec-decisions')}>← past log</div>

          <div className="cone-apex">
            <div className="cone-apex-kicker">10-YEAR TARGET</div>
            <div className="cone-apex-target">{target ? target.toUpperCase() : 'no data yet'}</div>
          </div>
          {threeYr && (
            <div className="cone-3yr">3-year picture — {threeYr.replace(/\.$/, '')}</div>
          )}

          {CONE_BANDS.map(b => (
            <div
              key={b.horizon}
              className="cone-band-label"
              style={{ left: (b.x / CONE_W * 100) + '%', top: `calc(50% - ${hcone(b.x) + 26}px)` }}
            >{b.label}</div>
          ))}

          {rocks.map(r => (
            <div
              key={r.id}
              className={'cone-rock' + (r.sel ? ' is-sel' : '') + (active ? ' has-active' : '')}
              style={{
                left: (r.x / CONE_W * 100) + '%',
                top: `calc(50% + ${r.y - 240}px)`,
                opacity: r.lit ? 1 : 0.22
              }}
              onClick={() => onFilter(filter === r.id ? null : r.id)}
              onMouseEnter={() => setHover(r.id)}
              onMouseLeave={() => setHover(null)}
            >
              <span className={'cone-rock-mark' + (r.g.status === 'done' ? ' is-done' : '')}>{U.statusMark(r.g.status)}</span>
              <span className="cone-rock-title">{r.g.title}</span>
              {r.g.owner && (
                <span
                  className={'cone-rock-owner' + (hoverPerson === r.g.owner ? ' is-lit' : '')}
                  onMouseEnter={() => setHoverPerson(r.g.owner)}
                  onMouseLeave={() => setHoverPerson(null)}
                >@{r.g.owner}</span>
              )}
            </div>
          ))}
        </div>
      </div>

      <div className="cone-inspector">
        {inspector && (
          <>
            <span className="cone-i-mark">{inspector.mark}</span>
            <span className="cone-i-title">{inspector.title}</span>
            <span className="cone-i-meta">{inspector.meta}</span>
            {inspector.kind === 'decision' ? (
              <span className="cone-i-link" onClick={() => jump('todo', 'sec-decisions')}>past log</span>
            ) : (
              <>
                <span className="cone-i-link" onClick={() => jump('goals', 'sec-timeline')}>see in timeline</span>
                <span className="cone-i-link" onClick={() => jump('todo', null)}>see in todo</span>
              </>
            )}
            <span
              className="cone-i-clear"
              onClick={() => { onFilter(null); if (onClearExternal) onClearExternal(); }}
            >clear ✕</span>
          </>
        )}
      </div>
    </div>
  );
}

Object.assign(window, { Cone });
