---
icon: material/home
---

<div class="nx-hero" markdown>

<img src="assets/nexus-logo-blue.png" alt="Nexus" class="nx-hero-logo" />

# Nexus

### Auth infrastructure for autonomous agents.

One authority for every service your agents touch. Nexus orchestrates provider connections, token lifecycle, agent identity, scoped sessions, and on-behalf-of delegation — your agents never write auth code or hold a raw secret.


<div class="nx-cta" markdown>

[Get started](getting-started/quickstart.md){ .nx-btn .nx-btn-primary }
[View on GitHub](https://github.com/Prescott-Data/nexus-framework){ .nx-btn .nx-btn-github target="_blank" rel="noopener" }

</div>

</div>

---

## What Nexus does

Every agent that connects to an external service hits the same wall. OAuth flows, token refresh loops, credential rotation, per-provider auth implementations — all of it written from scratch, per integration, per team. Nexus eliminates that wall entirely.

Register a provider once. Every agent in your fleet connects to it through a single authority. The Broker handles the OAuth handshake, encrypts the token at rest, runs the refresh loop, and issues only a short-lived credential when an agent asks. The agent uses it and discards it. If the agent is compromised, the attacker has nothing durable.

Beyond the OAuth layer, Nexus covers the full auth surface that production agent systems require. Agents are registered principals with declared scope ceilings — a `crm-agent` registered with `crm:contacts:read` cannot request `crm:delete`, even if the underlying connection has that scope. When a human user triggers an agent mission, the Broker validates the user's permission, stamps the session with their identity and tenant context, and enforces data isolation across every downstream operation. Internal business operations — `acme:gliding`, `pipeline:trigger` — are first-class scopes enforced at the same authority as OAuth tokens. Every credential request, session open, and session close is logged in a tamper-evident audit trail.

<div class="nx-grid" markdown>

<div class="nx-card" markdown>
<span class="nx-card-label">Broker</span>

**The authority.** Holds all master secrets encrypted at rest. Runs the background refresh loop. Never exposed to agents directly.
</div>

<div class="nx-card" markdown>
<span class="nx-card-label">Gateway</span>

**The public API.** Agents call the Gateway. It proxies to the Broker over an internal channel. Agents never reach the Broker.
</div>

<div class="nx-card" markdown>
<span class="nx-card-label">Bridge</span>

**The Go library.** Runs inside your agent process. Fetches credentials and injects them into outgoing HTTP and gRPC requests automatically.
</div>

<div class="nx-card" markdown>
<span class="nx-card-label">SDKs</span>

**Three first-class clients.** Go, TypeScript, and Python. Direct Gateway access for explicit credential fetches and MCP server integration.
</div>

</div>

---

## Quick start

```bash
cp .env.example .env
# Generate ENCRYPTION_KEY and STATE_KEY — see Getting Started
make up
```

Broker runs on `localhost:8080`. Gateway runs on `localhost:8090`.

---

## Where to start

Start with [Architecture](concepts/architecture.md) under the Concepts tab. It establishes the control plane and data plane split, the OAuth handshake flow, and the credential retrieval model. Every other page assumes that mental model.

Then follow [Deploy in Five Minutes](getting-started/quickstart.md) to run a stack and make your first connection. After that, the [Guides](guides/integrating-agents.md) cover the operational tasks you return to repeatedly.

---

## Explore

| | |
|---|---|
| **Source** | Browse the code, open issues, and submit PRs — [GitHub](https://github.com/Prescott-Data/nexus-framework){ target="_blank" rel="noopener" } |
| **OpenAPI** | The full Gateway v1 contract — [openapi.yaml](https://github.com/Prescott-Data/nexus-framework/blob/main/openapi.yaml){ target="_blank" rel="noopener" } |
| **Community** | Questions, showcases, and early feature previews — [Discord](https://discord.gg/AbskSXypq){ target="_blank" rel="noopener" } |
| **Blog** | Engineering deep-dives and architecture walkthroughs — [read the blog](https://developers.prescottdata.io/blog){ target="_blank" rel="noopener" } |