// WORK — outstanding tasks and decisions grouped by the person who holds
// them. Partners and contractors first, then `— unassigned —` LAST and
// highlighted, because work nobody holds is the finding this tab exists for.

function ViewWork({ data }) {
  const groups = (data.work && data.work.groups) || [];
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
                <WorkLane label="OVERDUE" items={g.overdue} tone="over" />
                <WorkLane label="DUE THIS WEEK" items={g.dueThisWeek} />
                <WorkLane label="OPEN" items={g.open} />
                <WorkLane label="DECISIONS" items={g.decisions} />
                <WorkLane label="WAITING" items={g.waiting} />
              </>
            ) : null}
          </section>
        );
      })}
    </>
  );
}

function WorkLane({ label, items, tone }) {
  if (!items || !items.length) return null;
  return (
    <div className="ooda-lane">
      <div className="ooda-lane-label">{label}</div>
      {items.map((it) => (
        <div key={it.id} className="ooda-row cols-work">
          <span>{it.title}</span>
          <span className="ooda-sub">{orDash(it.container || it.rock)}</span>
          <span className={"r ooda-sub" + (tone ? " " + tone : "")}>{orDash(it.due)}</span>
        </div>
      ))}
    </div>
  );
}
