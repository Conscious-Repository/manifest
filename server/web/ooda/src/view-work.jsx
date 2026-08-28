// WORK — outstanding tasks and decisions grouped by the person who holds
// them. Partners and contractors first, then `— unassigned —` LAST and
// highlighted, because work nobody holds is the finding this tab exists for.

function ViewWork({ data, me, openItem, onOpenItem }) {
  const onOpen = onOpenItem || function () {};
  const groups = (data.work && data.work.groups) || [];
  const mine = ((me && me.initials) || "").toUpperCase();
  const [open, setOpen] = React.useState({});
  const [onlyMine, setOnlyMine] = React.useState(false);
  if (!groups.length) return <Empty>no open work</Empty>;

  const count = (g) => g.overdue.length + g.dueThisWeek.length + g.open.length +
    g.decisions.length + g.waiting.length;

  // The reads are flat — the server cannot know who is asking, so "my work
  // first" is decided here. Everything stays visible; only the order moves.
  // `mine` is "" for a viewer with no initials on file. Comparing an empty
  // owner to it matched the UNASSIGNED group and floated it to the TOP —
  // the exact opposite of the rule that group exists to serve.
  const isMine = (g) => !!mine && g.owner.toUpperCase() === mine;
  const ordered = groups.slice().sort((a, b) => {
    const am = isMine(a), bm = isMine(b);
    return am === bm ? 0 : am ? -1 : 1;
  });
  const shown = onlyMine ? ordered.filter(isMine) : ordered;

  const sum = (k) => groups.reduce((n, g) => n + g[k].length, 0);
  const myTotal = groups.filter((g) => g.owner.toUpperCase() === mine).reduce((n, g) => n + count(g), 0);

  return (
    <>
      {/* the shape of the whole board before anyone scrolls: what is late,
          what lands this week, and what nobody holds */}
      <div className="ooda-worksum">
        <span className={sum("overdue") ? "over" : ""}>
          <em>OVERDUE</em><b>{sum("overdue") || DASH}</b>
        </span>
        <span><em>DUE THIS WEEK</em><b>{sum("dueThisWeek") || DASH}</b></span>
        <span><em>OPEN</em><b>{sum("open") || DASH}</b></span>
        <span><em>DECISIONS</em><b>{sum("decisions") || DASH}</b></span>
        <span className={groups.some((g) => !g.owner) ? "over" : ""}>
          <em>UNASSIGNED</em>
          <b>{(groups.find((g) => !g.owner) ? count(groups.find((g) => !g.owner)) : 0) || DASH}</b>
        </span>
      </div>
      {mine ? (
        <div className="ooda-toolbar">
          <button className={"ooda-chip" + (onlyMine ? " on" : "")}
            onClick={() => setOnlyMine(!onlyMine)}>
            {"MINE · " + (myTotal || 0)}
          </button>
          {onlyMine ? <span className="ooda-sub">showing only work you hold</span> : null}
        </div>
      ) : null}
      {!shown.length ? <Empty>nothing is assigned to you right now</Empty> : null}
      {shown.map((g, i) => {
        const total = count(g);
        const key = g.owner || "_unassigned";
        const isOpen = open[key] !== false; // sections start open
        const isMe = isMine(g);
        return (
          <section key={i} className={"ooda-sec" + (g.owner ? "" : " unassigned")}>
            <div className="ooda-sec-head click"
              onClick={() => setOpen({ ...open, [key]: !isOpen })} role="button">
              <span className="ooda-sec-title">
                {g.owner || "— UNASSIGNED —"}
                {g.name ? <em className="ooda-sec-name">{g.name}</em> : null}
                {isMe ? <em className="ooda-sec-you">you</em> : null}
              </span>
              <span className="ooda-sec-count">{total}</span>
              {g.overdue.length
                ? <span className="ooda-sec-count over">{g.overdue.length + " overdue"}</span>
                : null}
            </div>
            {isOpen ? (
              <>
                <WorkLane label="OVERDUE" items={g.overdue} tone="over" mine={mine} owner={g.owner} openItem={openItem} onOpen={onOpen} />
                <WorkLane label="DUE THIS WEEK" items={g.dueThisWeek} mine={mine} owner={g.owner} openItem={openItem} onOpen={onOpen} />
                <WorkLane label="OPEN" items={g.open} mine={mine} owner={g.owner} openItem={openItem} onOpen={onOpen} />
                <WorkLane label="DECISIONS" items={g.decisions} mine={mine} owner={g.owner} openItem={openItem} onOpen={onOpen} />
                <WorkLane label="WAITING" items={g.waiting} mine={mine} owner={g.owner} openItem={openItem} onOpen={onOpen} />
              </>
            ) : null}
          </section>
        );
      })}
    </>
  );
}

