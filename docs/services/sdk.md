# Nexus SDKs

The Nexus Framework ships three first-class client SDKs with full feature parity. See the [SDK Overview](../sdks/index.md) for a complete guide.

| SDK | Language | Package |
|---|---|---|
| [Go SDK](../sdks/go.md) | Go | `github.com/Prescott-Data/nexus-framework/nexus-sdk` |
| [TypeScript SDK](../sdks/typescript.md) | TypeScript / JavaScript | `@dromos/nexus-sdk` |
| [Python SDK](../sdks/python.md) | Python ≥ 3.11 | `nexus-sdk` |

## Capabilities

All SDKs provide:

- **Connection lifecycle** — `requestConnection`, `checkConnection`, `waitForActive`, `getToken`, `refreshConnection`
- **MCP token injection** — automatic `Authorization: Bearer` header injection via workspace + provider resolution
- **Token caching** — thread-safe, TTL-aware in-memory cache with configurable safety buffer
- **Retry logic** — exponential backoff with jitter
- **Structured errors** — machine-readable error codes from the gateway

See the [MCP Server Integration Guide](../guides/mcp-integration.md) for end-to-end examples.
