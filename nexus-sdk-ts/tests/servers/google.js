import { McpServer } from "@modelcontextprotocol/sdk/server/mcp.js";
import { StdioServerTransport } from "@modelcontextprotocol/sdk/server/stdio.js";
import { z } from "zod";
import { NexusClient } from "../../src/index.js";
const GATEWAY_URL = process.env.NEXUS_GATEWAY_URL || "http://localhost:8090";
const API_KEY = process.env.NEXUS_API_KEY || "nexus-admin-key";
const nexus = new NexusClient({ gatewayUrl: GATEWAY_URL, apiKey: API_KEY });
const server = new McpServer({
    name: "nexus-test-google",
    version: "1.0.0",
});
server.tool("google_userinfo", "Fetch the authenticated Google user's profile information", {
    workspace_id: z.string().describe("The Nexus Workspace ID"),
}, async (args) => {
    try {
        const authedFetch = nexus.createFetcher({
            provider: "google",
            workspaceId: args.workspace_id,
        });
        const response = await authedFetch("https://www.googleapis.com/oauth2/v3/userinfo", {
            headers: { "Accept": "application/json" },
        });
        if (!response.ok) {
            const errText = await response.text();
            throw new Error(`Google API ${response.status}: ${errText}`);
        }
        const userInfo = await response.json();
        return {
            content: [{ type: "text", text: JSON.stringify({
                        name: userInfo.name,
                        email: userInfo.email,
                        picture: userInfo.picture,
                    }, null, 2) }],
        };
    }
    catch (error) {
        return {
            isError: true,
            content: [{ type: "text", text: `Google error: ${error.message}` }],
        };
    }
});
async function run() {
    const transport = new StdioServerTransport();
    await server.connect(transport);
    console.error("[google-server] Running on stdio");
}
run().catch(console.error);
//# sourceMappingURL=google.js.map