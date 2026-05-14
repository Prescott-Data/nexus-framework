import { McpServer } from "@modelcontextprotocol/sdk/server/mcp.js";
import { StdioServerTransport } from "@modelcontextprotocol/sdk/server/stdio.js";
import { z } from "zod";
import { NexusClient } from "../../src/index.js";

const GATEWAY_URL = process.env.NEXUS_GATEWAY_URL || "http://localhost:8090";
const API_KEY = process.env.NEXUS_API_KEY || "nexus-admin-key";

const nexus = new NexusClient({ gatewayUrl: GATEWAY_URL, apiKey: API_KEY });

const server = new McpServer({
  name: "nexus-test-salesforce",
  version: "1.0.0",
});

server.tool(
  "salesforce_query",
  "Execute a SOQL query against Salesforce",
  {
    workspace_id: z.string().describe("The Nexus Workspace ID"),
    soql: z.string().describe("The SOQL query to execute").default("SELECT Id, Name FROM Account LIMIT 5"),
  },
  async (args) => {
    try {
      const authedFetch = nexus.createFetcher({
        provider: "salesforce",
        workspaceId: args.workspace_id,
      });

      // Salesforce requires knowing the instance URL. In a real setup, the token
      // response from Nexus would include the instance_url in its credentials.
      // For testing, we use login.salesforce.com's REST API endpoint.
      const instanceUrl = process.env.SALESFORCE_INSTANCE_URL || "https://login.salesforce.com";
      const queryUrl = `${instanceUrl}/services/data/v59.0/query?q=${encodeURIComponent(args.soql)}`;

      const response = await authedFetch(queryUrl, {
        headers: { "Accept": "application/json" },
      });

      if (!response.ok) {
        const errText = await response.text();
        throw new Error(`Salesforce API ${response.status}: ${errText}`);
      }

      const data = await response.json() as any;
      const records = (data.records || []).map((r: any) => ({
        id: r.Id,
        name: r.Name,
      }));

      return {
        content: [{ type: "text", text: JSON.stringify({ totalSize: data.totalSize, records }, null, 2) }],
      };
    } catch (error: any) {
      return {
        isError: true,
        content: [{ type: "text", text: `Salesforce error: ${error.message}` }],
      };
    }
  }
);

async function run() {
  const transport = new StdioServerTransport();
  await server.connect(transport);
  console.error("[salesforce-server] Running on stdio");
}

run().catch(console.error);
