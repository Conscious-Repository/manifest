/* Portal v2 — FIELD: the cone hero + YOURS TO DECIDE, the inspector
   (person / goal / decision), THIS WEEK, and the CAPITAL/HIRING/SCIENCE
   strips. Markup mirrors the design prototype exactly. */

function FieldRow({ r }) {
  return (
    <button className="v2-bare v2-hoverbg" onClick={r.open}
      style={{ display: 'grid', gridTemplateColumns: '16px minmax(0,1fr) 96px 70px', gap: 10, alignItems: 'baseline',
        textAlign: 'left', borderBottom: '1px solid var(--line-soft,#2a2a2a)', padding: '7px 2px', color: 'var(--ink,#d4d4d4)' }}>
      <span style={{ color: r.markColor, fontSize: 12 }}>{r.mark}</span>
      <span style={{ fontSize: 12 }}>{r.title}</span>
      <span style={{ fontSize: 11, color: 'var(--ink-faint,#888)' }}>{r.owner}</span>
      <span style={{ fontSize: 11, color: r.dueColor, textAlign: 'right' }}>{r.due}</span>
    </button>
  );
}

function FieldInspector({ sel, data, items, goalsIndex, me, filter, onSelect, onClear, pin, go }) {
  const U = window.PORTAL_UTIL;
  const D = window.PORTAL_DERIVE;
  const today = U.todayISO();
  if (!sel || sel.kind === 'item') return null;
  const label = sel.kind === 'person' ? 'PERSON'
    : sel.kind === 'decision' ? 'DECISION'
    : (goalsIndex.get(sel.id) && goalsIndex.get(sel.id).horizon === 'rock' ? 'ROCK' : 'GOAL');

  let body = null;
  if (sel.kind === 'person') {
    const people = (data.people && data.people.people) || [];
    const p = people.filter(x => x.initials === sel.id)[0] || { initials: sel.id, name: sel.id, role: '' };
    const tasks = items.filter(i => i.kind === 'task' && D.isOpen(i) && i.owner === p.initials);
    const decs = items.filter(i => i.kind === 'decision' && D.isOpen(i) && i.owner === p.initials);
    const goal = goalsIndex.goals.filter(g => g.owner === p.initials && g.horizon === 'rock')[0] ||
      goalsIndex.goals.filter(g => g.owner === p.initials)[0] || null;
    body = (
      <div style={{ display: 'grid', gridTemplateColumns: '260px minmax(0,1fr)', gap: 26, marginTop: 10 }}>
        <div>
          <div style={{ fontFamily: 'Alchimia,serif', fontSize: 30, lineHeight: 1.1, color: 'var(--ink,#e8e8e8)' }}>{p.name}</div>
          <div style={{ fontSize: 11, color: 'var(--ink-faint,#888)', marginTop: 4 }}>{p.role}</div>
          <button className="v2-bare v2-hoverbright" onClick={() => { if (goal) pin(goal.id); }}
            style={{ color: 'var(--accent,#0091ea)', fontSize: 12.5, textAlign: 'left', marginTop: 10 }}>
            {goal ? goal.title : 'no goal resolved'}
          </button>
        </div>
        <div>
          <div className="v2-label">OPEN · {tasks.length}{tasks.length === 1 ? ' task · ' : ' tasks · '}{decs.length}{decs.length === 1 ? ' decision' : ' decisions'}</div>
          <div style={{ display: 'flex', flexDirection: 'column', marginTop: 5 }}>
            {tasks.map(t => (
              <button key={t.id} className="v2-bare v2-hoverbg" onClick={() => onSelect('item', t.id)}
                style={{ display: 'grid', gridTemplateColumns: 'minmax(0,1fr) 70px', gap: 10, textAlign: 'left',
                  borderBottom: '1px solid var(--line-soft,#2a2a2a)', padding: '6px 2px', color: 'var(--ink,#d4d4d4)' }}>
                <span style={{ fontSize: 12 }}>{t.title}</span>
                <span style={{ fontSize: 11, color: (t.due && t.due < today) ? 'var(--warn,#a44)' : 'var(--ink-faint,#888)', textAlign: 'right' }}>
                  {t.due ? U.fmtDateShort(t.due) : '—'}
                </span>
              </button>
            ))}
            {decs.map(d => (
              <button key={d.id} className="v2-bare v2-hoverbg" onClick={() => onSelect('item', d.id)}
                style={{ textAlign: 'left', borderBottom: '1px solid var(--line-soft,#2a2a2a)', padding: '6px 2px',
                  color: 'var(--ink-dim,#aaa)', fontSize: 12 }}>◇ {d.title}</button>
            ))}
          </div>
        </div>
      </div>
    );
  } else if (sel.kind === 'decision') {
    const U2 = U;
    const d = items.filter(i => i.id === sel.id)[0];
    if (!d) return null;
    const gid = D.rockOf(goalsIndex, d);
    const g = gid ? goalsIndex.get(gid) : null;
    body = (
      <div style={{ marginTop: 10, maxWidth: 760 }}>
        <div style={{ fontSize: 11, color: 'var(--ink-faint,#888)' }}>
          {'decided ' + (d.decided ? U2.fmtDateShort(d.decided) : '—') + ' · ' + U2.personName(d.owner)}
        </div>
        <div style={{ fontSize: 15, marginTop: 6 }}>{d.title}</div>
        <div style={{ fontSize: 12.5, color: 'var(--ink-dim,#aaa)', marginTop: 8 }}>→ {d.outcome || 'no outcome recorded'}</div>
        <div style={{ display: 'flex', gap: 14, marginTop: 11, flexWrap: 'wrap' }}>
          <button className="v2-bare v2-underlink" onClick={() => { if (gid) pin(gid); }}
            style={{ color: 'var(--accent,#0091ea)', fontSize: 11 }}>{g ? g.title : 'unanchored'}</button>
          <button className="v2-bare v2-underlink v2-hoverink" onClick={() => onSelect('item', d.id)}
            style={{ color: 'var(--ink-faint,#888)', fontSize: 11 }}>open the record →</button>
        </div>
      </div>
    );
  } else {
    const g = goalsIndex.get(sel.id);
    if (!g) return null;
    const open = items.filter(i => D.isOpen(i) && goalsIndex.inFilter(D.rockOf(goalsIndex, i), g.id));
    const decided = items.filter(i => i.kind === 'decision' && i.status === 'decided' && D.rockOf(goalsIndex, i) === g.id);
    const meta = [g.status, D.hLabel(g.horizon), g.quarter || '', g.owner ? 'accountable ' + U.personName(g.owner) : 'unowned']
      .filter(Boolean).join(' · ');
    body = (
      <div style={{ display: 'grid', gridTemplateColumns: '260px minmax(0,1fr)', gap: 26, marginTop: 10 }}>
        <div>
          <div style={{ fontSize: 15 }}>{g.title}</div>
          <div style={{ fontSize: 11, color: 'var(--ink-faint,#888)', marginTop: 5 }}>{meta}</div>
          <div style={{ display: 'flex', gap: 9, marginTop: 11, flexWrap: 'wrap' }}>
            <button className="v2-btn v2-hoveraccent" onClick={() => pin(g.id)}
              style={{ color: 'var(--accent,#0091ea)', padding: '3px 9px' }}>
              {filter === g.id ? 'unpin filter' : 'pin as filter'}
            </button>
            <button className="v2-btn v2-hoverline" onClick={() => go('goals', 'sec-timeline')}
              style={{ color: 'var(--ink-faint,#888)', padding: '3px 9px' }}>in timeline</button>
          </div>
        </div>
        <div>
          <div className="v2-label">OPEN IN SUBTREE · {open.length}</div>
          <div style={{ display: 'flex', flexDirection: 'column', marginTop: 5 }}>
            {open.map(it => (
              <FieldRow key={it.id} r={window.PORTAL_DERIVE.row(it, data.team, U.todayISO(), onSelect)} />
            ))}
          </div>
          {decided.length > 0 && (
            <div style={{ marginTop: 12 }}>
              <div className="v2-label">DECIDED HERE</div>
              <div style={{ display: 'flex', flexDirection: 'column', marginTop: 4 }}>
                {decided.map(d => (
                  <button key={d.id} className="v2-bare v2-hoverbg" onClick={() => onSelect('decision', d.id)}
                    style={{ textAlign: 'left', borderBottom: '1px solid var(--line-soft,#2a2a2a)', padding: '5px 2px',
                      color: 'var(--ink-dim,#aaa)', fontSize: 12 }}>
                    {(d.decided ? U.fmtDateShort(d.decided) : '—') + ' · ' + d.title}
                  </button>
                ))}
              </div>
            </div>
          )}
        </div>
      </div>
    );
  }

  return (
    <section style={{ borderBottom: '1px solid var(--line,#3a3a3a)', padding: '14px 0' }}>
      <div style={{ display: 'flex', alignItems: 'baseline', gap: 10 }}>
        <span className="v2-label" style={{ letterSpacing: '.18em' }}>{label}</span>
        <button className="v2-bare v2-hoverink" onClick={onClear}
          style={{ marginLeft: 'auto', color: 'var(--ink-mute,#666)', fontSize: 11 }}>clear ✕</button>
      </div>
      {body}
    </section>
  );
}

