/* Team layer (portal move Phase 2–3) — Google sign-in chip, per-item drawer
   (comments for any signed-in member; status/fields for the assignee only),
   team/ adds for oneself, proposals for others (owner- or target-approved).
   All state lives server-side (/api/team/*); anonymous readers see everything,
   writes require an @aion.bio session. */

const TEAM_API = {
  post(path, body, method) {
    return fetch(path, {
      method: method || 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify(body || {})
    }).then(r => r.ok
      ? r.json().then(v => ({ ok: true, value: v }))
      : r.text().then(t => ({ ok: false, error: t.trim() || ('HTTP ' + r.status) })));
  }
};

/* Sign-in chip in the masthead: quiet text, no loud color. */
function AuthChip({ me, onChange }) {
  if (!me) return null;
  if (me.anon) {
    if (me.authConfigured === false) return null; // no OAuth client on this host
    return (
      <span className="auth-chip">
        <a className="auth-link" href="oauth2/login">sign in</a>
      </span>
    );
  }
  const signOut = () =>
    TEAM_API.post('oauth2/logout').then(() => onChange && onChange());
  return (
    <span className="auth-chip">
      <span className="auth-who">{me.email}</span>
      <span className="auth-link" onClick={signOut}> · sign out</span>
    </span>
  );
}

/* Item drawer — opened by clicking a TODO/decision row. */
function TeamItemDrawer({ item, me, team, onClose, onChange }) {
  const U = window.PORTAL_UTIL;
  const comments = (team && team.comments && team.comments[item.id]) || [];
  const signedIn = me && !me.anon;
  const mine = signedIn && item.owner &&
    (item.owner.toLowerCase() === (me.initials || '').toLowerCase() ||
     item.owner.toLowerCase() === me.email.split('@')[0].toLowerCase());
  const [text, setText] = React.useState('');
  const [status, setStatus] = React.useState(item.status || 'open');
  const [due, setDue] = React.useState(item.due || '');
  const [err, setErr] = React.useState('');

  const fail = r => { if (!r.ok) setErr(r.error); return r.ok; };

  const comment = () => {
    if (!text.trim()) return;
    TEAM_API.post('api/team/comment', { item: item.id, text: text })
      .then(r => { if (fail(r)) { setText(''); onChange(); } });
  };
  const save = fields =>
    TEAM_API.post('api/team/item/' + item.id, fields, 'PATCH')
      .then(r => { if (fail(r)) onChange(); });

  return (
    <div>
      <div className="drawer-scrim" onClick={onClose} />
      <div className="drawer team-drawer">
        <span className="drawer-close" onClick={onClose}>✕</span>
        <div className="drawer-body">
          <div className="drawer-kicker">
            <span className="mark">{U.statusMark(item.status)}</span> {item.kind}
            {item.team ? <span className="team-chip">team/</span> : null}
          </div>
          <div className="drawer-title">{item.title}</div>
          <div className="drawer-subtitle">
            {U.personName(item.owner)}{item.due ? ' · due ' + item.due : ''}
            {item.done_on ? ' · done ' + item.done_on : ''}
            {item.team && item.added_by ? ' · added by ' + item.added_by.split('@')[0] : ''}
          </div>

          {mine && (
            <div className="team-controls">
              <div className="team-label">status · yours to change</div>
              <div className="team-row">
                <select value={status} onChange={e => setStatus(e.target.value)}>
                  <option value="open">○ open</option>
                  <option value="in_progress">◐ in progress</option>
                  <option value="done">● done</option>
                </select>
                <input
                  type="date" value={due}
                  onChange={e => setDue(e.target.value)}
                  title="due"
                />
                <button className="team-btn" onClick={() => save({ status: status, due: due })}>save</button>
                {item.status !== 'done' && (
                  <button className="team-btn" onClick={() => save({ status: 'done' })}>mark done</button>
                )}
              </div>
            </div>
          )}

          <div className="team-comments">
            <div className="team-label">comments</div>
            {comments.length === 0 && <div className="no-data">none yet</div>}
            {comments.map(c => (
              <div className="team-comment" key={c.id}>
                <div className="team-comment-meta">
                  {c.author_name || c.author} · {String(c.at).slice(0, 10)}
                </div>
                <div className="team-comment-text">{c.text}</div>
              </div>
            ))}
            {signedIn ? (
              <div className="team-row">
                <input
                  className="team-input" placeholder="add a comment…" value={text}
                  onChange={e => setText(e.target.value)}
                  onKeyDown={e => { if (e.key === 'Enter') comment(); }}
                />
                <button className="team-btn" onClick={comment}>post</button>
              </div>
            ) : (
              <div className="team-hint"><a className="auth-link" href="oauth2/login">sign in</a> to comment</div>
            )}
          </div>
          <div className="team-err">{err}</div>
        </div>
      </div>
    </div>
  );
}

/* Add form — a team/ item for yourself (direct), or a proposal for another
   member (pending until the owner or the target approves). The target list
   comes from the roster; addresses derive first-name@aion.bio, the same
   convention the server maps back to initials. */
function teamRoster(me) {
  const mine = (me.initials || '').toLowerCase();
  return Object.entries(window.PORTAL_PEOPLE || {})
    .map(([initials, p]) => ({
      initials,
      name: p.name || initials,
      email: (p.name || initials).split(' ')[0].toLowerCase() + '@aion.bio'
    }))
    .filter(p => p.initials.toLowerCase() !== mine)
    .sort((a, b) => (a.name < b.name ? -1 : 1));
}

