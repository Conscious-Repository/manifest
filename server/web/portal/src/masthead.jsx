/* Masthead — backlink, wordmark, `last published` (meta.json), tab nav,
   rock-filter chip, page footer. Terse labels; no legend, no roster. */

function Masthead({ meta, me, onAuthChange }) {
  const published = meta && meta.published_at ? meta.published_at.slice(0, 10) : null;
  const by = meta && meta.published_by ? meta.published_by : null;
  return (
    <div className="masthead">
      <div>
        <div className="masthead-wordmark">
          <img className="masthead-logo" src="./assets/logo-wordmark-dark.png" alt="AION" />
        </div>
      </div>
      <div className="masthead-meta">
        {published
          ? <div>last published {published}{by ? ' · ' + by : ''} · confidential</div>
          : <div>last published — no data yet</div>}
        <AuthChip me={me} onChange={onAuthChange} />
      </div>
    </div>
  );
}

function TabNav({ tab, onTab }) {
  return (
    <div className="tabbar">
      <div className="tabbar-inner">
        {['overview', 'goals', 'task'].map(id => (
          <span
            key={id}
            className={'ptab' + (tab === id ? ' is-on' : '')}
            onClick={() => onTab(id)}
          >{id}</span>
        ))}
      </div>
    </div>
  );
}

function FilterChip({ filterGoal, onClear }) {
  if (!filterGoal) return null;
  const horizon = filterGoal.horizon === '1yr' ? '1 year'
    : filterGoal.horizon === 'rock' ? '90 day'
    : '30 day';
  return (
    <div className="filter-chip">
      <span className="filter-chip-label">{horizon} · {filterGoal.title}</span>
      <span className="filter-chip-clear" onClick={onClear}>clear ✕</span>
    </div>
  );
}

function Footer({ meta }) {
  const published = meta && meta.published_at ? meta.published_at.slice(0, 10) : '—';
  return (
    <div className="pfooter">
      <span>AION Biosciences · Confidential</span>
      <span>published from manifest · {published}</span>
    </div>
  );
}

// Quiet footnote when a file loaded but failed its shape check
function DriftNote({ errors, keys }) {
  const drifted = (keys || []).filter(k => errors && errors[k] === 'drift');
  if (!drifted.length) return null;
  return <div className="drift-note">schema drift · {drifted.join(' · ')}</div>;
}

Object.assign(window, { Masthead, TabNav, FilterChip, Footer, DriftNote });
