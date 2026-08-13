/* Todo tab — task outline grouped under rocks, collapsed weekly archive,
   open decisions, and the append-only log (never truncated). */

function Todo({ data, goalsIndex, filter, jump }) {
  const U = window.PORTAL_UTIL;
  const backlog = data.backlog;
  const today = U.todayISO();

  if (!backlog) {
    return (
      <div className="todo-tab">
        <div className="ov-head" id="sec-todo"><span className="ov-head-title">TODO</span></div>
        <div className="no-data">no data yet</div>
        <DriftNote errors={data.errors} keys={['backlog']} />
      </div>
    );
  }

  const items = backlog.items;
  const openTasks = items.filter(it => it.kind === 'task' && (it.status === 'open' || it.status === 'in_progress'));
  const doneTasks = items.filter(it => it.kind === 'task' && it.status === 'done');
  const openDecisions = items.filter(it => it.kind === 'decision' && it.status !== 'decided');
  const decidedLog = items.filter(it => it.kind === 'decision' && it.status === 'decided')
    .slice().sort((a, b) => ((b.decided || '') < (a.decided || '') ? -1 : 1));

  // group open tasks under their matched goal; free-slug / null rocks →
  // unanchored (hidden while a rock filter is pinned, per the design)
  const byGoal = {};
  const unanchored = [];
  openTasks.forEach(t => {
    const gid = goalsIndex ? goalsIndex.matchRock(t.rock) : null;
    if (gid) (byGoal[gid] = byGoal[gid] || []).push(t);
    else unanchored.push(t);
  });
  const sortTasks = list => list.slice().sort((a, b) => {
    if (a.due && b.due) return a.due < b.due ? -1 : 1;
    if (a.due) return -1;
    if (b.due) return 1;
    return 0;
  });

  const groups = [];
  if (goalsIndex) {
    goalsIndex.goals.forEach(g => {
      if (!byGoal[g.id]) return;
      if (filter && !goalsIndex.inFilter(g.id, filter)) return;
      const horizon = g.horizon === '1yr' ? '[12 mo]' : g.horizon === 'rock' ? '[90]' : '[30]';
      groups.push({ id: g.id, title: g.title, horizon, tasks: sortTasks(byGoal[g.id]) });
    });
  }
  if (!filter && unanchored.length) {
    groups.push({ id: '_un', title: 'unanchored', horizon: '', tasks: sortTasks(unanchored) });
  }

  return (
    <div className="todo-tab">
      <div className="dec-block" id="sec-decisions">
        <div className="ov-head"><span className="ov-head-title">DECISIONS</span></div>
        <div className="dec-open">
          {openDecisions.length === 0 && <div className="no-data">none open</div>}
          {openDecisions.map(d => (
            <div className="dec-row" key={d.id}>
              <span className="dec-title">{d.title}</span>
              <span className="dec-meta">
                decides: <span className="dec-who">{d.owner || '—'}</span>
                {d.needed_by ? ' · ' + d.needed_by : ''}
              </span>
            </div>
          ))}
        </div>

        <div className="log-block">
          <div className="log-head">log</div>
          <div className="log-rows">
            {decidedLog.length === 0 && <div className="no-data">nothing decided yet</div>}
            {decidedLog.map(d => (
              <div className="log-row" key={d.id}>
                <span className="log-date">{d.decided || '—'}</span>
                <span className="log-title">{d.title}</span>
                <span className="log-outcome">{d.outcome ? '→ ' + d.outcome : ''}</span>
              </div>
            ))}
          </div>
        </div>
      </div>

      <div className="ov-head" id="sec-todo"><span className="ov-head-title">TODO</span></div>

      <div className="todo-groups">
        {groups.length === 0 && <div className="no-data">no open tasks{filter ? ' under this rock' : ''}</div>}
        {groups.map(g => (
          <div key={g.id}>
            <div className="todo-group-head">
              <span className="todo-group-title">{g.title}</span>
              {g.horizon && <span className="todo-group-h">{g.horizon}</span>}
            </div>
            <div className="todo-rows">
              {g.tasks.map((t, i) => {
                const overdue = t.due && t.due < today;
                return (
                  <div className="todo-row" key={t.id}>
                    <span className="gutter">{i === g.tasks.length - 1 ? '└── ' : '├── '}</span>
                    <span className="todo-mark">{U.statusMark(t.status)}</span>
                    <span className="todo-title">{t.title}</span>
                    <span className="todo-owner">{t.owner ? '@' + t.owner : ''}</span>
                    <span className={'due-label' + (overdue ? ' is-overdue' : '')}>
                      {t.due ? 'due ' + t.due.slice(5) : ''}
                    </span>
                  </div>
                );
              })}
            </div>
          </div>
        ))}
      </div>

      <Archive doneTasks={doneTasks} />

      <DriftNote errors={data.errors} keys={['backlog', 'goals']} />
    </div>
  );
}

/* done tasks grouped by ISO week of done_on, newest first, collapsed */
function Archive({ doneTasks }) {
  const U = window.PORTAL_UTIL;
  const defaultOpen = !!(window.PORTAL_CONFIG && window.PORTAL_CONFIG.archiveOpenByDefault);
  const [open, setOpen] = React.useState({});

  const weeks = {};
  doneTasks.forEach(t => {
    const key = t.done_on ? U.isoWeekKey(t.done_on) : null;
    const k = key || '0000-00-00';
    (weeks[k] = weeks[k] || []).push(t);
  });
  const weekKeys = Object.keys(weeks).sort().reverse();

  return (
    <div className="archive-block">
      <div className="archive-head">archive</div>
      {weekKeys.length === 0 && <div className="no-data">nothing archived yet</div>}
      <div className="archive-weeks">
        {weekKeys.map(k => {
          const isOpen = open[k] != null ? open[k] : defaultOpen;
          const its = weeks[k].slice().sort((a, b) => ((b.done_on || '') < (a.done_on || '') ? -1 : 1));
          const label = k === '0000-00-00' ? 'undated' : U.weekLabel(k);
          return (
            <div key={k}>
              <div
                className="archive-week-head"
                onClick={() => setOpen(o => ({ ...o, [k]: !isOpen }))}
              >{label} — {its.length} done {isOpen ? '▾' : '▸'}</div>
              {isOpen && (
                <div className="archive-items">
                  {its.map((t, i) => (
                    <div className="archive-row" key={t.id}>
                      <span className="gutter">{i === its.length - 1 ? '└── ' : '├── '}</span>
                      <span className="todo-mark">●</span>
                      <span className="archive-title">{t.title}</span>
                      <span className="todo-owner">{t.owner ? '@' + t.owner : ''}</span>
                      <span className="archive-date">{t.done_on ? t.done_on.slice(5) : ''}</span>
                    </div>
                  ))}
                </div>
              )}
            </div>
          );
        })}
      </div>
    </div>
  );
}

Object.assign(window, { Todo });
