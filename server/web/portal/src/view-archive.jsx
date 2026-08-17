/* Portal v2 — ARCHIVE: one record, one grammar. Six filter tabs over the
   unified archive rows (done work, decided decisions, artifacts from comment
   attachments, papers from references.md, heuristics). */

function ArchiveView({ data, items, goalsIndex, filter, tab, onTab, onSelect, pin, openFile, go }) {
  const D = window.PORTAL_DERIVE;
  const U = window.PORTAL_UTIL;
  const hooks = {
    select: onSelect,
    openGoal: id => { pin(id, true); go('field'); onSelect('goal', id); },
    openFile: openFile
  };
  const arch = D.archiveRows(items, goalsIndex, filter, data.team, data.heuristics,
    data.references, tab, U.todayISO(), hooks);
  const tabs = [
    { id: 'all', label: 'everything' }, { id: 'work', label: 'closed work' }, { id: 'decisions', label: 'decisions' },
    { id: 'artifacts', label: 'artifacts' }, { id: 'papers', label: 'papers' }, { id: 'heuristics', label: 'heuristics' }
  ];
  return (
    <div>
      <div style={{ display: 'flex', gap: 0, borderBottom: '1px solid var(--line,#3a3a3a)', padding: '10px 0 0', flexWrap: 'wrap' }}>
        {tabs.map(t => (
          <button key={t.id} className="v2-bare v2-hoverink" onClick={() => onTab(t.id)}
            style={{ borderBottom: '2px solid ' + (tab === t.id ? 'var(--accent,#0091ea)' : 'transparent'),
              color: tab === t.id ? 'var(--ink,#d4d4d4)' : 'var(--ink-faint,#888)', padding: '5px 14px 7px', fontSize: 12 }}>
            {t.label} <span style={{ color: 'var(--ink-mute,#666)', fontSize: 11 }}>{arch.counts[t.id]}</span>
          </button>
        ))}
      </div>
      <div style={{ fontSize: 11, color: 'var(--ink-mute,#666)', maxWidth: '74ch', padding: '12px 0 0' }}>
        Closed work, decided decisions, attached artifacts, canonical papers and heuristics — one record, newest first, scoped by the pinned rock.
      </div>
      <div style={{ display: 'flex', flexDirection: 'column', marginTop: 12 }}>
        {arch.rows.map((e, i) => (
          <button key={i} className="v2-bare v2-hoverbg" onClick={e.open}
            style={{ display: 'grid', gridTemplateColumns: '74px 86px minmax(0,1fr) 210px', gap: 14, alignItems: 'baseline',
              textAlign: 'left', borderBottom: '1px solid var(--line-soft,#2a2a2a)', padding: '8px 4px', color: 'var(--ink,#d4d4d4)' }}>
            <span style={{ fontSize: 11, color: 'var(--ink-mute,#666)' }}>{e.date}</span>
            <span style={{ fontSize: 10, letterSpacing: '.12em', color: e.kindColor }}>{e.kind}</span>
            <span style={{ minWidth: 0 }}>
              <span style={{ fontSize: 12.5 }}>{e.title}</span>
              {e.detail ? <span style={{ display: 'block', fontSize: 11, color: 'var(--ink-faint,#888)', marginTop: 2 }}>{e.detail}</span> : null}
            </span>
            <span style={{ fontSize: 11, color: 'var(--ink-mute,#666)' }}>{e.provenance}</span>
          </button>
        ))}
      </div>
      {arch.rows.length === 0 && (
        <div style={{ color: 'var(--ink-mute,#666)', fontSize: 12, padding: '12px 0' }}>nothing archived under this scope yet</div>
      )}
      {(tab === 'papers' || tab === 'artifacts') && (
        <div style={{ fontSize: 11, color: 'var(--warn,#a44)', marginTop: 16, maxWidth: '74ch' }}>
          flagged · papers and artifacts have no published source in contract v1 — they read from content/references.md and comment attachments until a data/library.json section exists.
        </div>
      )}
    </div>
  );
}

Object.assign(window, { ArchiveView });
