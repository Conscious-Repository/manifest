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

const OODA_VIEWS = ["dashboard", "portfolio", "map", "work", "feed", "archive", "chat"];

// #/item/<id> is the shareable deep link to one work item. Item ids carry
// slashes (aion-bl/x), so everything past the prefix is the id.
function parseOodaHash() {
  const h = (window.location.hash || "").replace(/^#\/?/, "");
  const m = /^item\/(.+)$/.exec(h);
  if (m) {
    let id = m[1];
    try { id = decodeURIComponent(id); } catch (e) { /* keep it raw */ }
    return { view: "work", item: id };
  }
  return { view: OODA_VIEWS.includes(h) ? h : "dashboard", item: null };
}

function App() {
  const [route] = React.useState(parseOodaHash);   // read once, at mount
  const [view, setView] = React.useState(route.view);
  const [openItem, setOpenItem] = React.useState(route.item);
  const [data] = useOodaData();

  // One writer for the hash: an open item owns the URL, otherwise the tab
  // does. replaceState keeps expanding a row out of the back button.
  React.useEffect(() => {
    const h = openItem ? "#/item/" + openItem : "#/" + view;
    if (window.location.hash !== h) history.replaceState(null, "", h);
  }, [view, openItem]);

  // leaving work drops the open item, so its link cannot outlive the view
  const go = React.useCallback((v) => { setOpenItem(null); setView(v); }, []);

  let body;
  if (data.loading) body = <Empty>loading the portfolio…</Empty>;
  else if (data.error) body = <Empty>{"could not load: " + data.error}</Empty>;
  else if (view === "dashboard") body = <ViewDashboard data={data} me={data.me} go={go} />;
  else if (view === "portfolio") body = <ViewPortfolio data={data} />;
  else if (view === "map") body = <ViewMap />;
  else if (view === "work") body = <ViewWork data={data} me={data.me} openItem={openItem} onOpenItem={setOpenItem} />;
  else if (view === "feed") body = <ViewFeed />;
  else if (view === "archive") body = <ViewArchive />;
  else body = <ViewChat data={data} />;

  return (
    <Shell view={view} setView={go} me={data.me} sync={data.sync}>
      <ViewBoundary viewKey={view}>{body}</ViewBoundary>
    </Shell>
  );
}

ReactDOM.createRoot(document.getElementById("root")).render(<App />);
