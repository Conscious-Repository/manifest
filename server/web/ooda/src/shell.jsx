// The OODA shell: header + the four destinations. Deliberately quiet — the
// numbers carry the page, the chrome does not compete with them.

function Shell({ view, setView, me, sync, children }) {
  const tabs = [
    ["dashboard", "DASHBOARD"],
    ["portfolio", "PORTFOLIO"],
    ["work", "WORK"],
    ["chat", "CHAT"],
  ];
  return (
    <div className="ooda">
      <header className="ooda-head">
        <div className="ooda-brand">OODA</div>
        <nav className="ooda-tabs">
          {tabs.map(([key, label]) => (
            <button key={key}
              className={"ooda-tab" + (view === key ? " on" : "")}
              onClick={() => setView(key)}>{label}</button>
          ))}
        </nav>
        <div className="ooda-who">
          {me && me.email ? (
            <>
              <span className="ooda-who-name">{me.initials || me.name || me.email}</span>
              <form method="POST" action="/oauth2/logout" className="ooda-signout-form">
                <button className="ooda-signout" type="submit">sign out</button>
              </form>
            </>
          ) : null}
        </div>
      </header>
      {sync && sync.stale ? (
        <div className="ooda-stale">
          showing the last good read — the records did not recompose
          {sync.error ? " (" + sync.error + ")" : ""}
        </div>
      ) : null}
      <main className="ooda-main">{children}</main>
    </div>
  );
}

// Tile — a dashboard figure whose TITLE is a question and whose number is the
// answer. Clicking navigates to the rows behind it; a tile that leads nowhere
// is just a number, which is what we are trying not to build.
function Tile({ label, value, sub, onClick, tone }) {
  return (
    <button className={"ooda-tile" + (tone ? " " + tone : "") + (onClick ? " click" : "")}
      onClick={onClick} disabled={!onClick}>
      <div className="ooda-tile-label">{label}</div>
      <div className="ooda-tile-val">{value}</div>
      <div className="ooda-tile-sub">{sub || " "}</div>
    </button>
  );
}

function Section({ title, count, children }) {
  return (
    <section className="ooda-sec">
      <div className="ooda-sec-head">
        <span className="ooda-sec-title">{title}</span>
        {count != null ? <span className="ooda-sec-count">{count}</span> : null}
      </div>
      {children}
    </section>
  );
}

function Empty({ children }) { return <div className="ooda-empty">{children}</div>; }
