# Security Model

The Nexus Framework is built on the principle of **Least Privilege for Agents**. Agents should never hold the "keys to the kingdom" (Refresh Tokens); they should only hold short-lived "Usage Secrets."

## The 3-Key System

Security in Nexus relies on three primary environment variables that must be guarded with extreme care.

### 1. The Encryption Key (`ENCRYPTION_KEY`)
- **Type:** 32-byte Base64 encoded string.
- **Role:** Used for AES-GCM 256-bit encryption of tokens at rest in the PostgreSQL database.
- **Impact:** If compromised, an attacker can decrypt all stored Refresh Tokens. If lost, all existing connections are permanently broken.

### 2. The State Key (`STATE_KEY`)
- **Type:** 32-byte Base64 encoded string.
- **Role:** Used to sign and verify OIDC `state` and `nonce` parameters.
- **Impact:** Prevents CSRF (Cross-Site Request Forgery) and Replay Attacks during the handshake phase. Both the Broker and Gateway must use the same key.

### 3. The API Key (`API_KEY` / `BROKER_API_KEY`)
- **Role:** Authenticates the Gateway to the Broker and the Admin to the Broker.
- **Impact:** Controls access to provider registration and token retrieval.

## Usage Secrets vs. Master Secrets

Nexus enforces a strict boundary between the **Control Plane** and the **Data Plane**.

| Secret Type | Examples | Held By | Lifetime | Risk |
| :--- | :--- | :--- | :--- | :--- |
| **Master Secret** | Refresh Tokens, API Secrets | Broker (Vault) | Persistent | High (Account Takeover) |
| **Usage Secret** | Access Tokens, Signed Headers | Bridge (RAM) | Short (< 1hr) | Low (Temporary Access) |

### Protection Mechanism:
1.  The Broker retrieves the Master Secret (Refresh Token) from the database.
2.  It decrypts the secret and uses it to fetch a new Usage Secret (Access Token).
3.  It returns ONLY the Usage Secret to the Gateway and Bridge.
4.  If an Agent is compromised, the attacker only gains access to that single, short-lived Usage Secret. They cannot pivot to other connections or maintain long-term access.

## Network Hardening

### IP Allowlisting
The Broker supports an `ALLOWED_CIDRS` policy. In production, this should be restricted to the IP address of the **Nexus Gateway**. This ensures that even if an Admin Key is leaked, it cannot be used from outside your trusted network.

### mTLS (Roadmap)
Future versions of Nexus will support mutual TLS between the Gateway and Broker for cryptographically enforced identity beyond API keys.

---

## Audit Trail

Nexus maintains a tamper-evident **audit log** for all control-plane mutations. Every provider create, update, and delete — and every OAuth connection established — writes a record to the `audit_events` table with:

- The **event type** (`provider.created`, `provider.deleted`, `connection.created`, etc.)
- **Structured event data** (provider ID, name, workspace ID)
- The **caller IP address** and **User-Agent**

This audit log is queryable via the [`GET /v1/audit`](audit-log.md) endpoint and is the foundational building block for compliance, forensic analysis, and detecting unauthorized mutations.

!!! tip "GitOps for Auditability"
    For the strongest audit posture, use [`nexus-cli`](../guides/security-as-code.md) to manage providers declaratively. Every `nexus-cli apply` run goes through git history AND generates audit log entries — giving you two independent sources of truth.

---

## `STATE_KEY` Startup Guard

Both the Broker and Gateway will **fatal-exit at startup** if the `STATE_KEY` environment variable is absent:

```
FATAL: STATE_KEY environment variable is required and must be identical across Broker and Gateway
```

This prevents a class of silent misconfiguration where a randomly-generated key would cause all OAuth callbacks to fail with invalid state errors after any service restart. In production, `STATE_KEY` must be:

1. A 32-byte cryptographically random value, Base64 encoded.
2. **Identical** on both the Broker and all Gateway instances.
3. Stored as a managed secret (e.g., Google Secret Manager, AWS Secrets Manager) — not hardcoded.

