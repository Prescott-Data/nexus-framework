# Nexus

Nexus is a credential broker for autonomous agents. It sits between your agents and every third-party service they need to reach, and it manages the entire credential lifecycle so your agents never hold OAuth tokens, API keys, or refresh tokens directly.

When an agent needs to call Salesforce, Google Drive, or any other provider, it asks the Nexus broker for a session. The broker validates the request, fetches a short-lived access token, and returns only that. The agent uses the token and discards it. If the agent is compromised, the attacker has nothing durable.

Nexus handles token storage, refresh scheduling, OAuth handshakes, and audit logging as infrastructure concerns. You configure providers once and your agents authenticate against them through a single API surface.

---

## Quick start

The fastest path to a running Nexus stack is Docker Compose. This brings up the Broker, Gateway, PostgreSQL, and Redis.

```bash
cp .env.example .env
# Fill in ENCRYPTION_KEY and STATE_KEY — see Getting Started for key generation
make up
```

Once running:

| Service  | Default address         |
|----------|-------------------------|
| Broker   | http://localhost:8080   |
| Gateway  | http://localhost:8090   |

---

## How to read this documentation

If you are new to Nexus, start with [How Nexus Works](concepts/how-nexus-works.md). It explains the four-component architecture, the two data flows (OAuth handshake and credential retrieval), and the security boundary the system enforces. Reading it once makes every other page easier to understand.

After that, follow the [Getting Started](getting-started/quickstart.md) guide to deploy a working stack and make your first credential request.

If you are integrating agents that need third-party credentials, the [Agent Integration guide](guides/integrating-agents.md) covers the Go Bridge library and the manual HTTP flow for non-Go clients.

The [Guides](guides/managing-providers.md) cover the operational tasks you will return to repeatedly: registering providers, managing provider state declaratively with `nexus-cli`, and auditing credential access.

---

## Components at a glance

**The Broker** is the authority. It holds encrypted refresh tokens and API keys, runs the OAuth handshake with providers, and operates the background token refresh loop. No other service ever sees a refresh token.

**The Gateway** is the public API. Agents talk to the Gateway; the Gateway proxies requests to the Broker over an internal API key. Agents never reach the Broker directly.

**The Bridge** is a Go library that runs inside your agent process. It retrieves credentials from the Gateway and injects them into outgoing HTTP and gRPC requests automatically.

**The SDK** is a thin Go client for the Gateway API. Use it when you want direct control over credential retrieval without the full Bridge abstraction.

---

## Links

- [GitHub repository](https://github.com/Prescott-Data/nexus-framework)
- [OpenAPI specification](https://github.com/Prescott-Data/nexus-framework/blob/main/openapi.yaml)
- [CHANGELOG](https://github.com/Prescott-Data/nexus-framework/blob/main/CHANGELOG.md)
- [Prescott Data developer portal](https://developers.prescottdata.io)