// The OODA portal app. Mounts last (see index.html).

function App() {
  const [view, setView] = React.useState(() => {
    const h = (window.location.hash || "").replace(/^#\/?/, "");
    return ["dashboard", "portfolio", "work", "chat"].includes(h) ? h : "dashboard";
  });
  const [data] = useOodaData();

  React.useEffect(() => { window.location.hash = "#/" + view; }, [view]);

  let body;
  if (data.loading) body = <Empty>loading the portfolio…</Empty>;
  else if (data.error) body = <Empty>{"could not load: " + data.error}</Empty>;
  else if (view === "dashboard") body = <ViewDashboard data={data} go={setView} />;
  else if (view === "portfolio") body = <ViewPortfolio data={data} />;
  else if (view === "work") body = <ViewWork data={data} me={data.me} />;
  else body = <ViewChat data={data} />;

  return (
    <Shell view={view} setView={setView} me={data.me} sync={data.sync}>
      {body}
    </Shell>
  );
}

ReactDOM.createRoot(document.getElementById("root")).render(<App />);
