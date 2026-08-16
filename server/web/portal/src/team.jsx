/* Team layer (portal move Phase 2–3) — Google sign-in chip, per-item drawer
   (comments for any signed-in member; status/fields for the assignee only),
   team/ adds for oneself, proposals for others (owner- or target-approved).
   All state lives server-side (/api/team/*); viewing and writing both require
   an @aion.bio session (the whole portal is gated — server/portal.go). */

const TEAM_API = {
  post(path, body, method) {
    return fetch(path, {
      method: method || 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify(body || {})
    }).then(r => r.ok
      ? r.json().then(v => ({ ok: true, value: v }))
      : r.text().then(t => ({ ok: false, error: t.trim() || ('HTTP ' + r.status) })));
  },
  // get — NOT via PORTAL_UTIL.fetchJSON (its blind '?t=' cache-bust corrupts
  // query strings like ?id=…).
  get(path) {
    return fetch(path, { cache: 'no-store' }).then(r => r.ok
      ? r.json().then(v => ({ ok: true, value: v }))
      : r.text().then(t => ({ ok: false, error: t.trim() || ('HTTP ' + r.status) })));
  }
};

/* The taggable-agent roster (kairos plan Phase C) — fetched once per session
   on first drawer open; a 404 from an older server degrades to "no agents". */
let TEAM_AGENTS = null;
function loadAgents() {
  if (TEAM_AGENTS) return Promise.resolve(TEAM_AGENTS);
  return TEAM_API.get('api/team/agents').then(r => {
    TEAM_AGENTS = (r.ok && r.value && r.value.agents) ? r.value.agents : [];
    return TEAM_AGENTS;
  });
}

/* Each agent expands into its bare token plus one intent-tagged variant per
   persona (agent:kairos::brief) — intent rides the mention, never ownership. */
function agentOptions(agents, q) {
  const out = [];
  (agents || []).forEach(a => {
    out.push({ token: a.id, label: a.name });
    (a.personas || []).forEach(pi => out.push({ token: a.id + '::' + pi, label: a.name + '::' + pi }));
  });
  const ql = (q || '').toLowerCase();
  return out.filter(o => !ql ||
    o.label.toLowerCase().indexOf(ql) >= 0 || o.token.toLowerCase().indexOf(ql) >= 0);
}

/* Composer with @-mention typeahead: '@' + typing filters the agent list; a
   pick inserts the display label into the text and records the STRUCTURAL
   token on the POST (mentions ride the comment, manifest's dialog hook does
   the rest). Degrades to the plain composer when no agents are configured. */
function MentionComposer({ item, onPosted, onErr }) {
  const [text, setText] = React.useState('');
  const [mentions, setMentions] = React.useState([]);
  const [agents, setAgents] = React.useState(null);
  const [sug, setSug] = React.useState(null);
  React.useEffect(() => {
    let on = true;
    loadAgents().then(a => { if (on) setAgents(a); });
    return () => { on = false; };
  }, []);
  const labelFor = token => {
    const hit = agentOptions(agents, '').filter(o => o.token === token)[0];
    return hit ? hit.label : token;
  };
  const onInput = v => {
    setText(v);
    const m = /(^|\s)@([A-Za-z:.-]*)$/.exec(v);
    setSug(m && agents && agents.length ? agentOptions(agents, m[2]) : null);
  };
  const pick = o => {
    setText(t => t.replace(/@[A-Za-z:.-]*$/, '@' + o.label + ' '));
    setMentions(ms => ms.indexOf(o.token) < 0 ? ms.concat(o.token) : ms);
    setSug(null);
  };
  const post = () => {
    if (!text.trim()) return;
    // prune tokens whose visible @label the author deleted before sending
    const live = mentions.filter(tok => text.indexOf('@' + labelFor(tok)) >= 0);
    TEAM_API.post('api/team/comment', { item: item, text: text, mentions: live })
      .then(r => {
        if (r.ok) { setText(''); setMentions([]); setSug(null); onPosted(); }
        else onErr(r.error);
      });
  };
  const onKey = e => {
    if (sug && sug.length) {
      if (e.key === 'Enter' || e.key === 'Tab') { e.preventDefault(); pick(sug[0]); return; }
      if (e.key === 'Escape') { e.stopPropagation(); setSug(null); return; } // don't close the drawer
    }
    if (e.key === 'Enter') post();
  };
  return (
    <div className="team-row team-composer">
      {sug && sug.length > 0 && (
        <div className="mention-pop">
          {sug.map(o => (
            <div key={o.token} className="mention-opt" onMouseDown={e => { e.preventDefault(); pick(o); }}>
              @{o.label}
            </div>
          ))}
        </div>
      )}
      <input
        className="team-input"
        placeholder={agents && agents.length ? 'add a comment… @ tags the agent' : 'add a comment…'}
        value={text}
        onChange={e => onInput(e.target.value)}
        onKeyDown={onKey}
      />
      <button className="team-btn" onClick={post}>post</button>
    </div>
  );
}

/* The action loop on the drawer (kairos plan Phase D): delegation state,
   agent assign, the materialized PLAN, and fire — team-wide (any member).
   Renders nothing when the server predates the panel API. */