function WorkLane({ label, items, tone, mine, owner, openItem, onOpen }) {
  if (!items || !items.length) return null;
  return (
    <div className="ooda-lane">
      <div className="ooda-lane-label">{label}</div>
      {items.map((it) => (
        <WorkRow key={it.id} it={it} tone={tone} isMine={!!mine && mine === (owner || "").toUpperCase()}
          linked={!!openItem && openItem === it.id} onOpen={onOpen} />
      ))}
    </div>
  );
}

// The reassignment roster: partners + contractors from /api/ooda/people,
// fetched once and shared by every row's editor. The work payload only lists
// CURRENT holders, which would make handing work to an idle person impossible.
let peopleCache = null;
function loadPeople() {
  if (!peopleCache) {
    peopleCache = getJSON("/api/ooda/people")
      .then((r) => (r && r.people) || [])
      .catch(() => { peopleCache = null; return []; });
  }
  return peopleCache;
}

// AssignRow is the ONE control on work nobody holds: give it an owner. Open
// to every member (owner decision 2026-08-25) — the server's carve-out admits
// exactly {owner} on an unowned item and nothing else. The old copy here sent
// people to "propose instead", a control this portal never shipped.
function AssignRow({ it, setState, setErr }) {
  const [people, setPeople] = React.useState(null);
  const [busy, setBusy] = React.useState(false);
  React.useEffect(() => {
    let on = true;
    loadPeople().then((p) => { if (on) setPeople(p); });
    return () => { on = false; };
  }, []);
  const assign = async (initials) => {
    if (!initials) return;
    setErr(""); setBusy(true);
    try {
      await teamAPI.patch(it.id, { owner: initials });
      setState("assigned to " + initials + " ✓ — regroups on the next refresh");
    } catch (e) {
      setErr(String(e.message || e));
    }
    setBusy(false);
  };
  return (
    <div className="ooda-assign-row">
      <span className="ooda-sub">nobody holds this — </span>
      <select className="ooda-in" disabled={busy || !people} defaultValue=""
        onChange={(e) => assign(e.target.value)}>
        <option value="" disabled>{people ? "assign to…" : "loading roster…"}</option>
        {(people || []).map((p) => (
          <option key={p.initials} value={p.initials}>
            {p.initials + (p.name ? " — " + p.name : "")}
          </option>
        ))}
      </select>
      <span className="ooda-sub"> or comment below</span>
    </div>
  );
}

