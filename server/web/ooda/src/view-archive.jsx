// ARCHIVE — the log of what the team has captured and decided (ooda-portal
// email plan, phase 4). Everything here is CONFIRMED, so the read is flat:
// shared artifacts (email notes + chat uploads), and the extraction outcomes
// the approvals lane produced. Facets, newest first — the AION portal's
// archive shape, sized to OODA's data.

function useOodaArchive() {
  const [state, setState] = React.useState({ loading: true, error: "" });
  React.useEffect(() => {
    (async () => {
      try {
        const archive = await getJSON("/api/ooda/archive");
        setState({ loading: false, error: "", archive });
      } catch (e) {
        setState({ loading: false, error: String(e.message || e) });
      }
    })();
  }, []);
  return state;
}

function ViewArchive() {
  const state = useOodaArchive();
  const [facet, setFacet] = React.useState("all");
  if (state.loading) return <Empty>loading the archive…</Empty>;
  if (state.error) return <Empty>{"could not load: " + state.error}</Empty>;
  const a = state.archive || {};
  const artifacts = a.artifacts || [];
  const notes = a.emailNotes || [];
  const outcomes = a.outcomes || [];

  // one unified, dated row shape — the facet chips filter it
  const noteHashes = new Set(notes.map((n) => n.artifactHash));
  const rows = [];
  for (const e of artifacts) {
    rows.push({
      kind: noteHashes.has(e.hash) ? "email" : "file",
      date: (e.at || "").slice(0, 10),
      title: e.name,
      detail: [e.by, e.thread].filter(Boolean).join(" · "),
      href: "/api/chat/attach/" + e.hash,
    });
  }
  for (const o of outcomes) {
    rows.push({
      kind: "decision",
      date: "", // the proposal file carries no decided-at; the action line carries the content
      title: o.action,
      detail: o.status + " · " + (o.type === "re-contract" ? "money" : "task/decision") + " · " + o.agent,
      href: o.source ? "/api/chat/attach/" + o.source : "",
    });
  }
  rows.sort((x, y) => (y.date || "").localeCompare(x.date || ""));
  const shown = facet === "all" ? rows : rows.filter((r) => r.kind === facet);

  const facets = [
    ["all", "ALL", rows.length],
    ["email", "EMAIL NOTES", rows.filter((r) => r.kind === "email").length],
    ["file", "FILES", rows.filter((r) => r.kind === "file").length],
    ["decision", "OUTCOMES", rows.filter((r) => r.kind === "decision").length],
  ];

  return (
    <>
      <div className="ooda-facets">
        {facets.map(([key, label, n]) => (
          <button key={key} className={"ooda-facet" + (facet === key ? " on" : "")}
            onClick={() => setFacet(key)}>
            {label} <b>{n || DASH}</b>
          </button>
        ))}
      </div>
      {shown.length ? (
        <div className="ooda-archive">
          {shown.map((r, i) => (
            <div className="ooda-archive-row" key={i}>
              <span className="ooda-archive-date">{r.date || DASH}</span>
              <span className={"ooda-chip " + (r.kind === "decision" ? "money" : "")}>{r.kind}</span>
              {r.href ? (
                <a className="ooda-archive-title" href={r.href} target="_blank" rel="noopener">{r.title}</a>
              ) : (
                <span className="ooda-archive-title">{r.title}</span>
              )}
              <span className="ooda-sub">{r.detail}</span>
            </div>
          ))}
        </div>
      ) : (
        <Empty>nothing archived yet — confirmed email notes and decided proposals land here</Empty>
      )}
    </>
  );
}
