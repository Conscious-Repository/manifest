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
   @aion.bio address (pending until the owner or the target approves). */
function TeamAdd({ me, onClose, onChange }) {
  const [forWhom, setForWhom] = React.useState('me');
  const [target, setTarget] = React.useState('');
  const [title, setTitle] = React.useState('');
  const [due, setDue] = React.useState('');
  const [err, setErr] = React.useState('');

  const submit = () => {
    if (!title.trim()) return;
    const done = r => {
      if (!r.ok) { setErr(r.error); return; }
      onChange();
      onClose();
    };
    if (forWhom === 'me') {
      TEAM_API.post('api/team/items', { kind: 'task', title: title, due: due }).then(done);
    } else {
      TEAM_API.post('api/team/proposals', { kind: 'task', title: title, due: due, target: target }).then(done);
    }
  };

  return (
    <div className="team-add">
      <div className="team-row">
        <select value={forWhom} onChange={e => setForWhom(e.target.value)}>
          <option value="me">for me ({me.initials || me.email.split('@')[0]})</option>
          <option value="other">propose for…</option>
        </select>
        {forWhom === 'other' && (
          <input
            className="team-input" placeholder="name@aion.bio" value={target}
            onChange={e => setTarget(e.target.value)}
          />
        )}
        <input type="date" value={due} onChange={e => setDue(e.target.value)} title="due" />
      </div>
      <div className="team-row">
        <input
          className="team-input team-input-wide" placeholder="task…" value={title}
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

/* Pending proposals — visible to everyone; deciding needs to be the target or
   the portal owner. */
function TeamProposals({ me, team, onChange }) {
  const pending = ((team && team.proposals) || []).filter(p => p.status === 'pending');
  if (!pending.length) return null;
  const canDecide = p => me && !me.anon &&
    (me.admin || me.email.toLowerCase() === (p.target || '').toLowerCase());
  const decide = (p, approve) =>
    TEAM_API.post('api/team/proposals/decide', { id: p.id, approve: approve }).then(r => { if (r.ok) onChange(); });
  return (
    <div className="team-proposals">
      <div className="ov-head"><span className="ov-head-title">PROPOSED</span></div>
      {pending.map(p => (
        <div className="dec-row" key={p.id}>
          <span className="dec-title">{p.title}</span>
          <span className="dec-meta">
            for <span className="dec-who">{p.target}</span> · by {p.proposed_name || p.proposed_by}
            {canDecide(p) && (
              <span>
                {' '}· <span className="auth-link" onClick={() => decide(p, true)}>approve</span>
                {' '}/ <span className="auth-link" onClick={() => decide(p, false)}>reject</span>
              </span>
            )}
          </span>
        </div>
      ))}
    </div>
  );
}

Object.assign(window, { AuthChip, TeamItemDrawer, TeamAdd, TeamProposals, TEAM_API });
