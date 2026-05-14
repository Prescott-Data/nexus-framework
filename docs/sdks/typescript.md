---
icon: material/language-typescript
---

# TypeScript SDK

The `@dromos/nexus-sdk` package is a TypeScript/JavaScript client for the Nexus Gateway supporting both standard OAuth connection management and MCP server token injection.

## Install

```bash
npm install @dromos/nexus-sdk
```

!!! note "ESM Only"
    The package ships as native ES Modules. Ensure your `tsconfig.json` has `"module": "NodeNext"` or `"module": "ESNext"`.

## Initialization

```typescript
import { NexusClient } from '@dromos/nexus-sdk';

// Minimal
const client = new NexusClient({ gatewayUrl: 'https://nexus-gateway.example.com' });

// With options
const client = new NexusClient({
  gatewayUrl:  'https://nexus-gateway.example.com',
  apiKey:      'your-api-key',
  retryPolicy: { retries: 3, minDelayMs: 200, maxDelayMs: 2000, retryOn429: true },
  logger:      { info: console.error, warn: console.error, error: console.error },
});
```

## Standard App Methods

### requestConnection

```typescript
const conn = await client.requestConnection({
  userId:       'user-123',
  providerName: 'github',
  scopes:       ['repo', 'read:user'],
  returnUrl:    'https://myapp.com/callback',
});
// conn.authUrl      → redirect user here
// conn.connectionId → persist this
```

### checkConnection

```typescript
const status = await client.checkConnection(connectionId);
// 'active' | 'pending' | 'failed'
```

### waitForActive

Polls `checkConnection` until terminal status. Supports `AbortSignal` for timeout.

```typescript
const controller = new AbortController();
setTimeout(() => controller.abort(), 5 * 60 * 1000); // 5-minute timeout

const status = await client.waitForActive(connectionId, 1500, controller.signal);
```

### getTokenByConnectionId

```typescript
const token = await client.getTokenByConnectionId(connectionId);
console.log(token.accessToken);
```

### refreshConnection

```typescript
const newToken = await client.refreshConnection(connectionId);
```

## MCP / Workspace Methods

### resolveToken

Resolves a fresh token via `GET /v1/resolve`. Returns a `NexusTokenInfo` (suitable for caching).

```typescript
const tokenInfo = await client.resolveToken('workspace-123', 'github');
```

### getToken

Cache-aware. Fetches fresh only on cache miss or expiry (5-minute conservative default if no `expires_at`).

```typescript
const tokenInfo = await client.getToken('workspace-123', 'github');
```

### createFetcher

Returns a `fetch`-compatible function with automatic token injection. This is the **primary integration point for MCP server tool handlers**.

```typescript
const fetcher = client.createFetcher({
  workspaceId: 'workspace-123',
  provider:    'github',
});

// Use exactly like the native fetch API
const resp = await fetcher('https://api.github.com/user/repos');
const repos = await resp.json();
```

!!! warning "MCP stdio Safety"
    All SDK diagnostics write to `stderr` exclusively. Never use `console.log` (stdout) in code that runs inside a `StdioServerTransport` — it will corrupt the JSON-RPC stream. The SDK already handles this correctly.

### clearToken

```typescript
client.clearToken('workspace-123', 'github');
// Forces the next getToken() call to fetch fresh
```

## Error Handling

```typescript
import { NexusError } from '@dromos/nexus-sdk';

try {
  const token = await client.getTokenByConnectionId('conn-id');
} catch (err) {
  if (err instanceof NexusError) {
    console.error(err.code);       // 'connection_not_found'
    console.error(err.message);    // human-readable
    console.error(err.statusCode); // HTTP status
  }
}
```

## Type Reference

```typescript
interface NexusClientOptions {
  gatewayUrl:   string;
  apiKey?:      string;
  retryPolicy?: RetryPolicy;
  logger?:      NexusLogger;
}

interface RetryPolicy {
  retries:      number;
  minDelayMs?:  number;
  maxDelayMs?:  number;
  retryOn429?:  boolean;
}

interface FetcherOptions {
  workspaceId: string;
  provider:    string;
}

interface TokenResponse {
  accessToken:   string;
  tokenType?:    string;
  expiresAt?:    string | number;
  refreshToken?: string;
  credentials?:  Record<string, unknown>;
  raw?:          Record<string, unknown>;
}
```

## Security Notes

- All SDK logs write to `stderr` — safe for MCP stdio transports.
- Token types are normalized to `Bearer` (capitalized) per RFC 6750, regardless of how the provider returns `token_type`.
- A conservative 5-minute TTL is applied when providers omit `expires_at`, preventing silent cache of stale tokens.
