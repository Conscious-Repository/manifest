/* V/TO sheet — the 01–08 manifest form. Numbered sections, hairlines, type
   stepping from 44px Alchimia (10-year target) down to 13px measurables.
   Seeded placeholder lines (leading "—") render visibly generic. */

const isPlaceholder = s => typeof s === 'string' && s.trim().charAt(0) === '—';

function VtoLine({ text, size }) {
  return (
    <div className={'vto-line' + (isPlaceholder(text) ? ' is-placeholder' : '') + (size === 'sm' ? ' vto-line-sm' : '')}>
      {text}
    </div>
  );
}

function VtoSection({ num, title, meta, id, children }) {
  return (
    <>
      <div className="vto-head" id={id}>
        <span className="vto-num">{num}</span>
        <span className="vto-title">{title}</span>
        {meta && <span className="vto-meta">{meta}</span>}
      </div>
      <div className="vto-body">{children}</div>
    </>
  );
}

function Vto({ data, goalsIndex, filter, onFilter, jump }) {
  const U = window.PORTAL_UTIL;
  const vto = data.vto;
  const backlog = data.backlog;

  if (!vto && !goalsIndex) return <div className="no-data">no data yet</div>;

  const openDecisions = backlog
    ? backlog.items.filter(it => it.kind === 'decision' && it.status !== 'decided').length
    : null;

  // 06 rows: one_year_plan.goals ids where resolvable, else all 1yr goals
  let yearGoals = [];
  if (goalsIndex) {
    const ids = vto && vto.one_year_plan && Array.isArray(vto.one_year_plan.goals)
      ? vto.one_year_plan.goals.filter(id => goalsIndex.get(id))
      : [];
    yearGoals = (ids.length
      ? ids
      : goalsIndex.goals.filter(g => g.horizon === '1yr' && g.status !== 'done').map(g => g.id))
      .map(id => goalsIndex.get(id));
  }
  const measurables = (vto && vto.one_year_plan && vto.one_year_plan.measurables) || [];

  // 07 lists live rocks only — closed ones are archived in the timeline
  // below (quarter board) and in the overview field's past cone.
  const allRocks = goalsIndex ? goalsIndex.goals.filter(g => g.horizon === 'rock') : [];
  const rocks = allRocks.filter(g => g.status !== 'done');
  const closedCount = allRocks.length - rocks.length;

  // one row per rock, ordered by the first 1-year goal it serves; a rock
  // serving several lists them all on its `serves` line rather than repeating
  const rockInstances = [];
  if (goalsIndex) {
    const yearGoalsAll = goalsIndex.goals.filter(g => g.horizon === '1yr');
    const byRock = new Map();
    yearGoalsAll.forEach(y => {
      goalsIndex.childrenIds(y.id)
        .map(id => goalsIndex.get(id))
        .filter(g => g && g.horizon === 'rock' && g.status !== 'done')
        .forEach(r => {
          if (!byRock.has(r.id)) {
            const inst = { rock: r, serves: [] };
            byRock.set(r.id, inst);
            rockInstances.push(inst);
          }
          byRock.get(r.id).serves.push(y);
        });
    });
    rocks.filter(r => !byRock.has(r.id))
      .forEach(r => rockInstances.push({ rock: r, serves: [] }));
    // a rock serving every 1-year goal gets the short form
    rockInstances.forEach(inst => {
      inst.servesAll = yearGoalsAll.length > 1 && inst.serves.length === yearGoalsAll.length;
    });
  }

  let quarterLine = null;
  if (vto && vto.quarter && vto.quarter.end) {
    const start = vto.quarter.start || vto.quarter.end;
    const q = Math.floor(parseInt(start.slice(5, 7), 10) / 3.01) + 1;
    quarterLine = 'Q' + q + ' · ends ' + vto.quarter.end + ' · ' + rocks.length + ' rocks'
      + (closedCount ? ' · ' + closedCount + ' closed' : '');
  }

  const goalRow = (g, measurable, big) => (
    <div key={g.id}>
      <div
        className={'goal-row' + (big ? ' is-big' : '') + (filter === g.id ? ' is-sel' : '')}
        onClick={() => onFilter(filter === g.id ? null : g.id)}
      >
        <span className="goal-mark">{U.statusMark(g.status)}</span>
        <span className="goal-title">{g.title}</span>
        {g.owner && <span className="goal-owner">@{g.owner}</span>}
      </div>
      {measurable && <div className={'goal-measurable' + (isPlaceholder(measurable) ? ' is-placeholder' : '')}>{measurable}</div>}
    </div>
  );

  return (
    <div className="vto">
      <VtoSection num="01" title="CORE VALUES">
        {vto && Array.isArray(vto.core_values) && vto.core_values.length
          ? vto.core_values.map((v, i) => <VtoLine key={i} text={v} />)
          : <div className="no-data">no data yet</div>}
      </VtoSection>

      <VtoSection num="02" title="CORE FOCUS">
        {vto && vto.core_focus ? (
          <>
            <div className="vto-kv"><span className="vto-k">PURPOSE</span><VtoLine text={vto.core_focus.purpose} /></div>
            <div className="vto-kv"><span className="vto-k">NICHE</span><VtoLine text={vto.core_focus.niche} /></div>
          </>
        ) : <div className="no-data">no data yet</div>}
      </VtoSection>

      <VtoSection num="03" title="10-YEAR TARGET">
        {vto && vto.ten_year_target
          ? <div className="vto-target">{vto.ten_year_target.toUpperCase()}</div>
          : <div className="no-data">no data yet</div>}
      </VtoSection>

      <VtoSection num="04" title="3-YEAR PICTURE" meta={vto && vto.three_year_picture && vto.three_year_picture.date}>
        {vto && vto.three_year_picture && (vto.three_year_picture.measurables || []).length
          ? vto.three_year_picture.measurables.map((m, i) => <VtoLine key={i} text={m} />)
          : <div className="no-data">no data yet</div>}
      </VtoSection>

      <VtoSection num="05" title="WHO WE SERVE FIRST">
        {vto && vto.marketing_strategy ? (
          <>
            <div className="vto-kv"><span className="vto-k">TARGET</span><VtoLine text={vto.marketing_strategy.target} /></div>
            {(vto.marketing_strategy.uniques || []).map((u, i) => (
              <div className="vto-kv" key={i}><span className="vto-k">{i === 0 ? 'UNIQUES' : ''}</span><VtoLine text={u} /></div>
            ))}
          </>
        ) : <div className="no-data">no data yet</div>}
      </VtoSection>

      <VtoSection num="06" title="1-YEAR GOALS" meta={vto && vto.one_year_plan && vto.one_year_plan.date ? 'to ' + vto.one_year_plan.date : null}>
        {yearGoals.length
          ? <div className="goal-list">{yearGoals.map((g, i) => goalRow(g, measurables[i], true))}</div>
          : <div className="no-data">no data yet</div>}
      </VtoSection>

      <VtoSection num="07" title="ROCKS" meta={quarterLine} id="sec-rocks">
        {rockInstances.length ? (
          <div className="rock-list">
            {rockInstances.map(({ rock: r, serves, servesAll }) => {
              // live 30-day children only — the row hardcodes the "30 day" label
              const kids = goalsIndex.childrenIds(r.id)
                .map(id => goalsIndex.get(id))
                .filter(k => k && k.horizon === '30' && k.status !== 'done');
              const servesLabel = !serves.length ? 'unanchored'
                : servesAll ? `all ${serves.length} 1-year goals`
                : serves.map(y => y.title).join(' · ');
              return (
                <div key={r.id}>
                  {goalRow(r, null, false)}
                  <div className="rock-serves">serves {servesLabel}</div>
                  {kids.length > 0 && (
                    <div className="rock-kids">
                      {kids.map((k, i) => (
                        <div className="rock-kid" key={k.id}>
                          <span className="gutter">{i === kids.length - 1 ? '└── ' : '├── '}</span>
                          <span className="goal-mark">{U.statusMark(k.status)}</span>
                          <span className="rock-kid-title">{k.title}</span>
                          {k.owner && <span className="goal-owner">@{k.owner}</span>}
                          <span className="rock-kid-h">30 day</span>
                        </div>
                      ))}
                    </div>
                  )}
                </div>
              );
            })}
          </div>
        ) : <div className="no-data">no data yet</div>}
      </VtoSection>

      <VtoSection num="08" title="ISSUES">
        {openDecisions === null
          ? <div className="no-data">no data yet</div>
          : (
            <span className="issues-link" onClick={() => jump('task', 'sec-decisions')}>
              {openDecisions} open
            </span>
          )}
      </VtoSection>
    </div>
  );
}

Object.assign(window, { Vto });
