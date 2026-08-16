/* Timeline — a quarter board derived purely from manifest exports. The data
   is quarter-granular (every rock carries `quarter`; per-rock start/due just
   restate quarter bounds), so the view matches: one column per quarter from
   the earliest rock to one quarter past today, each rock exactly once in its
   quarter (multi-parent rocks are tagged with the 1-year goals they serve,
   not duplicated). Click a card → drawer of live tasks/decisions. */

/* "2026-Q3" → sortable index; index → "Q3 '26" */
function qIndexOf(q) {
  const m = /^(\d{4})-Q([1-4])$/.exec(q || '');
  return m ? (+m[1]) * 4 + (+m[2]) - 1 : null;
}
function qLabelOf(qi) {
  return 'Q' + ((qi % 4) + 1) + " '" + String(Math.floor(qi / 4)).slice(2);
}
function qOfDate(iso) {
  return (+iso.slice(0, 4)) * 4 + Math.floor((+iso.slice(5, 7) - 1) / 3);
}

/* window for a goal: explicit start/due → its quarter → the vto quarter
   (used by the drawer subtitle) */
function goalWindow(g, vto) {
  const U = window.PORTAL_UTIL;
  const q = U.parseQuarter(g.quarter) ||
    (vto && vto.quarter && vto.quarter.start ? vto.quarter : null);
  const start = g.start || (q && q.start) || null;
  const end = g.due || (q && q.end) || null;
  return start && end ? { start, end, dated: !!(g.start || g.due) } : null;
}

function buildQuarterBoard(data, goalsIndex, todayISO) {
  const items = (data.backlog && data.backlog.items) || [];
  const inSubtree = (item, rockId) => {
    const gid = goalsIndex.matchRock(item.rock);
    return !!gid && goalsIndex.inFilter(gid, rockId);
  };

  const rocks = goalsIndex.goals.filter(g => g.horizon === 'rock' && qIndexOf(g.quarter) != null);
  if (!rocks.length) return null;

  const currentQ = qOfDate(todayISO);
  const idxs = rocks.map(g => qIndexOf(g.quarter));
  const first = Math.min(...idxs);
  const last = Math.max(Math.max(...idxs), currentQ + 1);

  const columns = [];
  for (let qi = first; qi <= last; qi++) {
    const cards = rocks
      .filter(g => qIndexOf(g.quarter) === qi)
      .map(g => ({
        goal: g,
        serves: (g.serves_all && g.serves_all.length ? g.serves_all : (g.serves ? [g.serves] : []))
          .map(id => goalsIndex.get(id))
          .filter(Boolean),
        kids: goalsIndex.childrenIds(g.id)
          .map(id => goalsIndex.get(id))
          .filter(k => k && k.horizon === '30'),
        openTasks: items.filter(it => it.kind === 'task'
          && (it.status === 'open' || it.status === 'in_progress')
          && inSubtree(it, g.id)).length,
        openDecisions: items.filter(it => it.kind === 'decision'
          && it.status !== 'decided'
          && inSubtree(it, g.id)).length
      }));
    columns.push({ qi, label: qLabelOf(qi), current: qi === currentQ, past: qi < currentQ, cards });
  }
  return columns;
}

function Timeline({ data, goalsIndex, filter, selection, onSelect }) {
  const U = window.PORTAL_UTIL;
  const today = U.todayISO();

  const columns = React.useMemo(
    () => (goalsIndex ? buildQuarterBoard(data, goalsIndex, today) : null),
    [data, goalsIndex, today]);

  if (!goalsIndex || !columns) return <div className="no-data">no data yet</div>;

  const match = g => !filter
    || goalsIndex.inFilter(g.id, filter)
    || goalsIndex.inFilter(filter, g.id);
  const visible = columns.map(c => ({ ...c, cards: c.cards.filter(card => match(card.goal)) }));

  if (!visible.some(c => c.cards.length)) {
    return <div className="qb-none">no rocks match the current filter</div>;
  }

  return (
    <div className="qboard">
      {visible.map(col => (
        <div key={col.qi} className={'qb-col' + (col.current ? ' is-current' : '') + (col.past ? ' is-past' : '')}>
          <div className="qb-col-head">
            <span className="qb-col-q">{col.label}</span>
            {col.current && <span className="qb-col-now">today · {today.slice(5)}</span>}
          </div>
          <div className="qb-cards">
            {col.cards.length === 0 && (
              <div className="qb-empty">{col.past ? '—' : 'nothing planned yet'}</div>
            )}
            {col.cards.map(card => (
              <QuarterCard key={card.goal.id} card={card} selection={selection} onSelect={onSelect} />
            ))}
          </div>
        </div>
      ))}
    </div>
  );
}

