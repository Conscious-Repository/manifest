// The OODA portal app. Mounts last (see index.html).

// A STALE SHELL self-heals here.
//
// index.html is served no-cache but the scripts carry max-age, and the `?v=`
// token only busts the BROWSER cache — the server ignores the query string and
// returns current content for any version. So a browser holding an older
// index.html requests the NEW content of its OLD <script> list, and any file a
// deploy ADDED is simply never loaded. Its global is undefined, the first
// render throws, and because this is one React tree the whole portal goes
// white — which is exactly how the CHAT tab presented as a blank page after
// chat-actions.js was introduced.
//
// app.jsx is itself always fetched fresh (it is in every shell's list), so it
// is the right place to notice. One cache-busted reload, then stop and let the
// boundary explain rather than loop.
(function healStaleShell() {
  if (window.CHAT_ACTIONS) return;
  const u = new URL(window.location.href);
  if (u.searchParams.get("stale") === "1") return;
  u.searchParams.set("stale", "1");
  window.location.replace(u.toString());
})();

// hardReload re-fetches everything past any cache, keeping the tab you are on.
function hardReload() {
  const u = new URL(window.location.href);
  u.searchParams.set("r", String(Date.now()));
  window.location.replace(u.toString());
}

// ViewBoundary keeps one broken view from taking the portal with it. Without
// it a single exception unmounts the shell too — no tabs, no sign-out, no way
// to reach the surfaces that still work, and nothing on screen saying why.
class ViewBoundary extends React.Component {
  constructor(props) {
    super(props);
    this.state = { err: null };
  }
  static getDerivedStateFromError(err) { return { err }; }
  componentDidCatch(err, info) { console.error("OODA view failed:", err, info); }
  componentDidUpdate(prev) {
    // switching tabs clears the error, so a bad view never strands the others
    if (prev.viewKey !== this.props.viewKey && this.state.err) this.setState({ err: null });
  }
  render() {
    if (!this.state.err) return this.props.children;
    return (
      <div className="ooda-broken">
        <div className="ooda-broken-h">this view could not load</div>
        <div className="ooda-sub">{String(this.state.err.message || this.state.err)}</div>
        <div className="ooda-sub">
          Usually a half-updated page. The other tabs still work.
        </div>
        <button className="ooda-send" onClick={hardReload}>reload the page</button>
      </div>
    );
  }
}

function App() {
  const [view, setView] = React.useState(() => {
    const h = (window.location.hash || "").replace(/^#\/?/, "");
    return ["dashboard", "portfolio", "map", "work", "chat"].includes(h) ? h : "dashboard";
  });
  const [data] = useOodaData();

  React.useEffect(() => { window.location.hash = "#/" + view; }, [view]);

  let body;
  if (data.loading) body = <Empty>loading the portfolio…</Empty>;
  else if (data.error) body = <Empty>{"could not load: " + data.error}</Empty>;
  else if (view === "dashboard") body = <ViewDashboard data={data} go={setView} />;
  else if (view === "portfolio") body = <ViewPortfolio data={data} />;
  else if (view === "map") body = <ViewMap />;
  else if (view === "work") body = <ViewWork data={data} me={data.me} />;
  else body = <ViewChat data={data} />;

  return (
    <Shell view={view} setView={setView} me={data.me} sync={data.sync}>
      <ViewBoundary viewKey={view}>{body}</ViewBoundary>
    </Shell>
  );
}

ReactDOM.createRoot(document.getElementById("root")).render(<App />);
