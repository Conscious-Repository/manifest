// Fetch + revision polling for the OODA portal. Same discipline as the AION
// portal's: poll the cheap revision route while the tab is visible, refetch
// the expensive payloads only when the effective revision actually moved.

const OODA_POLL_MS = 20000;

async function getJSON(path) {
  const res = await fetch(path, { credentials: "same-origin", headers: { Accept: "application/json" } });
  if (res.status === 401) {
    // the session lapsed mid-session — send them back through the gate
    window.location.href = "/oauth2/login";
    throw new Error("signed out");
  }
  if (!res.ok) throw new Error(path + " → " + res.status);
  return res.json();
}

// useOodaData — one hook the shell owns: loads every surface's payload, then
// re-loads when the server's revision changes. Errors surface as a banner
// rather than an empty page, because empty reads as "nothing here".
function useOodaData() {
  const [state, setState] = React.useState({ loading: true, error: "", rev: "" });

  const load = React.useCallback(async () => {
    try {
      const [dashboard, portfolio, work, me] = await Promise.all([
        getJSON("/api/ooda/dashboard"),
        getJSON("/api/ooda/portfolio"),
        getJSON("/api/ooda/work"),
        getJSON("/api/me").catch(() => ({})),
      ]);
      const rev = await getJSON("/api/ooda/revision");
      setState({ loading: false, error: "", dashboard, portfolio, work, me, rev: rev.effectiveRevision, sync: rev });
    } catch (e) {
      setState((s) => ({ ...s, loading: false, error: String(e.message || e) }));
    }
  }, []);

  React.useEffect(() => { load(); }, [load]);

  React.useEffect(() => {
    let stop = false;
    const tick = async () => {
      if (stop || document.visibilityState !== "visible") return;
      try {
        const r = await getJSON("/api/ooda/revision");
        setState((s) => {
          if (s.rev && r.effectiveRevision && r.effectiveRevision !== s.rev) load();
          return s;
        });
      } catch (e) { /* a poll failure is not a page failure */ }
    };
    const h = setInterval(tick, OODA_POLL_MS);
    return () => { stop = true; clearInterval(h); };
  }, [load]);

  return [state, load];
}