// WorkRow honours the ASSIGNEE LOCK the shared layer enforces: only the person
// who holds an item may change its state — and there is no admin override lane
// (the AION decision of 2026-08-13, mirrored — and reaffirmed 2026-08-24,
// when an override was considered and declined). Everyone else sees WHY the
// control is unavailable rather than a 403 on click, and can still comment.
//
// The holder gets the AION item editor's moves, quiet: close (done, or
// decided-with-outcome for a decision), retitle, hand on, delete-to-archive.
function WorkRow({ it, tone, isMine, linked, onOpen }) {
  // `linked` means the URL points here (#/item/<id>): the row starts open and
  // scrolls itself into view, so a shared link lands on the work, not the top
  // of the board. Opening any row hands the URL to it.
  const [open, setOpen] = React.useState(!!linked);
  const [state, setState] = React.useState("");
  const [err, setErr] = React.useState("");
  const rowRef = React.useRef(null);

  React.useEffect(() => {
    if (linked && rowRef.current) rowRef.current.scrollIntoView({ block: "center", behavior: "smooth" });
  }, [linked]);

  const toggle = () => {
    const next = !open;
    setOpen(next);
    if (onOpen) onOpen(next ? it.id : null);
  };

  const isDecision = it.kind === "decision";
  const [editing, setEditing] = React.useState(false);
  const [busy, setBusy] = React.useState(false);
  const [title, setTitle] = React.useState(it.title || "");
  // The work read carries no status — everything here is open work. An
  // "unchanged" sentinel keeps a plain retitle from silently resetting an
  // in_progress item to open; the sentinel itself is never sent.
  const [status, setStatus] = React.useState("");
  const [owner, setOwner] = React.useState(it.owner || "");
  const [decided, setDecided] = React.useState(todayISO());
  const [outcome, setOutcome] = React.useState("");
  const [people, setPeople] = React.useState(null);

  React.useEffect(() => {
    if (!editing || people) return;
    let on = true;
    loadPeople().then((p) => { if (on) setPeople(p); });
    return () => { on = false; };
  }, [editing, people]);

  const done = async () => {
    setErr(""); setBusy(true);
    try {
      await teamAPI.patch(it.id, { status: "done", done_on: todayISO() });
      setState("done ✓ — it clears on the next refresh");
    } catch (e) { setErr(String(e.message || e)); }
    setBusy(false);
  };

  const save = async () => {
    setErr(""); setBusy(true);
    try {
      const f = {};
      const t = title.trim();
      if (t && t !== it.title) f.title = t;
      if (status) f.status = status;
      if (owner && owner !== it.owner) f.owner = owner;
      if (isDecision && status === "decided") {
        // a decided decision carries WHEN and WHAT — without them it leaves
        // the open list and lands in no archive (the view-item.jsx lesson)
        f.decided = decided || todayISO();
        if (outcome.trim()) f.outcome = outcome.trim();
      }
      if (!isDecision && status === "done") f.done_on = todayISO();
      if (Object.keys(f).length) {
        await teamAPI.patch(it.id, f);
        setState(status === "decided" || status === "done"
          ? "recorded ✓ — it clears on the next refresh"
          : "saved ✓ — it updates on the next refresh");
      }
      setEditing(false);
    } catch (e) { setErr(String(e.message || e)); }
    setBusy(false);
  };

  const del = async () => {
    if (!window.confirm('Delete "' + (it.title || it.id) + '"? It leaves every list; the archive keeps a copy.')) return;
    setErr(""); setBusy(true);
    try {
      await teamAPI.del(it.id);
      setEditing(false);
      setState("deleted — the archive keeps a snapshot; it clears on the next refresh");
    } catch (e) { setErr(String(e.message || e)); }
    setBusy(false);
  };

  // the roster always offers the current holder, even before the fetch lands
  const roster = people || [];
  const holderListed = roster.some((p) => (p.initials || "").toUpperCase() === (it.owner || "").toUpperCase());
  const ownerOptions = (holderListed || !it.owner ? [] : [{ initials: it.owner, name: "" }]).concat(roster);

  return (
    <>
      <div ref={rowRef} className={"ooda-row cols-work click" + (linked ? " sel" : "")}
        onClick={toggle} role="button">
        <span className="ooda-stack">
          <b>{it.title}</b>
          {it.kind === "decision" ? <em>decision</em> : null}
        </span>
        {/* where the work lives: the property, then the rock inside it. Showing
            only one of the two left partners guessing which project it was. */}
        <span className="ooda-sub">
          {[it.container, it.rock].filter(Boolean).join(" · ") || DASH}
        </span>
        <span className={"r ooda-sub" + (tone ? " " + tone : "")}>{orDash(it.due)}</span>
      </div>
      {open ? (
        <div className="ooda-work-detail">
          {!isMine && !it.owner ? (
            <AssignRow it={it} setState={setState} setErr={setErr} />
          ) : !isMine ? (
            <div className="ooda-sub">
              {it.owner + " holds this — comment below, or ask them"}
            </div>
          ) : state ? (
            <div className="ooda-sub">{state}</div>
          ) : editing ? (
            <>
              <div className="ooda-form">
                <label className="wide"><em>TITLE</em>
                  <input className="ooda-in" value={title}
                    onChange={(e) => setTitle(e.target.value)} /></label>
                <label><em>STATUS</em>
                  <select className="ooda-in" value={status}
                    onChange={(e) => setStatus(e.target.value)}>
                    <option value="">— unchanged —</option>
                    <option value="open">open</option>
                    <option value="in_progress">in progress</option>
                    {isDecision
                      ? <option value="decided">decided</option>
                      : <option value="done">done</option>}
                  </select></label>
                <label><em>OWNER</em>
                  <select className="ooda-in" value={owner}
                    onChange={(e) => setOwner(e.target.value)}>
                    {ownerOptions.map((p) => (
                      <option key={p.initials} value={p.initials}>
                        {p.initials + (p.name ? " — " + p.name : "")}
                      </option>
                    ))}
                  </select></label>
                {/* the two fields that make a decision closable — shown only
                    once one is being closed; an outcome box on an open
                    decision reads as a prompt to invent one */}
                {isDecision && status === "decided" ? (
                  <>
                    <label><em>DECIDED</em>
                      <input className="ooda-in" type="date" value={decided}
                        onChange={(e) => setDecided(e.target.value)} /></label>
                    <label className="wide"><em>OUTCOME</em>
                      <input className="ooda-in" value={outcome}
                        placeholder="what was decided, and why"
                        onChange={(e) => setOutcome(e.target.value)} /></label>
                  </>
                ) : null}
              </div>
              <div className="ooda-form-acts">
                <button className="ooda-ghost" onClick={del} disabled={busy}>delete</button>
                <button className="ooda-ghost" onClick={() => setEditing(false)} disabled={busy}>cancel</button>
                <button className="ooda-send" onClick={save} disabled={busy || !title.trim()}>
                  {busy ? "…" : isDecision && status === "decided" ? "record decision" : "save"}
                </button>
              </div>
            </>
          ) : (
            <div className="ooda-bid-open">
              {isDecision ? (
                // a decision closes via DECIDED, never done — opening the
                // editor with the date + outcome fields already revealed
                <button className="ooda-send" disabled={busy}
                  onClick={() => { setStatus("decided"); setEditing(true); }}>
                  record decision
                </button>
              ) : (
                <button className="ooda-send" onClick={done} disabled={busy}>mark done</button>
              )}
              <button className="ooda-ghost" onClick={() => setEditing(true)}>edit</button>
            </div>
          )}
          {err ? <div className="ooda-err">{err}</div> : null}
          <Thread itemID={it.id} title={it.title} />
        </div>
      ) : null}
    </>
  );
}
