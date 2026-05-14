# SDK Overview

Nexus ships three first-class client SDKs. Each provides identical functionality — choose the one that matches your application's language.

## Choose Your SDK

=== "Go"

    **Best for**: Go services, agents, and MCP servers built with [`mcp-go`](https://github.com/mark3labs/mcp-go).

    ```bash
    go get github.com/Prescott-Data/nexus-framework/nexus-sdk@latest
    ```

    → [Go SDK Reference](go.md)

=== "TypeScript"

    **Best for**: Node.js services, Next.js apps, and MCP servers built with the [MCP TypeScript SDK](https://github.com/modelcontextprotocol/typescript-sdk).

    ```bash
    npm install @dromos/nexus-sdk
    ```

    → [TypeScript SDK Reference](typescript.md)

=== "Python"

    **Best for**: Python services, FastAPI apps, and MCP servers built with the [MCP Python SDK](https://github.com/modelcontextprotocol/python-sdk). Zero dependencies.

    ```bash
    pip install nexus-sdk
    ```

    → [Python SDK Reference](python.md)

---

## Feature Parity Matrix

All three SDKs expose the same capabilities:

| Feature | Go | TypeScript | Python |
|---|:---:|:---:|:---:|
| `RequestConnection` | ✅ | ✅ | ✅ |
| `CheckConnection` | ✅ | ✅ | ✅ |
| `GetToken` (by connection ID) | ✅ | ✅ | ✅ |
| `RefreshConnection` | ✅ | ✅ | ✅ |
| `WaitForActive` | ✅ | ✅ | ✅ |
| `ResolveToken` (workspace + provider) | ✅ | ✅ | ✅ |
| Token Cache (TTL-aware) | ✅ | ✅ | ✅ |
| Auth Injection | `RoundTripper` | `createFetcher` | `authenticated_fetch` |
| Retry + Exponential Backoff | ✅ | ✅ | ✅ |
| Structured Errors | ✅ | ✅ | ✅ |
| MCP stdio-safe logging | — | ✅ (stderr) | ✅ (stderr) |
| Runtime Dependencies | 0 | 2 | 0 |

---

## Two Workflows

The SDKs support two distinct usage patterns:

### 1. Standard App Workflow

For backend services that manage OAuth connections on behalf of users. The full connection lifecycle:

```
request_connection() → redirect user → wait_for_active() → get_token()
```

### 2. MCP Server Workflow

For MCP servers that need to make authorized API calls on behalf of a workspace/tenant. Token management is fully automated:

```
createFetcher() / authenticated_fetch() / AuthenticatedHTTPClient()
    → auto-resolves token from gateway
    → caches with TTL
    → injects Authorization: Bearer header
    → makes API call
```

See the [MCP Integration Guide](../guides/mcp-integration.md) for end-to-end walkthroughs in all three languages.
