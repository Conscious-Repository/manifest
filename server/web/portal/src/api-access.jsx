/* Self-service API tokens. Secrets are held only in component memory and shown
   once; closing the modal discards them. Credential management is cookie-only
   server-side, so an API token can never mint another token. */

function ApiAccessModal({ onClose }) {
  const [tokens, setTokens] = React.useState([]);
  const [label, setLabel] = React.useState('');
  const [secret, setSecret] = React.useState('');
  const [secretID, setSecretID] = React.useState('');
  const [note, setNote] = React.useState('loading…');
  const [busy, setBusy] = React.useState(false);

  const load = React.useCallback(() => {
    return window.TEAM_API.get('api/tokens').then(r => {
      if (r.ok) {
        setTokens((r.value && r.value.tokens) || []);
        setNote('');
      } else setNote(r.error || 'could not load tokens');
      return r;
    });
  }, []);

  React.useEffect(() => { load(); }, [load]);

  const generate = () => {
    setBusy(true); setNote(''); setSecret(''); setSecretID('');
    window.TEAM_API.post('api/tokens', { label: label.trim() || 'External tooling' }).then(r => {
      setBusy(false);
      if (!r.ok) { setNote(r.error || 'could not generate token'); return; }
      setSecret(r.value.token || '');
      setSecretID(r.value.id || '');
      setLabel('');
      load();
    });
  };

  const copy = () => {
    if (!secret || !navigator.clipboard) return;
    navigator.clipboard.writeText(secret).then(() => setNote('copied to clipboard'))
      .catch(() => setNote('copy failed — select the token and copy it manually'));
  };

  const revoke = tok => {
    if (tok.revoked || !window.confirm('Revoke “' + tok.label + '”? Tools using it will immediately lose access.')) return;
    setBusy(true); setNote('');
    window.TEAM_API.post('api/tokens/' + encodeURIComponent(tok.id), {}, 'DELETE').then(r => {
      setBusy(false);
      if (!r.ok) { setNote(r.error || 'could not revoke token'); return; }
      if (tok.id === secretID) { setSecret(''); setSecretID(''); }
      load();
    });
  };

  const shortDate = s => s ? String(s).replace('T', ' ').slice(0, 16) + 'Z' : 'never';

  return (
    <div onClick={onClose} className="v2-modal-scrim">
      <div role="dialog" aria-modal="true" aria-label="API access" onClick={e => e.stopPropagation()}
        className="v2-modal" style={{ width: 'min(640px,92vw)' }}>
        <div style={{ display: 'flex', alignItems: 'baseline', gap: 10, padding: '14px 16px 10px', borderBottom: '1px solid var(--line,#3a3a3a)' }}>
          <span className="v2-label" style={{ letterSpacing: '.18em' }}>API ACCESS</span>
          <button className="v2-bare v2-hoverink" onClick={onClose}
            style={{ marginLeft: 'auto', color: 'var(--ink-mute,#666)', fontSize: 11 }}>esc ✕</button>
        </div>

        <div style={{ padding: 16 }}>
          <div style={{ fontSize: 12, color: 'var(--ink-dim,#aaa)', lineHeight: 1.65 }}>
            Tokens act exactly as your signed-in account. Existing assignee and admin locks still apply.
          </div>

          <div style={{ display: 'flex', gap: 8, marginTop: 12, flexWrap: 'wrap', alignItems: 'center' }}>
            <a className="v2-btn v2-hoveraccent" target="_blank" rel="noreferrer"
              href="https://github.com/Conscious-Repository/manifest/blob/main/docs/team-api.md"
              style={{ color: 'var(--accent,#0091ea)', padding: '4px 9px', textDecoration: 'none' }}>API reference ↗</a>
            <a className="v2-btn v2-hoveraccent" target="_blank" rel="noreferrer"
              href="https://github.com/Conscious-Repository/manifest/blob/main/integrations/portal-mcp/README.md"
              style={{ color: 'var(--accent,#0091ea)', padding: '4px 9px', textDecoration: 'none' }}>MCP setup ↗</a>
            <span style={{ fontSize: 11, color: 'var(--ink-mute,#666)' }}>
              Generate a token, copy it once, then follow either guide.
            </span>
          </div>

          <div style={{ display: 'flex', gap: 8, marginTop: 14, flexWrap: 'wrap' }}>
            <input className="v2-input" value={label} onChange={e => setLabel(e.target.value)} maxLength={80}
              placeholder="label, e.g. Claude Code" style={{ flex: '1 1 260px', padding: '5px 7px' }} />
            <button className="v2-btn v2-hoveraccent" disabled={busy} onClick={generate}
              style={{ color: 'var(--accent,#0091ea)', padding: '5px 10px' }}>{busy ? 'working…' : 'generate token'}</button>
          </div>

          {secret && (
            <div style={{ marginTop: 14, border: '1px solid var(--accent,#0091ea)', padding: 12, background: 'var(--bg-2,#242424)' }}>
              <div className="v2-label" style={{ color: 'var(--accent-bright,#5ec8f5)' }}>STORE THIS NOW · IT WILL NOT BE SHOWN AGAIN</div>
              <code style={{ display: 'block', marginTop: 8, fontSize: 12, color: 'var(--ink,#d4d4d4)', overflowWrap: 'anywhere', userSelect: 'all' }}>{secret}</code>
              <button className="v2-btn v2-hoverline" onClick={copy}
                style={{ marginTop: 9, color: 'var(--ink-dim,#aaa)', padding: '3px 9px' }}>copy</button>
            </div>
          )}

          <div className="v2-label" style={{ marginTop: 18 }}>YOUR TOKENS</div>
          <div style={{ display: 'flex', flexDirection: 'column', marginTop: 6, borderTop: '1px solid var(--line-soft,#2a2a2a)' }}>
            {!tokens.length && !note && <div style={{ fontSize: 11, color: 'var(--ink-mute,#666)', padding: '10px 0' }}>none yet</div>}
            {tokens.map(tok => (
              <div key={tok.id} style={{ display: 'grid', gridTemplateColumns: 'minmax(0,1fr) auto', gap: 12,
                padding: '9px 0', borderBottom: '1px solid var(--line-soft,#2a2a2a)', opacity: tok.revoked ? .5 : 1 }}>
                <div style={{ minWidth: 0 }}>
                  <div style={{ fontSize: 12, color: 'var(--ink,#d4d4d4)', overflow: 'hidden', textOverflow: 'ellipsis' }}>{tok.label}</div>
                  <div style={{ fontSize: 10.5, color: 'var(--ink-mute,#666)', marginTop: 2 }}>
                    {tok.id} · created {shortDate(tok.created)} · used {shortDate(tok.last_used)}{tok.revoked ? ' · REVOKED' : ''}
                  </div>
                </div>
                <button className="v2-bare v2-hoverwarn" disabled={busy || tok.revoked} onClick={() => revoke(tok)}
                  style={{ color: tok.revoked ? 'var(--ink-mute,#555)' : 'var(--ink-faint,#888)', fontSize: 11 }}>
                  {tok.revoked ? 'revoked' : 'revoke'}
                </button>
              </div>
            ))}
          </div>
          {note && <div style={{ fontSize: 11, color: 'var(--ink-faint,#888)', marginTop: 10 }}>{note}</div>}
          <div style={{ fontSize: 10.5, color: 'var(--ink-mute,#555)', marginTop: 14 }}>
            Base URL <code>https://portal.aion.bio</code> · send <code>Authorization: Bearer aiontok_…</code>
          </div>
        </div>
      </div>
    </div>
  );
}

Object.assign(window, { ApiAccessModal });
