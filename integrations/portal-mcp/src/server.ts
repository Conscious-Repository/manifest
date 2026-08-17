import { McpServer } from "@modelcontextprotocol/server";
import * as z from "zod/v4";

import { PortalClient, itemPath } from "./client.js";

type ToolResult = {
  content: Array<{ type: "text"; text: string }>;
  structuredContent?: Record<string, unknown>;
  isError?: boolean;
};

function ok(value: unknown): ToolResult {
  const structuredContent = value !== null && typeof value === "object" && !Array.isArray(value)
    ? value as Record<string, unknown>
    : { value };
  return {
    content: [{ type: "text", text: JSON.stringify(value, null, 2) }],
    structuredContent,
  };
}

function failed(error: unknown): ToolResult {
  return {
    content: [{ type: "text", text: error instanceof Error ? error.message : String(error) }],
    isError: true,
  };
}

function run(fn: () => Promise<unknown>): Promise<ToolResult> {
  return fn().then(ok, failed);
}

const item = z.string().min(1).describe("AION item id, for example team/calibrate-the-rig");
const kind = z.enum(["task", "decision"]).optional();
const due = z.string().optional().describe("YYYY-MM-DD, or omit");

export function createServer(client = new PortalClient()): McpServer {
  const server = new McpServer(
    { name: "aion-portal", version: "0.1.0" },
    {
      instructions: "These tools act as the AION teammate identified by AION_PORTAL_TOKEN. Existing assignee/admin locks always apply. Read team state before mutating an unfamiliar item. Agent mentions must be passed as structural mention tokens such as agent:kairos.",
    },
  );

  server.registerTool("whoami", {
    description: "Return the AION teammate represented by the configured token.",
    inputSchema: z.object({}),
    annotations: { readOnlyHint: true },
  }, () => run(() => client.request("api/me")));

  server.registerTool("list_team_state", {
    description: "Read the complete AION team view: published items and rocks plus live comments, overrides, team items, and proposals.",
    inputSchema: z.object({}),
    annotations: { readOnlyHint: true },
  }, () => run(() => client.request("api/team/snapshot")));

  server.registerTool("comment", {
    description: "Comment on a known item. Pass structural agent mentions such as agent:kairos in mentions.",
    inputSchema: z.object({
      item,
      text: z.string().min(1).max(4000),
      mentions: z.array(z.string().min(1)).optional(),
    }),
    annotations: { readOnlyHint: false, destructiveHint: false },
  }, ({ item, text, mentions }) => run(() => client.request("api/team/comment", "POST", { item, text, mentions })));

  server.registerTool("add_item", {
    description: "Create a team item owned by the authenticated teammate.",
    inputSchema: z.object({
      title: z.string().min(1), kind, rock: z.string().optional(), due,
    }),
    annotations: { readOnlyHint: false, destructiveHint: false },
  }, args => run(() => client.request("api/team/items", "POST", args)));

  server.registerTool("update_item", {
    description: "Update an item's team-editable fields. Only the current assignee can do this.",
    inputSchema: z.object({
      item,
      status: z.enum(["open", "in_progress", "done"]).optional(),
      done_on: z.string().optional(),
      due,
      needed_by: z.string().optional(),
      outcome: z.string().optional(),
    }).refine(v => [v.status, v.done_on, v.due, v.needed_by, v.outcome].some(x => x !== undefined), {
      message: "provide at least one field to update",
    }),
    annotations: { readOnlyHint: false, destructiveHint: true, idempotentHint: true },
  }, ({ item, ...fields }) => run(() => client.request(`api/team/item/${itemPath(item)}`, "PATCH", fields)));

  server.registerTool("propose_item", {
    description: "Propose a new item for another @aion.bio teammate.",
    inputSchema: z.object({
      target: z.string().email(), title: z.string().min(1), kind, rock: z.string().optional(), due,
    }),
    annotations: { readOnlyHint: false, destructiveHint: false },
  }, args => run(() => client.request("api/team/proposals", "POST", args)));

  server.registerTool("decide_proposal", {
    description: "Approve or reject a proposal. Only its target or the portal admin may decide it.",
    inputSchema: z.object({ id: z.string().min(1), approve: z.boolean() }),
    annotations: { readOnlyHint: false, destructiveHint: true },
  }, args => run(() => client.request("api/team/proposals/decide", "POST", args)));

  server.registerTool("list_agents", {
    description: "List team-visible agents and their intent personas.",
    inputSchema: z.object({}),
    annotations: { readOnlyHint: true },
  }, () => run(() => client.request("api/team/agents")));

  server.registerTool("get_panel", {
    description: "Read an item's plan record and delegation state.",
    inputSchema: z.object({ item }),
    annotations: { readOnlyHint: true },
  }, ({ item }) => run(() => client.request(`api/team/panel?id=${encodeURIComponent(item)}`)));

  server.registerTool("assign_agent", {
    description: "Assign a team agent to an item, or pass an empty owner to clear the assignment.",
    inputSchema: z.object({ item, owner: z.string().describe("Usually agent:kairos, or empty to clear") }),
    annotations: { readOnlyHint: false, destructiveHint: true, idempotentHint: true },
  }, args => run(() => client.request("api/team/assign", "POST", args)));

  server.registerTool("fire_agent", {
    description: "Execute the saved plan for an agent-held item.",
    inputSchema: z.object({ item }),
    annotations: { readOnlyHint: false, destructiveHint: true },
  }, args => run(() => client.request("api/team/fire", "POST", args)));

  server.registerTool("read_activity", {
    description: "Read one item's attributed activity trail.",
    inputSchema: z.object({ item }),
    annotations: { readOnlyHint: true },
  }, ({ item }) => run(() => client.request(`api/team/activity?item=${encodeURIComponent(item)}`)));

  server.registerTool("write_plan", {
    description: "Replace one plan-file section. Only the item assignee or portal admin may write it.",
    inputSchema: z.object({
      item,
      section: z.enum(["description", "plan"]),
      text: z.string(),
    }),
    annotations: { readOnlyHint: false, destructiveHint: true, idempotentHint: true },
  }, args => run(() => client.request("api/team/plan", "POST", args)));

  return server;
}
