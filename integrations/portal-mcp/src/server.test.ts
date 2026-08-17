import assert from "node:assert/strict";
import http from "node:http";
import { fileURLToPath } from "node:url";
import test from "node:test";

import { Client } from "@modelcontextprotocol/client";
import { StdioClientTransport } from "@modelcontextprotocol/client/stdio";

test("stdio server negotiates and exposes the planned tools", async t => {
  const portal = http.createServer((req, res) => {
    assert.equal(req.headers.authorization, "Bearer test-token");
    res.setHeader("Content-Type", "application/json");
    if (req.url === "/api/me") {
      res.end(JSON.stringify({ email: "hannah@aion.bio", admin: false }));
      return;
    }
    if (req.url === "/api/team/snapshot") {
      res.end(JSON.stringify({ backlog: { items: [] }, goals: { goals: [] }, team: { comments: {} } }));
      return;
    }
    res.statusCode = 404;
    res.end("not found");
  });
  await new Promise<void>(resolve => portal.listen(0, "127.0.0.1", resolve));
  t.after(() => portal.close());
  const address = portal.address();
  assert.ok(address && typeof address === "object");

  const entry = fileURLToPath(new URL("./index.js", import.meta.url));
  const transport = new StdioClientTransport({
    command: process.execPath,
    args: [entry],
    stderr: "pipe",
    env: {
      ...process.env,
      AION_PORTAL_TOKEN: "test-token",
      AION_PORTAL_URL: `http://127.0.0.1:${address.port}`,
    } as Record<string, string>,
  });
  const client = new Client({ name: "aion-portal-test", version: "0.1.0" });
  t.after(async () => { await client.close(); });
  await client.connect(transport);
  const result = await client.listTools();
  const names = result.tools.map(tool => tool.name).sort();
  assert.deepEqual(names, [
    "add_item", "assign_agent", "comment", "decide_proposal", "fire_agent",
    "get_panel", "list_agents", "list_team_state", "propose_item", "read_activity",
    "update_item", "whoami", "write_plan",
  ]);

  const whoami = await client.callTool({ name: "whoami", arguments: {} });
  assert.equal(whoami.isError, undefined);
  assert.deepEqual(whoami.structuredContent, { email: "hannah@aion.bio", admin: false });

  const snapshot = await client.callTool({ name: "list_team_state", arguments: {} });
  assert.equal(snapshot.isError, undefined);
  assert.deepEqual(snapshot.structuredContent, {
    backlog: { items: [] }, goals: { goals: [] }, team: { comments: {} },
  });
});
