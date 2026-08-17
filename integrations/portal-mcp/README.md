# AION portal MCP

Local stdio MCP server for the AION team portal. It maps MCP tools directly onto the authenticated [AION team API](../../docs/team-api.md), so every operation is attributed to the teammate who owns the configured token and keeps the portal's existing assignee/admin locks.

## Prerequisites

- Node.js 20 or newer.
- An AION API token: sign in at `https://portal.aion.bio`, choose **api access**, generate a token, and store it immediately.

## Run from this checkout

```sh
cd integrations/portal-mcp
npm install
npm run build
AION_PORTAL_TOKEN='aiontok_...' npm start
```

After this package is published, the command becomes:

```sh
AION_PORTAL_TOKEN='aiontok_...' npx -y @aion/portal-mcp
```

## Client configuration

Claude Desktop, Claude Code, Cursor, and other stdio MCP hosts accept the same server shape:

```json
{
  "mcpServers": {
    "aion": {
      "command": "npx",
      "args": ["-y", "@aion/portal-mcp"],
      "env": {
        "AION_PORTAL_TOKEN": "aiontok_...",
        "AION_PORTAL_URL": "https://portal.aion.bio"
      }
    }
  }
}
```

For an unpublished checkout, replace `command` and `args` with the absolute Node command for the built file:

```json
{
  "command": "node",
  "args": ["/absolute/path/to/manifest/integrations/portal-mcp/dist/index.js"],
  "env": {
    "AION_PORTAL_TOKEN": "aiontok_..."
  }
}
```

`AION_PORTAL_URL` is optional and defaults to `https://portal.aion.bio`. Set it to a local portal base URL for development.

## Tools

- `whoami`
- `list_team_state` — published items/rocks plus the live team overlay
- `comment`
- `add_item`
- `update_item`
- `propose_item`
- `decide_proposal`
- `list_agents`
- `get_panel`
- `assign_agent`
- `fire_agent`
- `read_activity`
- `write_plan`

HTTP failures are returned as MCP tool errors with the portal's status code and plain-text message, so a host can explain assignee locks, missing items, revoked tokens, and already-running agents.

## Development

```sh
npm run check
npm test
```

To inspect the server interactively after building:

```sh
AION_PORTAL_TOKEN='aiontok_...' \
  npx @modelcontextprotocol/inspector node dist/index.js
```

Never log to stdout in this package: stdout is the MCP JSON-RPC channel. Diagnostics belong on stderr.
