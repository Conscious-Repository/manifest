import assert from "node:assert/strict";
import http from "node:http";
import test from "node:test";

import { PortalClient, PortalError, itemPath } from "./client.js";

test("PortalClient sends bearer auth and parses JSON", async t => {
  const server = http.createServer((req, res) => {
    assert.equal(req.headers.authorization, "Bearer test-token");
    assert.equal(req.url, "/api/team/state");
    res.setHeader("Content-Type", "application/json");
    res.end(JSON.stringify({ items: ["ok"] }));
  });
  await new Promise<void>(resolve => server.listen(0, "127.0.0.1", resolve));
  t.after(() => server.close());
  const addr = server.address();
  assert.ok(addr && typeof addr === "object");
  const client = new PortalClient(`http://127.0.0.1:${addr.port}`, "test-token");
  assert.deepEqual(await client.request("api/team/state"), { items: ["ok"] });
});

test("PortalClient surfaces status and plain-text API errors", async t => {
  const server = http.createServer((_req, res) => {
    res.statusCode = 403;
    res.end("only the assignee can change this item\n");
  });
  await new Promise<void>(resolve => server.listen(0, "127.0.0.1", resolve));
  t.after(() => server.close());
  const addr = server.address();
  assert.ok(addr && typeof addr === "object");
  const client = new PortalClient(`http://127.0.0.1:${addr.port}`, "test-token");
  await assert.rejects(client.request("api/team/state"), error => {
    assert.ok(error instanceof PortalError);
    assert.equal(error.status, 403);
    assert.match(error.message, /only the assignee/);
    return true;
  });
});

test("itemPath preserves slash-delimited ids while escaping segments", () => {
  assert.equal(itemPath("team/a plan"), "team/a%20plan");
  assert.throws(() => itemPath("../api/tokens"), /safe slash-delimited/);
});
