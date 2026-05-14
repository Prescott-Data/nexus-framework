import { McpServer } from "@modelcontextprotocol/sdk/server/mcp.js";
import { StdioServerTransport } from "@modelcontextprotocol/sdk/server/stdio.js";
import { z } from "zod";
import { NexusClient } from "../../src/index.js";
const GATEWAY_URL = process.env.NEXUS_GATEWAY_URL || "http://localhost:8090";
const API_KEY = process.env.NEXUS_API_KEY || "nexus-admin-key";
const nexus = new NexusClient({ gatewayUrl: GATEWAY_URL, apiKey: API_KEY });
const server = new McpServer({
    name: "nexus-test-slack",
    version: "1.0.0",
});
server.tool("slack_list_channels", "List Slack channels the bot has access to", {
    workspace_id: z.string().describe("The Nexus Workspace ID"),
}, async (args) => {
    try {
        const authedFetch = nexus.createFetcher({
            provider: "slack",
            workspaceId: args.workspace_id,
        });
        const response = await authedFetch("https://slack.com/api/conversations.list?limit=5&types=public_channel", {
            headers: { "Accept": "application/json" },
        });
        if (!response.ok) {
            const errText = await response.text();
            throw new Error(`Slack API ${response.status}: ${errText}`);
        }
        const data = await response.json();
        if (!data.ok) {
            throw new Error(`Slack error: ${data.error}`);
        }
        const channels = (data.channels || []).map((c) => ({
            name: c.name,
            id: c.id,
            members: c.num_members,
        }));
        return {
            content: [{ type: "text", text: JSON.stringify(channels, null, 2) }],
        };
    }
    catch (error) {
        return {
            isError: true,
            content: [{ type: "text", text: `Slack error: ${error.message}` }],
        };
    }
});
async function run() {
    const transport = new StdioServerTransport();
    await server.connect(transport);
    console.error("[slack-server] Running on stdio");
}
run().catch(console.error);
//# sourceMappingURL=slack.js.map