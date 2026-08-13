/* Overview tab — TODAY header, the agency field with its detail panel
   (THIS WEEK by default, person/goal/rock/decision detail on selection),
   then the secondary strip: CAPITAL / HIRING / SCIENCE. */

function Overview({ data, goalsIndex, filter, onFilter, jump }) {
  const U = window.PORTAL_UTIL;
  const today = U.todayISO();

  // agency-field selection → the right-hand detail panel. Goal and rock
  // selections reuse the rock-filter pin (rock ids are goal ids);
  // person/decision selections persist here until cleared.
  const [agencySel, setAgencySel] = React.useState(null);
  const handleAgencySelect = React.useCallback(detail => {
    if (detail.type === 'goal' || detail.type === 'rock') {
      setAgencySel(null);
      onFilter(detail.id);
    } else if (detail.type === 'person') {
      setAgencySel({ type: 'person', id: detail.id, aliases: detail.item.aliases });
    } else if (detail.type === 'decision') {
      setAgencySel({ type: 'decision', item: detail.item.portal });
    } else {
      setAgencySel(null);
      onFilter(null);
    }
  }, [onFilter]);
  const clearAgency = React.useCallback(() => setAgencySel(null), []);

  return (
    <div>
      <div className="cone-block">
        <SetpointHead vto={data.vto} today={today} />
        <div className="cone-layout">
          <AgencyField data={data} goalsIndex={goalsIndex} onSelect={handleAgencySelect} />
          <FieldPanel
            data={data}
            goalsIndex={goalsIndex}
            filter={filter}
            selection={agencySel}
            today={today}
            onFilter={onFilter}
            onClearSel={clearAgency}
            jump={jump}
          />
        </div>
      </div>

      <div className="ov-columns">
        <CapitalBlock finances={data.finances} errors={data.errors} />
        <HiringBlock hiring={data.hiring} errors={data.errors} />
        <ScienceBlock references={data.references} errors={data.errors} />
      </div>
      <DriftNote errors={data.errors} keys={['finances', 'vto', 'goals', 'backlog', 'meta']} />
    </div>
  );
}

/* Header band — the collective set point (core focus purpose) with the
   10-year / 3-year horizons in an aligned label column beneath it. */
function SetpointHead({ vto, today }) {
  const purpose = (vto && vto.core_focus && vto.core_focus.purpose) || null;
  const tenYear = vto && vto.ten_year_target;
  const threeYear = vto && vto.three_year_picture;
  const threeYearText = threeYear && (threeYear.measurables || [])[0];

  return (
    <div className="setpoint-head">
      <div className="sp-kicker-row">
        <span className="sp-kicker">COLLECTIVE SET POINT</span>
        <span className="sp-today">TODAY · {today}</span>
      </div>
      <div className="sp-statement">{purpose ? purpose.toUpperCase() : 'no data yet'}</div>
      {(tenYear || threeYearText) && (
        <div className="sp-horizons">
          {tenYear && (
            <div className="sp-h">
              <span className="sp-h-k">10-YEAR · {new Date().getFullYear() + 10}</span>
              <span className="sp-h-t">{tenYear}</span>
            </div>
          )}
          {threeYearText && (
            <div className="sp-h">
              <span className="sp-h-k">3-YEAR{threeYear.date ? ' · ' + threeYear.date.toUpperCase() : ''}</span>
              <span className="sp-h-t">{threeYearText}</span>
            </div>
          )}
        </div>
      )}
    </div>
  );
}

/* Detail panel beside the field. Precedence mirrors the old inspector:
   person/decision selection > pinned goal/rock filter > THIS WEEK default. */
