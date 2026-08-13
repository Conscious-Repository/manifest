/* Gate — /dd-style client gate (accepted-cosmetic, don't "upgrade").
   Plaintext compare + sessionStorage flag under 'portal_auth'. */

function Gate({ onUnlock }) {
  const PASS = 'moiranova##';
  const inputRef = React.useRef(null);
  const [error, setError] = React.useState('');

  React.useEffect(() => {
    if (inputRef.current) inputRef.current.focus();
  }, []);

  const submit = (e) => {
    e.preventDefault();
    const el = inputRef.current;
    if (!el) return;
    if (el.value === PASS) {
      sessionStorage.setItem('portal_auth', 'true');
      onUnlock();
    } else {
      el.value = '';
      setError('incorrect');
      setTimeout(() => setError(''), 1500);
    }
  };

  return (
    <div className="gate">
      <div className="gate-box">
        <div className="gate-wordmark">
          <div className="gate-title">AION</div>
          <div className="gate-sub">portal</div>
        </div>
        <form className="gate-form" onSubmit={submit}>
          <label className="gate-label" htmlFor="gatepw">password</label>
          <div className="gate-input-row">
            <input
              id="gatepw"
              ref={inputRef}
              type="password"
              autoComplete="off"
              enterKeyHint="go"
            />
            <button type="submit" className="gate-submit" aria-label="unlock">[→]</button>
          </div>
          <div className="gate-error">{error}</div>
        </form>
      </div>
    </div>
  );
}

Object.assign(window, { Gate });
