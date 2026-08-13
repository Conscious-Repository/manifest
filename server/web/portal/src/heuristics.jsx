/* Heuristics — the curated synthesis list, rendered in upstream order.
   Statement + quiet ×N reinforcement count (omitted when N ≤ 1).
   No sources shown — provenance lives in manifest. */

function Heuristics({ heuristics, errors }) {
  return (
    <div className="heur-block">
      <div className="heur-head">heuristics</div>
      {!heuristics ? (
        <div className="no-data">no data yet</div>
      ) : heuristics.heuristics.length === 0 ? (
        <div className="no-data">none yet</div>
      ) : (
        <div className="heur-list">
          {heuristics.heuristics.map(h => {
            const n = (h.reinforcements || []).length;
            return (
              <div className="heur-row" key={h.id}>
                <span className="heur-statement">{h.statement}</span>
                {n > 1 && <span className="heur-count">×{n}</span>}
              </div>
            );
          })}
        </div>
      )}
      <DriftNote errors={errors} keys={['heuristics']} />
    </div>
  );
}

Object.assign(window, { Heuristics });
