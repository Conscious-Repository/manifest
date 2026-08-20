// WORK — outstanding tasks and decisions grouped by the person who holds
// them. Partners and contractors first, then `— unassigned —` LAST and
// highlighted, because work nobody holds is the finding this tab exists for.

function ViewWork({ data, me }) {
  const groups = (data.work && data.work.groups) || [];
  const mine = ((me && me.initials) || "").toUpperCase();
  const [open, setOpen] = React.useState({});
  if (!groups.length) return <Empty>no open work</Empty>;
  return (
    <>
      {groups.map((g, i) => {
        const total = g.overdue.length + g.dueThisWeek.length + g.open.length +
          g.decisions.length + g.waiting.length;
        const key = g.owner || "_unassigned";
        const isOpen = open[key] !== false; // sections start open
        return (
          <section key={i} className={"ooda-sec" + (g.owner ? "" : " unassigned")}>
            <div className="ooda-sec-head click"
              onClick={() => setOpen({ ...open, [key]: !isOpen })} role="button">
              <span className="ooda-sec-title">
                {g.owner || "— UNASSIGNED —"}
              </span>
              <span className="ooda-sec-count">{total}</span>
            </div>
            {isOpen ? (
              <>
                <WorkLane label="OVERDUE" items={g.overdue} tone="over" mine={mine} owner={g.owner} />
                <WorkLane label="DUE THIS WEEK" items={g.dueThisWeek} mine={mine} owner={g.owner} />
                <WorkLane label="OPEN" items={g.open} mine={mine} owner={g.owner} />
                <WorkLane label="DECISIONS" items={g.decisions} mine={mine} owner={g.owner} />
                <WorkLane label="WAITING" items={g.waiting} mine={mine} owner={g.owner} />
              </>
            ) : null}
          </section>
        );
      })}
    </>
  );
}

function WorkLane({ label, items, tone, mine, owner }) {
  if (!items || !items.length) return null;
  return (
    <div className="ooda-lane">
      <div className="ooda-lane-label">{label}</div>
      {items.map((it) => (
        <WorkRow key={it.id} it={it} tone={tone} isMine={!!mine && mine === (owner || "").toUpperCase()} />
      ))}
    </div>
  );
}

// WorkRow honours the ASSIGNEE LOCK the shared layer enforces: only the person
// who holds an item may change its state — and there is no admin override lane
// (the AION decision of 2026-08-13, mirrored). Everyone else sees WHY the
// control is unavailable rather than a 403 on click, and can still comment.
function WorkRow({ it, tone, isMine }) {
  const [open, setOpen] = React.useState(false);
  const [state, setState] = React.useState("");
  const [err, setErr] = React.useState("");

  const done = async () => {
    setErr("");
    try {
      await teamAPI.patch(it.id, { status: "done", done_on: todayISO() });
      setState("done ✓ — it clears on the next refresh");
    } catch (e) { setErr(String(e.message || e)); }
  };

  return (
    <>
      <div className="ooda-row cols-work click" onClick={() => setOpen(!open)} role="button">
        <span>{it.title}</span>
        <span className="ooda-sub">{orDash(it.container || it.rock)}</span>
        <span className={"r ooda-sub" + (tone ? " " + tone : "")}>{orDash(it.due)}</span>
      </div>
      {open ? (
        <div className="ooda-work-detail">
          {it.kind === "decision" ? null : isMine ? (
            <button className="ooda-send" onClick={done} disabled={!!state}>
              {state || "mark done"}
            </button>
          ) : (
            <div className="ooda-sub">
              {(it.owner || "nobody") + " holds this — comment or propose instead"}
            </div>
          )}
          {err ? <div className="ooda-err">{err}</div> : null}
          <Thread itemID={it.id} title={it.title} />
        </div>
      ) : null}
    </>
  );
}
