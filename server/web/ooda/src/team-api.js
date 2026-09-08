// The write half of the portal (ooda-portal plan, Stage C). Every one of these
// routes comes from the SHARED layer the AION portal uses — same assignee
// lock, same admin rules, one copy — except the bid, which is OODA's own.

async function postJSON(path, body) {
  return portalRequest(path, { method: "POST", headers: { "Content-Type": "application/json", Accept: "application/json" }, body: JSON.stringify(body || {}) });
}
async function patchJSON(path, body) {
  return portalRequest(path, { method: "PATCH", headers: { "Content-Type": "application/json", Accept: "application/json" }, body: JSON.stringify(body || {}) });
}
async function deleteJSON(path) {
  return portalRequest(path, { method: "DELETE", headers: { Accept: "application/json" } });
}

const teamAPI = {
  comment: (item, text) => postJSON("/api/team/comment", { item, text }),
  addItem: (kind, title, rock, due) => postJSON("/api/team/items", { kind, title, rock, due }),
  // encodeURIComponent is load-bearing: rock work items carry ids like
  // prop/748-n-euclid#shell/roof, and a raw `#` starts the URL FRAGMENT —
  // fetch drops it and the server sees a truncated id it cannot resolve.
  patch: (itemID, fields) => patchJSON("/api/team/item/" + encodeURIComponent(itemID), fields),
  // DELETE archives with a snapshot server-side — nothing is destroyed.
  del: (itemID) => deleteJSON("/api/team/item/" + encodeURIComponent(itemID)),
  propose: (target, kind, title, rock, due) =>
    postJSON("/api/team/proposals", { target, kind, title, rock, due }),
  decide: (id, approve) => postJSON("/api/team/proposals/decide", { id, approve }),
  bid: (b) => postJSON("/api/ooda/bid", b),
  state: () => getJSON("/api/team/state"),
};

// teamComments — the overlay's comments for one item id ("" → none yet).
function teamComments(state, itemID) {
  const c = (state && state.comments) || {};
  return c[itemID] || [];
}
