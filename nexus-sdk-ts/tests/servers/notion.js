import { McpServer } from "@modelcontextprotocol/sdk/server/mcp.js";
import { StdioServerTransport } from "@modelcontextprotocol/sdk/server/stdio.js";
import { z } from "zod";
import { NexusClient } from "../../src/index.js";
const GATEWAY_URL = process.env.NEXUS_GATEWAY_URL || "http://localhost:8090";
const API_KEY = process.env.NEXUS_API_KEY || "nexus-admin-key";
const nexus = new NexusClient({ gatewayUrl: GATEWAY_URL, apiKey: API_KEY });
const server = new McpServer({
    name: "nexus-test-notion",
    version: "1.0.0",
});
server.tool("notion_search", "Search across all Notion pages and databases the integration has access to", {
    workspace_id: z.string().describe("The Nexus Workspace ID"),
    query: z.string().describe("Search query string").default(""),
}, async (args) => {
    try {
        const authedFetch = nexus.createFetcher({
            provider: "notion",
            workspaceId: args.workspace_id,
        });
        const response = await authedFetch("https://api.notion.com/v1/search", {
            method: "POST",
            headers: {
                "Content-Type": "application/json",
                "Notion-Version": "2022-06-28",
            },
            body: JSON.stringify({
                query: args.query,
                page_size: 5,
            }),
        });
        if (!response.ok) {
            const errText = await response.text();
            throw new Error(`Notion API ${response.status}: ${errText}`);
        }
        const data = await response.json();
        const results = (data.results || []).map((r) => ({
            id: r.id,
            type: r.object,
            title: r.properties?.title?.title?.[0]?.plain_text
                || r.properties?.Name?.title?.[0]?.plain_text
                || r.title?.[0]?.plain_text
                || "(untitled)",
        }));
        return {
            content: [{ type: "text", text: JSON.stringify(results, null, 2) }],
        };
    }
    catch (error) {
        return {
            isError: true,
            content: [{ type: "text", text: `Notion error: ${error.message}` }],
        };
    }
});
async function run() {
    const transport = new StdioServerTransport();
    await server.connect(transport);
    console.error("[notion-server] Running on stdio");
}
run().catch(console.error);
//# sourceMappingURL=notion.js.map