/* Portal v2 — GOALS: SET POINT (V/TO), the 1yr → rock → 30-day ladder, and
   the quarter timeline. Markup mirrors the prototype. */

function GoalsView({ data, items, goalsIndex, filter, pin, go }) {
  const U = window.PORTAL_UTIL;
  const D = window.PORTAL_DERIVE;
  const vto = data.vto || {};
  const focus = vto.core_focus || {};
  const threeYr = vto.three_year_picture || {};
  const quarter = vto.quarter || {};
  const openDecisions = items.filter(i => i.kind === 'decision' && D.isOpen(i)).length;
  const ladder = D.ladder(vto, goalsIndex, filter, pin);
  const q = D.quarterInfo(vto);
  const rows = D.timelineRows(vto, goalsIndex, filter, pin);

  return (
    <div>
      <section style={{ padding: '20px 0 22px', borderBottom: '1px solid var(--line,#3a3a3a)' }}>
        <div className="v2-label" style={{ letterSpacing: '.18em' }}>SET POINT</div>
        <div style={{ fontFamily: 'Alchimia,serif', fontSize: 52, lineHeight: 1.06, color: 'var(--ink,#e8e8e8)', maxWidth: '19ch', marginTop: 12 }}>
          {focus.purpose || ''}
        </div>
        <div style={{ display: 'grid', gridTemplateColumns: 'repeat(3,minmax(0,1fr))', gap: 22, marginTop: 24, maxWidth: 1040 }}>
          <div>
            <div className="v2-label">NICHE</div>
            <div style={{ fontSize: 13, marginTop: 5 }}>{focus.niche || ''}</div>
          </div>
          <div>
            <div className="v2-label">10-YEAR TARGET</div>
            <div style={{ fontSize: 13, marginTop: 5 }}>{vto.ten_year_target || ''}</div>
          </div>
          <div>
            <div className="v2-label">3-YEAR PICTURE{threeYr.date ? ' · ' + threeYr.date : ''}</div>
            <div style={{ fontSize: 13, marginTop: 5 }}>{(threeYr.measurables || [])[0] || ''}</div>
          </div>
        </div>
        <div style={{ display: 'flex', gap: 26, marginTop: 22, fontSize: 12, color: 'var(--ink-faint,#888)', flexWrap: 'wrap' }}>
          <div><span style={{ color: 'var(--ink-mute,#666)' }}>core value ·</span> {(vto.core_values || [])[0] || ''}</div>
          <div><span style={{ color: 'var(--ink-mute,#666)' }}>quarter ·</span> {quarter.start ? quarter.start + ' → ' + quarter.end : ''}</div>
          <button className="v2-bare v2-underlink v2-hoverink" onClick={() => go('work')}
            style={{ color: 'var(--ink-faint,#888)', fontSize: 12 }}>issues · {openDecisions} open decisions →</button>
        </div>
      </section>

      <section style={{ padding: '18px 0 4px' }}>
        <div className="v2-label" style={{ letterSpacing: '.18em' }}>1-YEAR GOALS · ROCKS · 30-DAY</div>
        <div style={{ display: 'flex', flexDirection: 'column', gap: 20, marginTop: 14 }}>
          {ladder.map((y, yi) => (
            <div key={yi} style={{ borderLeft: '1px solid var(--line,#3a3a3a)', paddingLeft: 14 }}>
              <button className="v2-bare v2-hoveraccent-t" onClick={y.pin}
                style={{ color: y.color, fontSize: 15, textAlign: 'left' }}>{y.title}</button>
              <div style={{ fontSize: 11, color: 'var(--ink-mute,#666)', marginTop: 2 }}>1yr · {y.measurable}</div>
              <div style={{ display: 'flex', flexDirection: 'column', gap: 12, marginTop: 12 }}>
                {y.rocks.map((r, ri) => (
                  <div key={ri} style={{ border: '1px solid var(--line,#3a3a3a)', background: 'var(--bg-1,#1e1e1e)', padding: '10px 12px' }}>
                    <div style={{ display: 'flex', gap: 10, alignItems: 'baseline' }}>
                      <button className="v2-bare v2-hoveraccent-t" onClick={r.pin}
                        style={{ color: r.color, fontSize: 13, textAlign: 'left' }}>{r.title}</button>
                      <span style={{ marginLeft: 'auto', fontSize: 11, color: 'var(--ink-faint,#888)' }}>{r.owner}</span>
                      <span style={{ fontSize: 11, color: 'var(--ink-mute,#666)' }}>{r.quarter}</span>
                    </div>
                    <div style={{ fontSize: 11, color: 'var(--ink-mute,#666)', marginTop: 3 }}>{r.serves}</div>
                    <div style={{ display: 'flex', flexDirection: 'column', gap: 3, marginTop: 8 }}>
                      {r.children.map((c, ci) => (
                        <button key={ci} className="v2-bare v2-hoverink" onClick={c.pin}
                          style={{ display: 'flex', gap: 9, alignItems: 'baseline', padding: '1px 0',
                            color: 'var(--ink-dim,#aaa)', fontSize: 12, textAlign: 'left' }}>
                          <span style={{ color: 'var(--ink-mute,#555)', fontSize: 11 }}>{c.tree}</span>
                          <span>{c.title}</span>
                          <span style={{ color: 'var(--ink-mute,#666)', fontSize: 11 }}>{c.owner}</span>
                        </button>
                      ))}
                    </div>
                  </div>
                ))}
              </div>
            </div>
          ))}
        </div>
      </section>

      <section id="sec-timeline" style={{ padding: '28px 0 4px' }}>
        <div style={{ display: 'flex', alignItems: 'baseline', gap: 12, borderBottom: '1px solid var(--line,#3a3a3a)', paddingBottom: 6 }}>
          <span className="v2-label" style={{ letterSpacing: '.18em' }}>TIMELINE · {q.label}</span>
          <span style={{ marginLeft: 'auto', fontSize: 11, color: 'var(--ink-mute,#666)' }}>{rows.length} live rocks</span>
        </div>
        <div style={{ display: 'grid', gridTemplateColumns: '250px minmax(0,1fr)', marginTop: 10 }}>
          <div style={{ fontSize: 10, color: 'var(--ink-mute,#666)', letterSpacing: '.14em', paddingBottom: 6 }}>ROCK</div>
          <div style={{ display: 'grid', gridTemplateColumns: 'repeat(3,minmax(0,1fr))', fontSize: 10,
            color: 'var(--ink-mute,#666)', letterSpacing: '.14em', paddingBottom: 6 }}>
            {q.months.map(m => (
              <div key={m} style={{ borderLeft: '1px solid var(--line-soft,#303030)', paddingLeft: 6 }}>{m}</div>
            ))}
          </div>
        </div>
        <div style={{ display: 'flex', flexDirection: 'column' }}>
          {rows.map((t, i) => (
            <button key={i} className="v2-bare v2-hoverbg" onClick={t.pin}
              style={{ display: 'grid', gridTemplateColumns: '250px minmax(0,1fr)', alignItems: 'center',
                borderTop: '1px solid var(--line-soft,#2a2a2a)', padding: '6px 0', textAlign: 'left' }}>
              <span style={{ fontSize: 12, color: t.color, paddingRight: 12 }}>{t.title}</span>
              <span style={{ position: 'relative', display: 'block', height: 16,
                background: 'linear-gradient(90deg,var(--line-soft,#2e2e2e) 1px,transparent 1px) 0 0/33.33% 100%' }}>
                <span style={{ position: 'absolute', top: 3, left: t.left, width: t.width, height: 10,
                  border: '1px solid ' + t.stroke, boxSizing: 'border-box' }}>
                  <span style={{ position: 'absolute', top: 0, left: 0, right: 0, bottom: 0,
                    background: t.fill, opacity: t.fillOpacity }} />
                </span>
              </span>
            </button>
          ))}
        </div>
      </section>
    </div>
  );
}

Object.assign(window, { GoalsView });
