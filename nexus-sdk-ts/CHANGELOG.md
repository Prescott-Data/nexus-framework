# Nexus SDK — TypeScript

## v0.2.3 — 2026-05-14

Initial release of the Nexus TypeScript SDK (`@dromos/nexus-sdk`), evolved from the `nexus-mcp-adapter`.

### Features

**Gateway API**
- `requestConnection()` — initiate OAuth consent flows
- `checkConnection()` — poll connection status
- `getTokenByConnectionId()` — retrieve tokens by connection ID
- `refreshConnection()` — force token refresh
- `waitForActive()` — poll with `AbortSignal` support

**MCP / Workspace Token Resolution**
- `resolveToken()` — workspace + provider based resolution via `/v1/resolve`
- `getToken()` — cache-aware token retrieval
- `createFetcher()` — drop-in `fetch` replacement with automatic `Authorization` header injection
- `clearToken()` — manual cache eviction

**Infrastructure**
- Configurable `RetryPolicy` with exponential backoff and jitter
- Structured `NexusError` class with `code`, `message`, and `statusCode`
- `NexusLogger` interface for custom log routing
- Conservative 5-minute TTL fallback when providers omit `expires_at`
- `Bearer` token type normalization per RFC 6750
- All logging to `stderr` — safe for MCP `StdioServerTransport`
- ESM-native with `verbatimModuleSyntax` compliance

### Validated Against

- GitHub OAuth (token retrieval + API call)
- Google OAuth (token retrieval + userinfo)
- Notion OAuth (token retrieval + search API, including Bearer normalization fix)
