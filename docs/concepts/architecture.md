# Architecture

Nexus splits credential management into two planes.

The **control plane** handles credential lifecycle: registering providers, completing OAuth handshakes, storing tokens, and scheduling refreshes. The **data plane** delivers credentials to agents on demand.

## The four services

**Broker** — the trust boundary. The only service that holds plaintext credential material. Owns the database, runs the encryption vault, and executes token refreshes. Never reachable by agents.

**Gateway** — the public API. Agents and your backend talk to the Gateway. It proxies credential requests to the Broker over an internal channel. Agents never see the Broker address.

**Bridge** — the agent library. Runs inside your agent process. Fetches tokens from the Gateway, injects them into outgoing requests, and refreshes them before expiry. Your agent code never touches a token directly.

**SDK** — the thin client. Wraps the Gateway's REST API for explicit credential fetches. The Bridge uses the SDK internally; you can use it directly when you need manual control.

## Data flow

```
Your agent
  └─ Bridge (in-process)
       └─ GET /v1/token/{connection_id}
            └─ Gateway
                 └─ Broker (internal network only)
                      └─ PostgreSQL (encrypted tokens)
```

Agents initiate all communication. The Broker never pushes data outward.

## What Nexus does not manage

Nexus has no user table. `workspace_id` is an opaque string you choose — typically your application's user ID. Nexus does not decide which agents may access which connections. That enforcement belongs to your application.
