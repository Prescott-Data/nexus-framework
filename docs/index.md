<div class="nx-hero" markdown>

<img src="../assets/monogram-brand.svg" alt="Nexus" class="nx-hero-logo">

# Nexus

Credential brokering for autonomous agents. Nexus sits between your agents and every third-party service they reach, managing the full credential lifecycle so your agents never hold OAuth tokens, API keys, or refresh tokens directly.

<div class="nx-cta-text" markdown>

[Deploy in five minutes](getting-started/quickstart.md) · [How Nexus works](concepts/how-nexus-works.md) · [GitHub](https://github.com/Prescott-Data/nexus-framework)

</div>

<div class="nx-cta" markdown>

[Get started](getting-started/quickstart.md){ .nx-btn .nx-btn-primary }
[View on GitHub](https://github.com/Prescott-Data/nexus-framework){ .nx-btn .nx-btn-github }

</div>

<div class="nx-stats" markdown>
<div class="nx-stat"><span class="nx-stat-value">AES-256</span><span class="nx-stat-label">Token encryption</span></div>
<div class="nx-stat"><span class="nx-stat-value">4</span><span class="nx-stat-label">Core components</span></div>
<div class="nx-stat"><span class="nx-stat-value">v0.4</span><span class="nx-stat-label">Current release</span></div>
<div class="nx-stat"><span class="nx-stat-value">Go</span><span class="nx-stat-label">Runtime</span></div>
</div>

</div>

---

## What Nexus does

When an agent needs to call Salesforce, Google Drive, or any other provider, it asks the Nexus Gateway for a session. The Gateway forwards the request to the Broker, which decrypts the stored refresh token, fetches a fresh access token from the provider, and returns only that short-lived token. The agent uses it and discards it. If the agent is compromised, the attacker has nothing durable.

Nexus handles token storage, refresh scheduling, OAuth handshakes, and audit logging as infrastructure. You configure providers once. Your agents authenticate against them through a single API surface.

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
<span class="nx-card-label">SDK</span>

**The thin client.** Direct Gateway access for explicit credential fetches. Use when you want control rather than automation.
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

## Documentation map

Read [How Nexus Works](concepts/how-nexus-works.md) first. It establishes the control plane / data plane split, the OAuth handshake, and the credential retrieval model. Every other page assumes that mental model.

Then follow [Deploy in Five Minutes](getting-started/quickstart.md) to run a stack and make your first connection. After that, the [Guides](guides/integrating-agents.md) cover the operational tasks you return to repeatedly.

---

## Links

- [GitHub repository](https://github.com/Prescott-Data/nexus-framework)
- [OpenAPI spec](https://github.com/Prescott-Data/nexus-framework/blob/main/openapi.yaml)
- [CHANGELOG](https://github.com/Prescott-Data/nexus-framework/blob/main/CHANGELOG.md)
- [Prescott Data developer portal](https://developers.prescottdata.io)