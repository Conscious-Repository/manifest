# AION team API

The AION team API exposes the same shared team state and collaboration actions as [portal.aion.bio](https://portal.aion.bio). API calls use the caller's real portal identity, so comments and activity are attributed to that teammate and every existing assignee/admin lock still applies.

Base URL: `https://portal.aion.bio`

## Authentication

1. Sign in to the portal with your `@aion.bio` Google account.
2. Choose **api access** in the lower-left account area.
3. Label and generate a token.
4. Store the displayed secret immediately. It is shown once.

Send the token on every API request:

```http
Authorization: Bearer aiontok_...
```

Tokens are per-user, stored hashed, and revocable. Revoking a token takes effect on its next request. A token cannot generate or revoke tokens; those actions require an interactive portal cookie session.

The API does not currently send CORS headers. Command-line tools, server-side code, and local MCP servers work directly. A browser-based custom UI should call the API through its own same-origin backend/proxy rather than expose the token in frontend code.

## Conventions

- Request and response bodies are JSON unless an endpoint returns a file.
- Errors are plain text with meaningful HTTP statuses: `400` invalid input, `401` missing/invalid/revoked credentials, `403` an assignee/admin lock, `404` unknown records, and `409` an already-running agent.
- Item IDs commonly contain a slash, such as `team/calibrate-the-rig`. Query/body IDs should be sent unchanged. The update route is `PATCH /api/team/item/team/calibrate-the-rig`.
- Dates are `YYYY-MM-DD`; timestamps are RFC 3339.
- The team store is the portal's shared derived state. **Since 2026-08-24 it is a staging area, not a parallel truth**: a reconciler writes portal-created items, field edits on published items, and approved proposals into `system/aion/backlog.md`, then clears what it handed over. Comments and the activity trail stay in the store. See ARCHITECTURE.md §12 (2026-08-24).

## Endpoint summary

| Method | Path | Purpose | Authorization beyond membership |
| --- | --- | --- | --- |
| `GET` | `/api/me` | Current identity and portal capabilities | None |
| `GET` | `/api/team/snapshot` | Complete published team view plus live overlay | None |
| `GET` | `/api/team/state` | All shared comments, overrides, team items, and proposals | None |
| `POST` | `/api/team/comment` | Comment and optionally mention agents | Item must exist |
| `DELETE` | `/api/team/comment` | Delete a comment | Comment author or portal admin |
| `PATCH` | `/api/team/item/{id...}` | Change team-editable item fields | Item assignee only |
| `DELETE` | `/api/team/item/{id...}` | Archive an item (leaves every list; the archive keeps a copy) | Item assignee only |
| `POST` | `/api/team/items` | Add an item owned by the caller | None |
| `POST` | `/api/team/proposals` | Propose an item for another teammate | Target must be `@aion.bio` |
| `POST` | `/api/team/proposals/decide` | Approve/reject a proposal | Target or portal admin |
| `GET` | `/api/team/agents` | List team-visible agents/personas | None |
| `GET` | `/api/team/panel?id=...` | Read an item's plan and delegation state | Item must exist |
| `POST` | `/api/team/assign` | Assign/clear a team agent | None beyond membership |
| `POST` | `/api/team/fire` | Execute an agent-held plan | None beyond membership |
| `GET` | `/api/team/activity?item=...` | Read one item's activity trail | None |
| `POST` | `/api/team/plan` | Write a plan section | Item assignee or portal admin |
| `GET` | `/api/team/file/{hash}` | Read a comment attachment | File must exist |
| `GET` | `/api/tokens` | List your tokens | Portal cookie only |
| `POST` | `/api/tokens` | Generate a token | Portal cookie only |
| `DELETE` | `/api/tokens/{id}` | Revoke your token | Portal cookie only |

## Identity

### `GET /api/me`

```json
{
  "email": "hannah@aion.bio",
  "name": "Hannah Zmuda",
  "initials": "HZ",
  "person": "Hannah Zmuda",
  "admin": false,
  "canFire": true
}
```

## Complete team snapshot

### `GET /api/team/snapshot`

This is the recommended starting point for scripts and custom backends. It
returns the same inputs the portal combines in the browser:

```json
{
  "backlog": {"items": []},
  "goals": {"goals": []},
  "people": {"people": []},
  "meta": {"published_at": "2026-08-17T16:00:00Z"},
  "team": {
    "comments": {},
    "overrides": {},
    "items": [],
    "proposals": []
  }
}
```

`backlog.items` contains the published tasks and decisions. `goals.goals`
contains the hierarchy, including rocks (`horizon: "rock"`). `team` is the
live multi-writer overlay described below. Consumers should ignore additive
fields they do not recognize.

## Team state overlay

### `GET /api/team/state`

Returns the complete shared overlay:

```json
{
  "comments": {
    "team/calibrate-the-rig": [
      {
        "id": "c-1786960000000000000",
        "item": "team/calibrate-the-rig",
        "author": "hannah@aion.bio",
        "author_name": "Hannah Zmuda",
        "text": "Ready for review",
        "mentions": ["agent:kairos"],
        "files": [],
        "at": "2026-08-17T16:00:00Z"
      }
    ]
  },
  "overrides": {
    "team/calibrate-the-rig": {
      "fields": {"status": "in_progress", "due": "2026-08-22"},
      "by": "hannah@aion.bio",
      "at": "2026-08-17T16:01:00Z"
    }
  },
  "items": [],
  "proposals": []
}
```

`items` entries have this shape:

```json
{
  "id": "team/calibrate-the-rig",
  "kind": "task",
  "title": "Calibrate the rig",
  "owner": "HZ",
  "captured": "2026-08-17",
  "rock": "operations-company-health",
  "due": "2026-08-22",
  "status": "open",
  "team": true,
  "added_by": "hannah@aion.bio"
}
```

## Comments

### `POST /api/team/comment`

Any member may comment on any known item.

```json
{
  "item": "team/calibrate-the-rig",
  "text": "Kairos, draft the verification steps",
  "mentions": ["agent:kairos"]
}
```

The response is the stored comment. Agent mentions are structural tokens such as `agent:kairos` or `agent:kairos::brief`; putting `@kairos` only in prose does not substitute for `mentions` in an API call.

### `DELETE /api/team/comment`

```json
{"item": "team/calibrate-the-rig", "id": "c-1786960000000000000"}
```

Response:

```json
{"deleted": "c-1786960000000000000"}
```

## Items and proposals

### `POST /api/team/items`

Creates a direct team item owned by the caller. `kind` is `task` or `decision` and defaults to `task`.

```json
{
  "kind": "task",
  "title": "Calibrate the rig",
  "rock": "operations-company-health",
  "due": "2026-08-22"
}
```

The response is the created team item.

### `PATCH /api/team/item/{id...}`

Only the item's assignee may update it — **including the portal admin, who has no override** (decided 2026-08-13, reaffirmed 2026-08-24).

The closed field set is `status`, `done_on`, `due`, `needed_by`, `outcome`, `title`, `owner`, and `decided`. Status is `open`, `in_progress`, `done`, or `decided`.

Setting `owner` reassigns the item, after which the caller can no longer edit it — the lock follows the assignment.

```json
{"status": "done", "outcome": "Calibration passed"}
```

Closing a **decision** uses `decided`, not `done`: the archive selects on `status: decided` with a `decided` date, so a decision closed as `done` leaves the open list and reaches no archive.

```json
{"status": "decided", "decided": "2026-08-24", "outcome": "Ultrasound, MRI as fallback"}
```

### `DELETE /api/team/item/{id...}`

Only the item's assignee. The item is archived, not erased: it leaves every live view, the team store keeps an attributed snapshot, and the reconciler removes the backlog line.

Response:

```json
{
  "item": "team/calibrate-the-rig",
  "override": {
    "fields": {"status": "done", "done_on": "2026-08-17", "outcome": "Calibration passed"},
    "by": "hannah@aion.bio",
    "at": "2026-08-17T16:10:00Z"
  }
}
```

### `POST /api/team/proposals`

```json
{
  "target": "rj@aion.bio",
  "kind": "task",
  "title": "Review the protocol",
  "rock": "operations-company-health",
  "due": "2026-08-24"
}
```

The response is a proposal with `status: "pending"`.

### `POST /api/team/proposals/decide`

```json
{"id": "prop/review-the-protocol", "approve": true}
```

The proposal target or portal admin may decide it. Approval creates a `team/...` item and returns the proposal with its `item_id`.

## Agents and plans

### `GET /api/team/agents`

```json
{
  "agents": [
    {"id": "agent:kairos", "name": "Kairos", "harness": "kairos", "personas": ["brief", "review"]}
  ]
}
```

### `GET /api/team/panel?id=team/calibrate-the-rig`

Returns the item's plan record and current delegation state. The payload is additive and may grow as the agent loop gains fields; clients should ignore unknown keys.

### `POST /api/team/assign`

Any member may assign the team agent. Use an empty owner to clear it.

```json
{"item": "team/calibrate-the-rig", "owner": "agent:kairos"}
```

The response is the updated panel payload.

### `POST /api/team/fire`

```json
{"item": "team/calibrate-the-rig"}
```

Response:

```json
{"ok": true, "queued": true}
```

If the agent is already active, the endpoint returns `409`.

### `GET /api/team/activity?item=team%2Fcalibrate-the-rig`

Returns `{"activity": [...]}` for one item. Entries contain timestamp/actor/action fields and an action-specific payload.

### `POST /api/team/plan`

The item assignee or portal admin may replace one section at a time. `section` is `description` or `plan`.

```json
{
  "item": "team/calibrate-the-rig",
  "section": "plan",
  "text": "1. Run calibration\n2. Attach result\n3. Mark complete"
}
```

The response is the updated panel payload.

### `GET /api/team/file/{hash}`

Returns an attachment blob. Add `?dl=1` for `Content-Disposition: attachment`.

## Portal chat API

Bearer authentication also works on the portal's native Kairos chat routes because they share the same identity gate:

- `GET /api/chat/threads`
- `POST /api/chat/thread` with `{op,id,title,rock}`
- `POST /api/chat/ask` with `{thread,text,ritual,context}`
- `GET /api/chat/engine`
- `POST /api/chat/proposal` with `{thread,msg,index,apply}`

These are currently portal-client contracts rather than the stable team API surface. Prefer the item/plan tools above for long-lived integrations, and tolerate additive chat response fields.

## Token-management routes

These routes intentionally accept only the signed-in portal cookie, not bearer tokens.

- `GET /api/tokens` → `{"tokens":[{"id","label","created","last_used","revoked"}]}`
- `POST /api/tokens` with `{"label":"Claude Code"}` → `{"id","label","created","token"}`
- `DELETE /api/tokens/{id}` → `{"ok":true}`

## `curl` examples

Set the token without putting it in shell history repeatedly:

```sh
export AION_PORTAL_TOKEN='aiontok_...'
```

Who am I and read the complete board:

```sh
curl -sS -H "Authorization: Bearer $AION_PORTAL_TOKEN" \
  https://portal.aion.bio/api/me

curl -sS -H "Authorization: Bearer $AION_PORTAL_TOKEN" \
  https://portal.aion.bio/api/team/snapshot
```

Comment and notify Kairos:

```sh
curl -sS https://portal.aion.bio/api/team/comment \
  -H "Authorization: Bearer $AION_PORTAL_TOKEN" \
  -H 'Content-Type: application/json' \
  --data '{"item":"team/calibrate-the-rig","text":"Draft verification steps","mentions":["agent:kairos"]}'
```

Propose an item:

```sh
curl -sS https://portal.aion.bio/api/team/proposals \
  -H "Authorization: Bearer $AION_PORTAL_TOKEN" \
  -H 'Content-Type: application/json' \
  --data '{"target":"rj@aion.bio","title":"Review calibration output","kind":"task"}'
```

Assign and run Kairos:

```sh
curl -sS https://portal.aion.bio/api/team/assign \
  -H "Authorization: Bearer $AION_PORTAL_TOKEN" \
  -H 'Content-Type: application/json' \
  --data '{"item":"team/calibrate-the-rig","owner":"agent:kairos"}'

curl -sS https://portal.aion.bio/api/team/fire \
  -H "Authorization: Bearer $AION_PORTAL_TOKEN" \
  -H 'Content-Type: application/json' \
  --data '{"item":"team/calibrate-the-rig"}'
```
