// The write half of the portal (ooda-portal plan, Stage C). Every one of these
// routes comes from the SHARED layer the AION portal uses — same assignee
// lock, same admin rules, one copy — except the bid, which is OODA's own.

async function postJSON(path, body) {
  const res = await fetch(path, {
    method: "POST", credentials: "same-origin",
    headers: { "Content-Type": "application/json", Accept: "application/json" },
    body: JSON.stringify(body || {}),
  });
  if (!res.ok) throw new Error((await res.text().catch(() => "")).trim() || ("HTTP " + res.status));
  return res.json().catch(() => ({}));
}

async function patchJSON(path, body) {
  const res = await fetch(path, {
    method: "PATCH", credentials: "same-origin",
    headers: { "Content-Type": "application/json", Accept: "application/json" },
    body: JSON.stringify(body || {}),
  });
  if (!res.ok) throw new Error((await res.text().catch(() => "")).trim() || ("HTTP " + res.status));
  return res.json().catch(() => ({}));
}

const teamAPI = {
  comment: (item, text) => postJSON("/api/team/comment", { item, text }),
  addItem: (kind, title, rock, due) => postJSON("/api/team/items", { kind, title, rock, due }),
  patch: (itemID, fields) => patchJSON("/api/team/item/" + itemID, fields),
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