function FieldView({ data, items, goalsIndex, me, filter, sel, w, onSelect, onClearSel, pin, go, openFile }) {
  const U = window.PORTAL_UTIL;
  const D = window.PORTAL_DERIVE;
  const today = U.todayISO();
  const teamOn = !!(data.team && me && !me.anon);
  const coneCols = w >= 1080 ? 'minmax(0,1fr) 168px' : 'minmax(0,1fr)';
  const coneH = w >= 1440 ? '640px' : w >= 1180 ? '580px' : w >= 940 ? '400px' : '320px';
  const weekCols = w >= 1280 ? 'repeat(2,minmax(0,1fr))' : 'minmax(0,1fr)';

  const my = D.myDecisions(items, goalsIndex, filter, me ? me.initials : '', today,
    onSelect, () => go('work'));
  const week = D.thisWeek(items, goalsIndex, filter, today, teamOn ? data.team : null, onSelect);
  const openCount = items.filter(it => D.isOpen(it) && D.visible(goalsIndex, it, filter)).length;

  const fin = data.finances || {};
  const hiring = U.parseFieldFile(data.hiring || '').map(r => ({
    role: r.fields.role || r.rest, stage: r.fields.stage || '', priority: parseInt(r.fields.priority || '9', 10)
  })).filter(h => h.role).sort((a, b) => a.priority - b.priority);
  const refs = D.paperEntries(data.references || '');

  const onCone = React.useCallback(detail => {
    if (!detail) return;
    if (detail.type === 'clear') { onClearSel(); return; }
    if (detail.type === 'goal' || detail.type === 'rock') { pin(detail.id, true); onSelect('goal', detail.id); return; }
    if (detail.type === 'person') { onSelect('person', detail.id); return; }
    if (detail.type === 'decision') { onSelect('decision', detail.id); }
  }, [onClearSel, onSelect, pin]);

  return (
    <div>
      <section style={{ display: 'grid', gridTemplateColumns: coneCols, gap: 16,
        borderBottom: '1px solid var(--line,#3a3a3a)', padding: '8px 0 14px' }}>
        <div style={{ position: 'relative', minWidth: 0 }} className="v2-conewrap">
          <AgencyField data={data} goalsIndex={goalsIndex} onSelect={onCone}
            selection={sel && sel.kind !== 'item' ? sel : null} />
          <style>{'.v2-conewrap .aaf__svg{height:' + coneH + '}'}</style>
        </div>
        {w >= 1080 && (
          <div style={{ display: 'flex', flexDirection: 'column', gap: 12, fontSize: 10, color: 'var(--ink-faint,#777)', paddingTop: 8 }}>
            <div style={{ letterSpacing: '.16em' }}>YOURS TO DECIDE</div>
            <div style={{ display: 'flex', flexDirection: 'column', gap: 9 }}>
              {my.rows.map((d, i) => (
                <button key={i} className="v2-bare v2-hoverbg" onClick={d.open}
                  style={{ display: 'flex', flexDirection: 'column', gap: 3, alignItems: 'flex-start',
                    borderLeft: '2px solid ' + d.edge, padding: '1px 0 1px 8px', textAlign: 'left', width: '100%' }}>
                  <span style={{ fontSize: 11.5, lineHeight: 1.45, color: 'var(--ink,#d4d4d4)' }}>{d.title}</span>
                  <span style={{ fontSize: 10, color: d.byColor }}>{d.by}</span>
                </button>
              ))}
            </div>
            {my.empty && <div style={{ color: 'var(--ink-mute,#555)', fontSize: 11 }}>nothing waiting on you</div>}
            {my.othersShown && (
              <button className="v2-bare v2-hoverink" onClick={my.goDecisions}
                style={{ marginTop: 'auto', color: 'var(--ink-mute,#666)', fontSize: 10, textAlign: 'left' }}>{my.othersLine}</button>
            )}
          </div>
        )}
      </section>

      <FieldInspector sel={sel} data={data} items={items} goalsIndex={goalsIndex} me={me}
        filter={filter} onSelect={onSelect} onClear={onClearSel} pin={pin} go={go} />

      <section style={{ borderBottom: '1px solid var(--line,#3a3a3a)', padding: '16px 0' }}>
        <div style={{ display: 'flex', alignItems: 'baseline', gap: 12 }}>
          <span className="v2-label" style={{ letterSpacing: '.18em' }}>THIS WEEK</span>
          <button className="v2-bare v2-hoverink" onClick={() => go('work')}
            style={{ marginLeft: 'auto', color: 'var(--ink-mute,#666)', fontSize: 11 }}>all open work ({openCount}) →</button>
        </div>
        <div style={{ display: 'grid', gridTemplateColumns: weekCols, gap: '0 30px', marginTop: 8 }}>
          {week.rows.map((r, i) => <FieldRow key={i} r={r} />)}
        </div>
        {week.empty && <div style={{ color: 'var(--ink-mute,#666)', fontSize: 12, padding: '10px 0' }}>nothing due in the next 7 days</div>}
      </section>

      <section style={{ display: 'grid', gridTemplateColumns: 'repeat(3,minmax(0,1fr))', gap: 0 }}>
        <div style={{ padding: '14px 20px 16px 0', borderRight: '1px solid var(--line,#3a3a3a)' }}>
          <div className="v2-label" style={{ letterSpacing: '.18em' }}>CAPITAL</div>
          <div style={{ fontFamily: 'Alchimia,serif', fontSize: 44, lineHeight: 1.05, color: 'var(--ink,#e6e6e6)', marginTop: 8 }}>
            {typeof fin.capital === 'number' ? U.fmtMoney(fin.capital) : '—'}
          </div>
          <div style={{ fontSize: 12, color: 'var(--ink-faint,#888)', marginTop: 6 }}>
            {typeof fin.monthly_burn === 'number' ? 'burn ' + U.fmtMoney(fin.monthly_burn) + ' / mo' : ''}
          </div>
          <div style={{ fontSize: 12, color: 'var(--ink-faint,#888)' }}>
            {fin.runway_months ? 'runway ' + fin.runway_months + ' months' : ''}
          </div>
          <div style={{ fontSize: 11, color: 'var(--ink-mute,#666)', marginTop: 6 }}>{fin.as_of ? 'as of ' + fin.as_of : ''}</div>
        </div>
        <div style={{ padding: '14px 20px', borderRight: '1px solid var(--line,#3a3a3a)' }}>
          <div className="v2-label" style={{ letterSpacing: '.18em' }}>HIRING</div>
          <div style={{ display: 'flex', flexDirection: 'column', gap: 6, marginTop: 9 }}>
            {hiring.map((h, i) => (
              <div key={i} style={{ display: 'flex', gap: 8, alignItems: 'baseline', fontSize: 12 }}>
                <span style={{ color: 'var(--ink-mute,#555)', fontSize: 11 }}>{'p' + h.priority}</span>
                <span>{h.role}</span>
                <span style={{ marginLeft: 'auto', color: 'var(--ink-faint,#777)', fontSize: 11 }}>{h.stage}</span>
              </div>
            ))}
          </div>
          {hiring.length === 0 && <div style={{ color: 'var(--ink-mute,#666)', fontSize: 12, marginTop: 8 }}>no open roles</div>}
        </div>
        <div style={{ padding: '14px 0 16px 20px' }}>
          <div className="v2-label" style={{ letterSpacing: '.18em' }}>SCIENCE</div>
          <div style={{ display: 'flex', flexDirection: 'column', gap: 6, marginTop: 9 }}>
            {refs.map((r, i) => (
              <div key={i} style={{ fontSize: 12 }}>
                <a href={r.url} target="_blank" rel="noopener">{r.title}</a>
                {r.source ? <span style={{ color: 'var(--ink-mute,#666)', fontSize: 11 }}> · {r.source}</span> : null}
              </div>
            ))}
          </div>
          <button className="v2-bare v2-underlink v2-hoverink" onClick={() => go('archive', null, 'papers')}
            style={{ marginTop: 10, color: 'var(--ink-faint,#888)', fontSize: 11 }}>archive →</button>
        </div>
      </section>
    </div>
  );
}

Object.assign(window, { FieldView });