function TeamAdd({ me, onClose, onChange }) {
  const [forWhom, setForWhom] = React.useState('me'); // 'me' | roster email | 'other'
  const [target, setTarget] = React.useState('');
  const [kind, setKind] = React.useState('task');
  const [title, setTitle] = React.useState('');
  const [due, setDue] = React.useState('');
  const [err, setErr] = React.useState('');
  const roster = teamRoster(me);

  const submit = () => {
    if (!title.trim()) return;
    const done = r => {
      if (!r.ok) { setErr(r.error); return; }
      onChange();
      onClose();
    };
    if (forWhom === 'me') {
      TEAM_API.post('api/team/items', { kind: kind, title: title, due: due }).then(done);
    } else {
      const tgt = forWhom === 'other' ? target : forWhom;
      TEAM_API.post('api/team/proposals', { kind: kind, title: title, due: due, target: tgt }).then(done);
    }
  };

  return (
    <div className="team-add">
      <div className="team-row">
        <select value={forWhom} onChange={e => setForWhom(e.target.value)} title="who it's for">
          <option value="me">for me ({me.initials || me.email.split('@')[0]})</option>
          {roster.map(p => (
            <option key={p.initials} value={p.email}>propose for {p.name}</option>
          ))}
          <option value="other">propose for…</option>
        </select>
        {forWhom === 'other' && (
          <input
            className="team-input" placeholder="name@aion.bio" value={target}
            onChange={e => setTarget(e.target.value)}
          />
        )}
        <select value={kind} onChange={e => setKind(e.target.value)} title="kind">
          <option value="task">task</option>
          <option value="decision">decision</option>
        </select>
        <input type="date" value={due} onChange={e => setDue(e.target.value)} title="due" />
      </div>
      <div className="team-row">
        <input
          className="team-input team-input-wide"
          placeholder={kind === 'decision' ? 'decision…' : 'task…'} value={title}
          onChange={e => setTitle(e.target.value)}
          onKeyDown={e => { if (e.key === 'Enter') submit(); }}
        />
        <button className="team-btn" onClick={submit}>
          {forWhom === 'me' ? 'add' : 'propose'}
        </button>
        <button className="team-btn" onClick={onClose}>cancel</button>
      </div>
      <div className="team-err">{err}</div>
    </div>
  );
}

/* Proposals — pending ones visible to everyone (deciding needs the target or
   the portal owner), decided ones behind a collapsed log. */
function TeamProposals({ me, team, onChange }) {
  const U = window.PORTAL_UTIL;
  const all = (team && team.proposals) || [];
  const pending = all.filter(p => p.status === 'pending');
  const decided = all.filter(p => p.status !== 'pending')
    .slice().sort((a, b) => (String(b.decided_at) < String(a.decided_at) ? -1 : 1))
    .slice(0, 10);
  const [showDecided, setShowDecided] = React.useState(false);
  const [err, setErr] = React.useState('');
  if (!pending.length && !decided.length) return null;

  const forMe = p => me && !me.anon &&
    me.email.toLowerCase() === (p.target || '').toLowerCase();
  const canDecide = p => forMe(p) || (me && !me.anon && me.admin);
  const decide = (p, approve) =>
    TEAM_API.post('api/team/proposals/decide', { id: p.id, approve: approve })
      .then(r => { if (r.ok) { setErr(''); onChange(); } else setErr(r.error); });
  const who = p => p.target_owner ? U.personName(p.target_owner) : p.target;
  const proposer = p => (p.proposed_name || p.proposed_by || '').split(' ')[0] ||
    (p.proposed_by || '').split('@')[0];

  return (
    <div className="team-proposals">
      <div className="ov-head">
        <span className="ov-head-title">PROPOSED</span>
        {pending.length > 0 && <span className="prop-count">{pending.length} pending</span>}
      </div>
      {pending.length === 0 && <div className="no-data">none pending</div>}
      {pending.map(p => (
        <div className={'dec-row' + (forMe(p) ? ' prop-for-me' : '')} key={p.id}>
          <span className="dec-title">
            {p.title}
            {p.kind === 'decision' ? <span className="team-chip">decision</span> : null}
            {forMe(p) ? <span className="team-chip prop-you-chip">for you</span> : null}
          </span>
          <span className="dec-meta">
            for <span className="dec-who">{who(p)}</span> · by {proposer(p)}
            {p.due ? ' · due ' + p.due : ''}
            {canDecide(p) && (
              <span>
                {' '}· <span className="auth-link" onClick={() => decide(p, true)}>approve</span>
                {' '}/ <span className="auth-link" onClick={() => decide(p, false)}>reject</span>
              </span>
            )}
          </span>
        </div>
      ))}
      {decided.length > 0 && (
        <div className="prop-decided">
          <span className="prop-decided-toggle" onClick={() => setShowDecided(s => !s)}>
            decided {showDecided ? '▾' : '▸'}
          </span>
          {showDecided && decided.map(p => (
            <div className="dec-row prop-decided-row" key={p.id}>
              <span className="prop-verdict">{p.status === 'approved' ? '✓' : '✕'}</span>
              <span className="dec-title">{p.title}</span>
              <span className="dec-meta">
                for {who(p)} · {p.status}
                {p.decided_by ? ' by ' + p.decided_by.split('@')[0] : ''}
                {p.decided_at ? ' · ' + String(p.decided_at).slice(0, 10) : ''}
              </span>
            </div>
          ))}
        </div>
      )}
      <div className="team-err">{err}</div>
    </div>
  );
}

Object.assign(window, { AuthChip, TeamItemDrawer, TeamAdd, TeamProposals, TEAM_API });
