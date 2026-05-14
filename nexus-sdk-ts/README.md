# Nexus SDK — TypeScript

The official TypeScript/JavaScript client for the [Nexus Framework](https://github.com/Prescott-Data/nexus-framework) — a provider-agnostic OAuth 2.0 / OIDC integration layer for autonomous agents and MCP servers.

## Install

```bash
npm install @dromos/nexus-sdk
```

## Quick Start

### Standard App Flow

```typescript
import { NexusClient, NexusClientOptions, RequestConnectionInput } from '@dromos/nexus-sdk';

const client = new NexusClient({
  gatewayUrl: 'https://nexus-gateway.example.com',
});

// 1. Initiate OAuth consent
const conn = await client.requestConnection({
  userId:       'user-123',
  providerName: 'github',
  scopes:       ['repo', 'read:user'],
  returnUrl:    'https://myapp.com/callback',
});
// → Redirect user to conn.authUrl

// 2. Poll until user completes consent
const status = await client.waitForActive(conn.connectionId);

// 3. Fetch token
const token = await client.getTokenByConnectionId(conn.connectionId);
console.log(token.accessToken);
```

### MCP Server Flow

Use `createFetcher` to build a `fetch`-compatible function that automatically resolves and injects the correct `Authorization` header for each tenant.

```typescript
import { NexusClient } from '@dromos/nexus-sdk';

const client = new NexusClient({ gatewayUrl: 'https://nexus-gateway.example.com' });

// workspaceId identifies the tenant; provider is "github", "notion", etc.
const fetcher = client.createFetcher({
  workspaceId: 'workspace-123',
  provider:    'github',
});

// Drop-in replacement for fetch — token resolved, cached, and injected automatically.
const resp = await fetcher('https://api.github.com/user/repos');
const repos = await resp.json();
```

> **MCP stdio safety**: All SDK logs are written to `stderr`, never `stdout`. This prevents corruption of the MCP JSON-RPC transport when running as a stdio server.

## Configuration

```typescript
const client = new NexusClient({
  gatewayUrl: 'https://nexus-gateway.example.com',

  // Optional: API key for gateway authentication
  apiKey: 'your-api-key',

  // Optional: retry policy with exponential backoff + jitter
  retryPolicy: {
    retries:    3,
    minDelayMs: 200,
    maxDelayMs: 2000,
    retryOn429: true,
  },

  // Optional: custom logger (must write to stderr for MCP safety)
  logger: {
    info:  (msg, ...args) => console.error(`[MySDK] ${msg}`, ...args),
    warn:  (msg, ...args) => console.error(`[MySDK:WARN] ${msg}`, ...args),
    error: (msg, ...args) => console.error(`[MySDK:ERROR] ${msg}`, ...args),
  },
});
```

## API Reference

### Gateway Methods

| Method | Description |
|---|---|
| `requestConnection(input)` | Initiate an OAuth consent flow. Returns `authUrl` and `connectionId`. |
| `checkConnection(connectionId)` | Poll the connection status (`"active"`, `"pending"`, `"failed"`). |
| `getTokenByConnectionId(connectionId)` | Retrieve the current token for a connection. |
| `refreshConnection(connectionId)` | Force a token refresh via the gateway. |
| `waitForActive(connectionId, intervalMs?, signal?)` | Poll until terminal status. Supports `AbortSignal` for timeout/cancellation. |

### MCP / Workspace Methods

| Method | Description |
|---|---|
| `resolveToken(workspaceId, provider)` | Resolve a fresh token via `GET /v1/resolve`. |
| `getToken(workspaceId, provider)` | Resolve from `TokenManager` cache, or fetch fresh. |
| `clearToken(workspaceId, provider)` | Evict a cached token manually. |
| `createFetcher(options)` | Returns a `fetch`-compatible function with automatic token injection. |

### Types

```typescript
interface NexusClientOptions {
  gatewayUrl:   string;
  apiKey?:      string;
  retryPolicy?: RetryPolicy;
  logger?:      NexusLogger;
}

interface RequestConnectionInput {
  userId:       string;
  providerName: string;
  scopes:       string[];
  returnUrl:    string;
  metadata?:    Record<string, unknown>;
}

interface FetcherOptions {
  workspaceId: string;
  provider:    string;
}
```

## Error Handling

```typescript
import { NexusError } from '@dromos/nexus-sdk';

try {
  const token = await client.getTokenByConnectionId('conn-id');
} catch (err) {
  if (err instanceof NexusError) {
    console.error(err.code);       // e.g., "connection_not_found"
    console.error(err.message);    // human-readable
    console.error(err.statusCode); // HTTP status, if applicable
  }
}
```

## Testing

```bash
# Run the MCP integration test suite against a live gateway
NEXUS_GATEWAY_URL=https://your-gateway.example.com \
NEXUS_API_KEY=your-api-key \
npx tsx tests/runner.ts --providers github,google,notion --workspace your-workspace-id
```

## Notes

- **ESM only**: The package uses native ES Modules with `.js` extensions in imports.
- **Zero MCP dependency**: `createFetcher` works with any HTTP-based API. The `@modelcontextprotocol/sdk` package is a dev dependency only.
- Token types are normalized to `Bearer` (capitalized) per RFC 6750, regardless of how the provider returns them.
