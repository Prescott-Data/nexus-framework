# MCP Server Integration

This guide shows how to build a **Model Context Protocol (MCP) server** that uses the Nexus SDK to make authorized API calls on behalf of a workspace/tenant — in TypeScript, Go, and Python.

## What is MCP?

[Model Context Protocol (MCP)](https://modelcontextprotocol.io) is an open standard for exposing tools and data sources to AI agents. An **MCP server** exposes a set of tools (e.g., "list GitHub repos", "post a Slack message") that an AI agent can invoke. MCP servers typically run as child processes communicating over stdio.

When an MCP server needs to call a third-party API on behalf of a user, it needs an access token — and that token must be scoped to the right tenant. Nexus handles this automatically.

## How it Works

```
AI Agent → MCP Client → MCP Server (your code) → Nexus SDK → Nexus Gateway → Upstream API
                                                       ↑
                                              (resolves & caches token)
```

1. The MCP server receives a tool invocation from the AI agent, e.g. `list_repos`.
2. The tool handler calls the Nexus SDK's auth injector (`createFetcher` / `AuthenticatedHTTPClient` / `authenticated_fetch`).
3. The SDK checks its token cache for the workspace+provider pair. On a cache miss, it calls `GET /v1/resolve` on the Nexus Gateway.
4. The SDK injects the `Authorization: Bearer <token>` header and makes the upstream API call.
5. The result is returned to the AI agent.

---

## TypeScript MCP Server

=== "Tool Handler"

    ```typescript
    import { Server } from '@modelcontextprotocol/sdk/server/index.js';
    import { StdioServerTransport } from '@modelcontextprotocol/sdk/server/stdio.js';
    import { NexusClient } from '@dromos/nexus-sdk';

    const WORKSPACE_ID = process.env.WORKSPACE_ID!;
    const GATEWAY_URL  = process.env.NEXUS_GATEWAY_URL!;

    const nexus = new NexusClient({ gatewayUrl: GATEWAY_URL });
    const fetcher = nexus.createFetcher({ workspaceId: WORKSPACE_ID, provider: 'github' });

    const server = new Server({ name: 'github-server', version: '1.0.0' }, {
      capabilities: { tools: {} },
    });

    server.setRequestHandler(ListToolsRequestSchema, async () => ({
      tools: [{
        name: 'list_repos',
        description: 'List GitHub repositories for the authenticated user',
        inputSchema: { type: 'object', properties: {}, required: [] },
      }],
    }));

    server.setRequestHandler(CallToolRequestSchema, async (req) => {
      if (req.params.name === 'list_repos') {
        const resp = await fetcher('https://api.github.com/user/repos?per_page=10&sort=updated');
        const repos = await resp.json() as any[];
        return {
          content: [{ type: 'text', text: repos.map(r => r.full_name).join('\n') }],
        };
      }
      throw new Error(`Unknown tool: ${req.params.name}`);
    });

    const transport = new StdioServerTransport();
    await server.connect(transport);
    // All console.error() calls are safe — NexusClient already uses stderr only.
    console.error('[github-server] Running on stdio');
    ```

!!! warning "stdout is sacred"
    In an MCP stdio server, stdout is the JSON-RPC channel. Any `console.log()` call will corrupt the protocol. Always use `console.error()` for diagnostics. The Nexus SDK already does this internally.

---

## Go MCP Server

Using [`mark3labs/mcp-go`](https://github.com/mark3labs/mcp-go):

```go
package main

import (
    "context"
    "encoding/json"
    "io"
    "time"

    "github.com/mark3labs/mcp-go/mcp"
    "github.com/mark3labs/mcp-go/server"
    oauthsdk "github.com/Prescott-Data/nexus-framework/nexus-sdk"
)

func main() {
    nexus  := oauthsdk.New(os.Getenv("NEXUS_GATEWAY_URL"))
    cache  := oauthsdk.NewTokenCache(30 * time.Second)
    workspace := os.Getenv("WORKSPACE_ID")

    // Authenticated HTTP client — token resolved and injected automatically
    gh := nexus.AuthenticatedHTTPClient(cache, workspace, "github")

    s := server.NewMCPServer("github-server", "1.0.0")

    s.AddTool(mcp.NewTool("list_repos",
        mcp.WithDescription("List GitHub repositories"),
    ), func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
        resp, err := gh.Get("https://api.github.com/user/repos?per_page=10&sort=updated")
        if err != nil {
            return mcp.NewToolResultError(err.Error()), nil
        }
        defer resp.Body.Close()

        var repos []map[string]any
        json.NewDecoder(resp.Body).Decode(&repos)

        names := make([]string, 0, len(repos))
        for _, r := range repos {
            names = append(names, r["full_name"].(string))
        }
        return mcp.NewToolResultText(strings.Join(names, "\n")), nil
    })

    server.ServeStdio(s)
}
```

---

## Python MCP Server

Using the [`mcp`](https://github.com/modelcontextprotocol/python-sdk) Python package:

```python
import asyncio
import json
import os

import mcp.server.stdio
from mcp.server import Server
from mcp.types import Tool, TextContent, CallToolResult

from nexus_sdk import NexusClient, NexusClientOptions, TokenCache

WORKSPACE_ID = os.environ["WORKSPACE_ID"]
GATEWAY_URL  = os.environ["NEXUS_GATEWAY_URL"]

nexus = NexusClient(NexusClientOptions(gateway_url=GATEWAY_URL))
cache = TokenCache()

app = Server("github-server")

@app.list_tools()
async def list_tools():
    return [Tool(
        name="list_repos",
        description="List GitHub repositories for the authenticated user",
        inputSchema={"type": "object", "properties": {}, "required": []},
    )]

@app.call_tool()
async def call_tool(name: str, arguments: dict):
    if name == "list_repos":
        status, _, body = nexus.authenticated_fetch(
            cache, WORKSPACE_ID, "github",
            "https://api.github.com/user/repos?per_page=10&sort=updated",
            headers={"User-Agent": "my-mcp-server/1.0"},
        )
        repos = json.loads(body)
        names = "\n".join(r["full_name"] for r in repos)
        return [TextContent(type="text", text=names)]
    raise ValueError(f"Unknown tool: {name}")

async def main():
    async with mcp.server.stdio.stdio_server() as (r, w):
        await app.run(r, w, app.create_initialization_options())

if __name__ == "__main__":
    asyncio.run(main())
```

---

## Common Pitfalls

| Issue | Cause | Fix |
|---|---|---|
| MCP client gets garbled responses | Using `console.log` / `print()` to stdout | Use `console.error` (TS) / `logging` to stderr (Python) |
| 401 errors after a few minutes | Token TTL too long, caching stale tokens | SDK defaults to 5-min TTL when `expires_at` is missing |
| Notion returns 401 with valid token | `token_type: "bearer"` (lowercase) sent as-is | SDK normalizes to `Bearer` per RFC 6750 |
| `connection_not_found` error | No active OAuth connection for the workspace | Run the consent flow for that workspace+provider pair |
| Cold start latency | First request resolves token synchronously | Pre-warm: call `get_cached_token` at server startup |

---

## Consent Flow

Before an MCP server can fetch tokens, a connection must be established. Initiate the OAuth consent flow with any of the SDKs:

=== "TypeScript"

    ```typescript
    const conn = await nexus.requestConnection({
      userId: 'workspace-123', providerName: 'github',
      scopes: ['repo'], returnUrl: 'https://your-app.com/callback',
    });
    console.log('Authorize here:', conn.authUrl);
    const status = await nexus.waitForActive(conn.connectionId);
    ```

=== "Go"

    ```go
    conn, _ := nexus.RequestConnection(ctx, oauthsdk.RequestConnectionInput{
        UserID: "workspace-123", ProviderName: "github",
        Scopes: []string{"repo"}, ReturnURL: "https://your-app.com/callback",
    })
    log.Println("Authorize here:", conn.AuthURL)
    nexus.WaitForActive(ctx, conn.ConnectionID, 1500*time.Millisecond)
    ```

=== "Python"

    ```python
    conn = nexus.request_connection(RequestConnectionInput(
        user_id='workspace-123', provider_name='github',
        scopes=['repo'], return_url='https://your-app.com/callback',
    ))
    print('Authorize here:', conn.auth_url)
    nexus.wait_for_active(conn.connection_id)
    ```