function QuarterCard({ card, selection, onSelect }) {
  const U = window.PORTAL_UTIL;
  const g = card.goal;
  const done = g.status === 'done';
  const isSel = selection && selection.id === g.id;
  const open = e => { e.stopPropagation(); onSelect({ kind: 'goal', id: g.id }); };

  const meta = done
    ? 'closed ' + (g.closed ? g.closed.slice(5) : '')
    : [
        g.owner ? '@' + g.owner : null,
        card.openTasks ? card.openTasks + ' tasks' : null,
        card.openDecisions ? card.openDecisions + (card.openDecisions === 1 ? ' decision' : ' decisions') : null
      ].filter(Boolean).join(' · ') || 'no open items';

  return (
    <div
      className={'qb-card' + (done ? ' is-done' : '') + (isSel ? ' is-selected' : '')}
      role="button"
      tabIndex={0}
      onClick={open}
      onKeyDown={e => { if (e.key === 'Enter' || e.key === ' ') { e.preventDefault(); open(e); } }}
    >
      <div className="qb-card-title">
        <span className="qb-mark">{U.statusMark(g.status)}</span>
        <span>{g.title}</span>
      </div>
      <div className="qb-card-meta">{meta}</div>
      {card.serves.length > 0 && (
        <div className="qb-tags">
          {card.serves.map(s => (
            <span key={s.id} className="qb-tag" title={s.title}>{s.id.split('/').pop()}</span>
          ))}
        </div>
      )}
      {!done && card.kids.length > 0 && (
        <div className="qb-kids">
          {card.kids.map(k => (
            <div
              key={k.id}
              className="qb-kid"
              onClick={e => { e.stopPropagation(); onSelect({ kind: 'goal', id: k.id }); }}
              title={k.title + (k.owner ? ' @' + k.owner : '')}
            >
              <span className="qb-mark">{U.statusMark(k.status)}</span>
              <span className="qb-kid-title">{k.title}</span>
              {k.owner && <span className="qb-kid-owner">@{k.owner}</span>}
            </div>
          ))}
        </div>
      )}
    </div>
  );
}

/* drawer — everything in it is live backlog/goals data */
function TimelineDrawer({ selection, data, goalsIndex, onClose }) {
  const U = window.PORTAL_UTIL;
  if (!selection || !goalsIndex) return null;
  const g = goalsIndex.get(selection.id);
  if (!g) return null;

  const items = (data.backlog && data.backlog.items) || [];
  const inSub = it => {
    const gid = goalsIndex.matchRock(it.rock);
    return !!gid && goalsIndex.inFilter(gid, g.id);
  };
  const openTasks = items.filter(it => it.kind === 'task'
    && (it.status === 'open' || it.status === 'in_progress') && inSub(it));
  const openDecisions = items.filter(it => it.kind === 'decision'
    && it.status !== 'decided' && inSub(it));
  const recentDone = items.filter(it => it.kind === 'task'
    && it.status === 'done' && inSub(it))
    .sort((a, b) => ((b.done_on || '') < (a.done_on || '') ? -1 : 1))
    .slice(0, 5);

  const win = goalWindow(g, data.vto);
  const horizon = g.horizon === 'rock' ? '90 day' : g.horizon === '30' ? '30 day' : '1 year';
  const statusLabel = g.closed ? 'closed'
    : g.status === 'done' ? 'done'
    : g.status === 'in_progress' ? 'in progress' : 'open';

  return (
    <>
      <div className="drawer-scrim" onClick={onClose} />
      <aside className="drawer" role="dialog" aria-modal="true">
        <button className="drawer-close" onClick={onClose} aria-label="close">[esc] close</button>
        <div className="drawer-body">
          <div className="drawer-kicker">
            <span>{U.statusMark(g.status)} {horizon} · {statusLabel}</span>
          </div>
          <h2 className="drawer-title">{g.title}</h2>
          <div className="drawer-subtitle">
            {win ? U.fmtDateShort(win.start) + ' → ' + U.fmtDateShort(win.end) + (win.dated ? '' : ' · quarter default') : 'no window'}
          </div>

          <dl className="drawer-meta">
            <div><dt>owner</dt><dd>{g.owner ? U.personName(g.owner) : '—'}</dd></div>
            {g.quarter && <div><dt>quarter</dt><dd>{g.quarter}</dd></div>}
          </dl>

          <div className="drawer-section">
            <div className="drawer-section-label">open tasks ({openTasks.length})</div>
            {openTasks.length === 0 && <div className="no-data">none linked yet</div>}
            <ul className="drawer-gate-list">
              {openTasks.map(t => (
                <li key={t.id}>
                  <div className="drawer-gate-row">
                    <span>{U.statusMark(t.status)}</span>
                    <span className="drawer-gate-title">{t.title}</span>
                    <span className="drawer-gate-due">{t.owner ? '@' + t.owner : ''}{t.due ? ' · ' + t.due.slice(5) : ''}</span>
                  </div>
                </li>
              ))}
            </ul>
          </div>

          {openDecisions.length > 0 && (
            <div className="drawer-section">
              <div className="drawer-section-label">open decisions ({openDecisions.length})</div>
              <ul className="drawer-gate-list">
                {openDecisions.map(d => (
                  <li key={d.id}>
                    <div className="drawer-gate-row">
                      <span>◆</span>
                      <span className="drawer-gate-title">{d.title}</span>
                      <span className="drawer-gate-due">{d.owner ? '@' + d.owner : ''}{d.needed_by ? ' · ' + d.needed_by : ''}</span>
                    </div>
                  </li>
                ))}
              </ul>
            </div>
          )}

          {recentDone.length > 0 && (
            <div className="drawer-section">
              <div className="drawer-section-label">recently done</div>
              <ul className="drawer-gate-list">
                {recentDone.map(t => (
                  <li key={t.id}>
                    <div className="drawer-gate-row">
                      <span>●</span>
                      <span className="drawer-gate-title">{t.title}</span>
                      <span className="drawer-gate-due">{t.done_on ? t.done_on.slice(5) : ''}</span>
                    </div>
                  </li>
                ))}
              </ul>
            </div>
          )}
        </div>
      </aside>
    </>
  );
}

Object.assign(window, { Timeline, TimelineDrawer });