function FieldPanel({ data, goalsIndex, filter, selection, today, onFilter, onClearSel, jump }) {
  const U = window.PORTAL_UTIL;
  const items = (data.backlog && data.backlog.items) || [];
  const openTasks = items.filter(t => t.kind === 'task' && (t.status === 'open' || t.status === 'in_progress'));
  const openDecisions = items.filter(i => i.kind === 'decision' && i.status !== 'decided');
  const decided = items
    .filter(i => i.kind === 'decision' && i.status === 'decided' && i.decided)
    .sort((a, b) => (a.decided < b.decided ? 1 : -1));
  const goalOf = it => { const gid = goalsIndex ? goalsIndex.matchRock(it.rock) : null; return gid ? goalsIndex.get(gid) : null; };
  const clearAll = () => { onFilter(null); onClearSel(); };
  const trunc = (s, n) => (s && s.length > n ? s.slice(0, n - 1) + '…' : s);
  const horizonLabel = h => h === 'rock' ? '90 day' : h === '30' ? '30 day' : '12 month';

  const taskRow = t => {
    const goal = goalOf(t);
    const overdue = t.due && t.due < today;
    return (
      <div className="fp-row" key={t.id}>
        <span className="fp-mark">{U.statusMark(t.status)}</span>
        <span className="fp-row-main">
          <span className="fp-row-title">{t.title}</span>
          <span className="fp-row-meta">
            {[goal ? goal.title : null, t.owner ? '@' + t.owner : null].filter(Boolean).join(' · ')}
          </span>
        </span>
        {t.due && (
          <span className={'due-label' + (overdue ? ' is-overdue' : '')}>
            {(overdue ? 'overdue ' : 'due ') + t.due.slice(5)}
          </span>
        )}
      </div>
    );
  };
  const decisionRow = (d, withOutcome) => (
    <div className="fp-row" key={d.id}>
      <span className="fp-mark">◆</span>
      <span className="fp-row-main">
        <span className="fp-row-title">{d.title}</span>
        <span className="fp-row-meta">
          {[d.decided || (d.needed_by ? 'needed by ' + d.needed_by : 'open'), d.owner ? '@' + d.owner : null].filter(Boolean).join(' · ')}
        </span>
        {withOutcome && d.outcome && <span className="fp-row-meta">→ {trunc(d.outcome, 90)}</span>}
      </span>
    </div>
  );
  const section = (title, rows, emptyText) => (
    <div className="fp-section" key={title}>
      <div className="fp-section-title">{title}</div>
      {rows && rows.length ? rows : <div className="no-data">{emptyText}</div>}
    </div>
  );

  if (selection && selection.type === 'person' && goalsIndex) {
    const aliases = selection.aliases || [selection.id];
    const rec = ((data.people && data.people.people) || []).find(p => aliases.includes(p.initials));
    const tasks = openTasks.filter(t => aliases.includes(t.owner));
    const owned = goalsIndex.goals.filter(g => aliases.includes(g.owner) && g.status !== 'done');
    const mainGoal = owned.find(g => g.horizon === 'rock') || owned[0] || null;
    const openDecs = openDecisions.filter(d => aliases.includes(d.owner));
    return (
      <div className="field-panel">
        <div className="fp-head">
          <span className="fp-head-title">@{selection.id}</span>
          <span className="fp-clear" onClick={clearAll}>clear ✕</span>
        </div>
        <div className="fp-head-meta">{rec ? [rec.name, rec.role].filter(Boolean).join(' · ') : ''}</div>
        {mainGoal && (
          <div className="fp-section">
            <div className="fp-section-title">MAIN GOAL</div>
            <div className="fp-row fp-row--link" onClick={() => onFilter(mainGoal.id)}>
              <span className="fp-mark">{U.statusMark(mainGoal.status)}</span>
              <span className="fp-row-main">
                <span className="fp-row-title">{mainGoal.title}</span>
                <span className="fp-row-meta">{horizonLabel(mainGoal.horizon)}</span>
              </span>
            </div>
          </div>
        )}
        {section('OPEN TASKS', tasks.map(taskRow), 'no open tasks')}
        {section('OPEN DECISIONS', openDecs.map(d => decisionRow(d, false)), 'no open decisions')}
        <div className="fp-links">
          <span className="cone-i-link" onClick={() => jump('todo', null)}>see in todo</span>
        </div>
      </div>
    );
  }

  if (selection && selection.type === 'decision') {
    const d = selection.item;
    const goal = goalOf(d);
    return (
      <div className="field-panel">
        <div className="fp-head">
          <span className="fp-head-title">◆ DECISION</span>
          <span className="fp-clear" onClick={clearAll}>clear ✕</span>
        </div>
        <div className="fp-head-meta">
          {[d.decided ? 'decided ' + d.decided : 'open', d.owner ? '@' + d.owner : null].filter(Boolean).join(' · ')}
        </div>
        <div className="fp-decision-title">{d.title}</div>
        {goal && (
          <div className="fp-row-meta fp-row--link" onClick={() => onFilter(goal.id)}>
            {U.statusMark(goal.status)} {goal.title}
          </div>
        )}
        {d.outcome && <div className="fp-outcome">{d.outcome}</div>}
        <div className="fp-links">
          <span className="cone-i-link" onClick={() => jump('todo', 'sec-decisions')}>past log</span>
        </div>
      </div>
    );
  }

  if (filter && goalsIndex && goalsIndex.get(filter)) {
    const g = goalsIndex.get(filter);
    const isDone = g.status === 'done';
    const inSub = it => { const gid = goalsIndex.matchRock(it.rock); return !!gid && goalsIndex.inFilter(gid, g.id); };
    const tasks = openTasks.filter(inSub);
    const openDecs = openDecisions.filter(inSub);
    const decidedHere = decided.filter(inSub).slice(0, 5);
    return (
      <div className="field-panel">
        <div className="fp-head">
          <span className="fp-head-title">{U.statusMark(g.status)} {g.title}</span>
          <span className="fp-clear" onClick={clearAll}>clear ✕</span>
        </div>
        <div className="fp-head-meta">
          {[isDone ? ('completed ' + (g.closed || '')) : horizonLabel(g.horizon), g.quarter || null, g.owner ? 'accountable @' + g.owner : null].filter(Boolean).join(' · ')}
        </div>
        {section('OPEN TASKS', tasks.map(taskRow), isDone ? 'none — rock is done' : 'no open tasks')}
        {section('OPEN DECISIONS', openDecs.map(d => decisionRow(d, false)), 'no open decisions')}
        {isDone && decidedHere.length > 0 && section('DECIDED HERE', decidedHere.map(d => decisionRow(d, true)), '')}
        <div className="fp-links">
          <span className="cone-i-link" onClick={() => jump('goals', 'sec-timeline')}>see in timeline</span>
          <span className="cone-i-link" onClick={() => jump('todo', null)}>see in todo</span>
        </div>
      </div>
    );
  }

  // default view stays short on purpose: only what's due this week; the
  // full open/undated list lives on the todo tab behind `all open items`
  const horizon = U.addDays(today, 7);
  const week = openTasks
    .filter(t => t.due && t.due <= horizon)
    .sort((a, b) => (a.due < b.due ? -1 : 1));
  return (
    <div className="field-panel">
      <div className="fp-head"><span className="fp-head-title">THIS WEEK</span></div>
      <div className="fp-section">
        {week.length ? week.map(taskRow) : <div className="no-data">nothing due this week</div>}
      </div>
      <div className="fp-links">
        <span className="cone-i-link" onClick={() => jump('todo', null)}>all open items ({openTasks.length})</span>
      </div>
    </div>
  );
}

