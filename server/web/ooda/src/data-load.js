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
  // Overlapping load()s (poll-triggered + manual) can resolve out of order —
  // only the NEWEST call's result may land. Each call takes a sequence number
  // and checks it before every state write.
  const seqRef = React.useRef(0);

  const load = React.useCallback(async () => {
    const seq = ++seqRef.current;
    try {
      // The revision comes FIRST: it is the baseline the payloads can only be
      // NEWER than. Fetched after them, a store change mid-load left the
      // stored rev ahead of the rendered data, and the poll compared equal
      // against a stale render forever.
      const base = await getJSON("/api/ooda/revision");
      const [dashboard, portfolio, work, me] = await Promise.all([
        getJSON("/api/ooda/dashboard"),
        getJSON("/api/ooda/portfolio"),
        getJSON("/api/ooda/work"),
        getJSON("/api/me").catch(() => ({})),
      ]);
      if (seq !== seqRef.current) return; // a newer load superseded this one
      setState({ loading: false, error: "", dashboard, portfolio, work, me, rev: base.effectiveRevision, sync: base });
      // the one window this ordering leaves open: a change DURING the payload
      // fetches. Check once more and reload on top of the just-applied data.
      const after = await getJSON("/api/ooda/revision");
      if (seq === seqRef.current && after.effectiveRevision && after.effectiveRevision !== base.effectiveRevision) load();
    } catch (e) {
      if (seq !== seqRef.current) return;
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
