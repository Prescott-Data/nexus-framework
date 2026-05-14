import { McpServer } from "@modelcontextprotocol/sdk/server/mcp.js";
import { StdioServerTransport } from "@modelcontextprotocol/sdk/server/stdio.js";
import { z } from "zod";
import { NexusClient } from "../src/index";
// 1. Initialize the global Nexus Client
const nexus = new NexusClient({
    gatewayUrl: "https://gateway.nexus.local",
    apiKey: "nexus_test_key_123",
});
// 2. Initialize the MCP Server
const server = new McpServer({
    name: "github-multi-tenant-server",
    version: "1.0.0",
});
// 3. Define a tool that uses the NexusClient to fetch data
server.tool("github_search_prs", "Search for pull requests on GitHub for a specific workspace", {
    workspace_id: z.string().describe("The Nexus Workspace ID of the user"),
    query: z.string().describe("The search query for GitHub PRs"),
}, async (args) => {
    try {
        // Create a fetcher scoped to this user and the GitHub provider
        const customFetch = nexus.createFetcher({
            provider: "github",
            workspaceId: args.workspace_id,
        });
        // Use the fetcher to make the API call. 
        // The Authorization header is automatically injected!
        const url = `https://api.github.com/search/issues?q=${encodeURIComponent(args.query)}+type:pr`;
        const response = await customFetch(url, {
            headers: {
                "Accept": "application/vnd.github.v3+json",
                "User-Agent": "Nexus-MCP-Adapter-Example"
            }
        });
        if (!response.ok) {
            throw new Error(`GitHub API returned ${response.status}: ${response.statusText}`);
        }
        const data = await response.json();
        // Return the MCP compliant response
        return {
            content: [
                {
                    type: "text",
                    text: JSON.stringify(data.items || [], null, 2),
                },
            ],
        };
    }
    catch (error) {
        return {
            isError: true,
            content: [
                {
                    type: "text",
                    text: `Failed to search PRs: ${error.message}`,
                },
            ],
        };
    }
});
// Start the server
async function run() {
    const transport = new StdioServerTransport();
    await server.connect(transport);
    console.error("GitHub Multi-Tenant MCP Server running on stdio");
}
run().catch(console.error);
//# sourceMappingURL=index.js.map