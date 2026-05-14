---
icon: material/code-braces
---

# Client Libraries

Nexus ships three client libraries for agents: the Bridge (Go), and SDKs for Go, TypeScript, and Python. They all talk to the same Gateway API. The difference is what they manage for you.

## The Bridge

The Bridge is a Go library that runs inside your agent process and manages the full lifecycle of a persistent connection. You call `MaintainWebSocket` or `MaintainGRPCConnection`, hand it a handler, and the Bridge handles everything: token fetch, credential injection, in-place token refresh before expiry, reconnection with exponential backoff, and Prometheus metrics.

Use the Bridge when your agent holds a long-lived WebSocket or gRPC connection to a provider service and you want the connection to stay authenticated automatically across token rotations.

See [The Bridge](bridge.md) for the full API and configuration options.

## The SDKs

The SDKs are thin HTTP clients for the Gateway API. They expose `GetToken`, `ResolveToken`, `RequestConnection`, and related methods. You call them explicitly when you need a credential and apply it yourself. There is no automatic injection or connection management.

Three SDKs ship with Nexus, all with the same method surface:

| Language | Package | Install |
|---|---|---|
| Go | `nexus-sdk` | `go get github.com/Prescott-Data/nexus-framework/nexus-sdk` |
| TypeScript | `nexus-sdk` | `npm install nexus-sdk` |
| Python | `nexus-sdk` | `pip install nexus-sdk` |

Use an SDK when your agent makes discrete HTTP calls rather than holding a persistent connection, when you are building an MCP server, or when your agent is written in TypeScript or Python.

## MCP servers

Both the Go and TypeScript SDKs include a `ResolveToken` method designed for MCP server integration. An MCP server receives a workspace ID and provider name, calls `ResolveToken` to get the current credential, and injects it into the outgoing request to the provider. The SDK handles token caching and expiry internally.

See [MCP Server Integration](../guides/mcp-integration.md) for complete examples in all three languages.

## Choosing between Bridge and SDK

| | Bridge | SDK |
|---|---|---|
| Connection type | Persistent WebSocket or gRPC | Discrete HTTP calls |
| Credential injection | Automatic | Manual |
| Token refresh | In-place, transparent | Caller responsibility |
| Reconnection | Built-in with backoff | Not applicable |
| Languages | Go only | Go, TypeScript, Python |
| MCP servers | Not the right tool | Designed for this |

If your agent is Go and holds a persistent connection, use the Bridge. In all other cases, use an SDK.