function CapitalBlock({ finances, errors }) {
  const U = window.PORTAL_UTIL;
  const empty = !finances
    || (finances.capital == null && finances.monthly_burn == null && finances.runway_months == null);
  const money = v => v == null ? '—' : U.fmtMoney(v, finances.currency);
  return (
    <div className="side-block">
      <div className="side-head">CAPITAL</div>
      {empty ? (
        <div className="no-data">no data yet</div>
      ) : (
        <div className="cap-rows">
          <div className="cap-row"><span className="cap-k">OPERATING CAPITAL</span><span className="cap-v">{money(finances.capital)}</span></div>
          <div className="cap-row"><span className="cap-k">MONTHLY BURN</span><span className="cap-v">{money(finances.monthly_burn)}</span></div>
          <div className="cap-row"><span className="cap-k">RUNWAY</span><span className="cap-v">{finances.runway_months == null ? '—' : finances.runway_months + ' mo'}</span></div>
          {finances.as_of && <div className="cap-asof">as of {finances.as_of}</div>}
          {finances.note && <div className="cap-note">{finances.note}</div>}
        </div>
      )}
    </div>
  );
}

/* hiring.md rows via [key:: value] fields, sorted by priority when present */
function HiringBlock({ hiring, errors }) {
  const U = window.PORTAL_UTIL;
  let rows = null;
  if (hiring != null) {
    rows = U.parseFieldFile(hiring)
      .map(r => ({
        role: r.fields.role || r.rest,
        candidate: r.fields.candidate || '',
        stage: r.fields.stage || '',
        next: r.fields.next || '',
        priority: r.fields.priority ? parseInt(r.fields.priority, 10) : null,
        extra: r.fields.role ? r.rest : ''
      }))
      .filter(r => r.role);
    rows.sort((a, b) => (a.priority == null ? 1e9 : a.priority) - (b.priority == null ? 1e9 : b.priority));
  }
  return (
    <div className="side-block">
      <div className="side-head">HIRING</div>
      {rows === null ? (
        <div className="no-data">no data yet</div>
      ) : rows.length === 0 ? (
        <div className="no-data">no open roles</div>
      ) : (
        <div className="hire-rows">
          {rows.map((r, i) => (
            <div className="hire-row" key={i}>
              <span className="hire-num">{r.priority != null ? '#' + r.priority : ''}</span>
              <span className="hire-role">{r.role}{r.extra ? ' ' + r.extra : ''}</span>
              <span className="hire-meta">
                {[r.candidate, r.stage, r.next].filter(Boolean).join(' · ')}
              </span>
            </div>
          ))}
        </div>
      )}
    </div>
  );
}

/* references.md → ├──/└── link tree; [url::] makes the title a link */
function ScienceBlock({ references, errors }) {
  const U = window.PORTAL_UTIL;
  let rows = null;
  if (references != null) {
    rows = U.parseFieldFile(references)
      .map(r => ({
        title: r.rest,
        url: r.fields.url || null,
        source: r.fields.source || '',
        date: r.fields.date || ''
      }))
      .filter(r => r.title);
  }
  return (
    <div className="side-block">
      <div className="side-head">SCIENCE</div>
      {rows === null ? (
        <div className="no-data">no data yet</div>
      ) : rows.length === 0 ? (
        <div className="no-data">no references yet</div>
      ) : (
        <div className="sci-rows">
          {rows.map((r, i) => (
            <div className="sci-row" key={i}>
              <span className="gutter">{i === rows.length - 1 ? '└── ' : '├── '}</span>
              <span className="sci-title">
                {r.url
                  ? <a href={r.url} target="_blank" rel="noopener">{r.title}</a>
                  : r.title}
                {(r.source || r.date) && (
                  <span className="sci-meta"> {[r.source, r.date].filter(Boolean).join(' · ')}</span>
                )}
              </span>
            </div>
          ))}
        </div>
      )}
    </div>
  );
}

Object.assign(window, { Overview });
