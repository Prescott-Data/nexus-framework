import { McpServer } from "@modelcontextprotocol/sdk/server/mcp.js";
import { StdioServerTransport } from "@modelcontextprotocol/sdk/server/stdio.js";
import { z } from "zod";
import { NexusClient } from "../../src/index.js";

const GATEWAY_URL = process.env.NEXUS_GATEWAY_URL || "http://localhost:8090";
const API_KEY = process.env.NEXUS_API_KEY || "nexus-admin-key";

const nexus = new NexusClient({ gatewayUrl: GATEWAY_URL, apiKey: API_KEY });

const server = new McpServer({
  name: "nexus-test-github",
  version: "1.0.0",
});

server.tool(
  "github_list_repos",
  "List the authenticated user's GitHub repositories",
  {
    workspace_id: z.string().describe("The Nexus Workspace ID"),
  },
  async (args) => {
    try {
      const authedFetch = nexus.createFetcher({
        provider: "github",
        workspaceId: args.workspace_id,
      });

      const response = await authedFetch("https://api.github.com/user/repos?per_page=5&sort=updated", {
        headers: {
          "Accept": "application/vnd.github.v3+json",
          "User-Agent": "Nexus-MCP-Test",
        },
      });

      if (!response.ok) {
        const errText = await response.text();
        throw new Error(`GitHub API ${response.status}: ${errText}`);
      }

      const repos = await response.json() as any[];
      const summary = repos.map((r: any) => ({
        name: r.full_name,
        stars: r.stargazers_count,
        language: r.language,
      }));

      return {
        content: [{ type: "text", text: JSON.stringify(summary, null, 2) }],
      };
    } catch (error: any) {
      return {
        isError: true,
        content: [{ type: "text", text: `GitHub error: ${error.message}` }],
      };
    }
  }
);

async function run() {
  const transport = new StdioServerTransport();
  await server.connect(transport);
  console.error("[github-server] Running on stdio");
}

run().catch(console.error);