function TeamAgentPanel({ item, me, onChange }) {
  const U = window.PORTAL_UTIL;
  const [panel, setPanel] = React.useState(null);
  const [agents, setAgents] = React.useState([]);
  const [sel, setSel] = React.useState('');
  const [err, setErr] = React.useState('');
  const load = React.useCallback(() =>
    TEAM_API.get('api/team/panel?id=' + encodeURIComponent(item.id))
      .then(r => setPanel(r.ok ? r.value : null)), [item.id]);
  React.useEffect(() => {
    let on = true;
    load();
    loadAgents().then(a => { if (on) setAgents(a); });
    return () => { on = false; };
  }, [load]);
  const deleg = (panel && panel.delegation) || null;
  const state = deleg ? deleg.state : '';
  React.useEffect(() => { // poll only while the loop is moving
    if (!/queued|running/.test(state)) return;
    const t = setInterval(() => { load(); onChange(); }, 20000);
    return () => clearInterval(t);
  }, [state, load]);
  if (!panel) return null;
  const assignee = panel.assignee || '';
  const isAgent = assignee.indexOf('agent:') === 0;
  const canAct = me && !me.anon && me.canFire;
  const assign = () =>
    TEAM_API.post('api/team/assign', { item: item.id, owner: sel })
      .then(r => {
        if (r.ok) { setErr(''); setSel(''); setPanel(r.value); onChange(); }
        else setErr(r.error);
      });
  const fire = () =>
    TEAM_API.post('api/team/fire', { item: item.id })
      .then(r => {
        if (r.ok) { setErr(''); load(); onChange(); } else setErr(r.error);
      });
  if (!agents.length && !isAgent && !panel.plan) return null; // nothing agentic to show
  return (
    <div className="team-controls team-agent">
      <div className="team-label">
        agent
        {state ? <span className={'team-chip deleg-chip deleg-' + state}>{state}</span> : null}
      </div>
      <div className="team-row">
        <span className="team-assignee">
          {isAgent ? '✦ ' + assignee.slice(6) : (assignee || 'unassigned')}
        </span>
        {canAct && agents.length > 0 && (
          <select value={sel} onChange={e => setSel(e.target.value)}>
            <option value="">assign the agent…</option>
            {agents.map(a => <option key={a.id} value={a.id}>✦ {a.name}</option>)}
          </select>
        )}
        {canAct && sel && <button className="team-btn" onClick={assign}>assign</button>}
        {canAct && isAgent && panel.plan && !/queued|running/.test(state) && (
          <button className="team-btn team-fire" onClick={fire}>fire → execute the plan</button>
        )}
      </div>
      {panel.plan ? (
        <div>
          <div className="team-label">plan</div>
          <div className="team-plan">{U.renderInline(panel.plan)}</div>
        </div>
      ) : null}
      <div className="team-err">{err}</div>
    </div>
  );
}

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
  // logout returns 204 (no body) — a plain fetch clears the cookie, then we send
  // the browser to the portal root, where the gate now serves the branded
  // landing page (logo + Sign in). We do NOT bounce to Google: that would let a
  // still-live Google session silently re-authenticate and the user could never
  // stay signed out. (TEAM_API.post would JSON-parse the empty 204 and reject
  // before anything ran.)
  const signOut = () =>
    fetch('oauth2/logout', { method: 'POST' }).finally(() => { location.href = '/'; });
  return (
    <span className="auth-chip">
      <span className="auth-who">{me.email}</span>
      {' · '}
      <span className="auth-link" onClick={signOut}>sign out</span>
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
  const [status, setStatus] = React.useState(item.status || 'open');
  const [due, setDue] = React.useState(item.due || '');
  const [err, setErr] = React.useState('');

  const fail = r => { if (!r.ok) setErr(r.error); return r.ok; };

  // A comment is deletable by its author or the portal owner (admin).
  const canDelete = c => signedIn &&
    (me.admin || (c.author || '').toLowerCase() === (me.email || '').toLowerCase());
  const removeComment = c => {
    if (!window.confirm('Delete this comment?')) return;
    TEAM_API.post('api/team/comment', { item: item.id, id: c.id }, 'DELETE')
      .then(r => { if (fail(r)) onChange(); });
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

          {signedIn && <TeamAgentPanel item={item} me={me} onChange={onChange} />}

          <div className="team-comments">
            <div className="team-label">comments</div>
            {comments.length === 0 && <div className="no-data">none yet</div>}
            {comments.map(c => (
              <div className="team-comment" key={c.id}>
                <div className="team-comment-meta">
                  {c.author_name || c.author} · {String(c.at).slice(0, 10)}
                  {canDelete(c) && (
                    <span className="auth-link team-comment-del" onClick={() => removeComment(c)}> · delete</span>
                  )}
                </div>
                <div className="team-comment-text">{c.text}</div>
              </div>
            ))}
            {signedIn ? (
              <MentionComposer item={item.id} onPosted={onChange} onErr={setErr} />
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

Object.assign(window, { AuthChip, TeamItemDrawer, TeamAdd, TeamProposals, TEAM_API, MentionComposer, TeamAgentPanel });
