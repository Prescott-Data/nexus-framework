# The Security Model

Nexus is built on one principle: agents should never hold master secrets. An agent that is compromised should give an attacker only a short-lived, scoped access token. The refresh token or API key that would let them persist.

This page explains how that boundary is enforced, what the three critical environment variables are and what happens if any of them is mishandled, and how the network is hardened to limit the blast radius of a credential leak.

---

## Master secrets and usage secrets

Nexus draws a hard line between two categories of credential.

Master secrets are the credentials that grant long-term or permanent access: OAuth refresh tokens and provider API keys. These are stored in the Broker's PostgreSQL database, encrypted at rest. No service other than the Broker ever holds a master secret. The Gateway does not store them. The Bridge does not store them. Your application stores only a `connection_id` that refers to them.

Usage secrets are the credentials that grant short-lived access: OAuth access tokens and the signed headers produced from API keys. These are held in the Bridge's process memory for the duration of a request or connection and discarded. If an attacker gains access to an agent process, they obtain at most a usage secret that expires within an hour.

| Category | Examples | Where it lives | Lifetime |
|---|---|---|---|
| Master secret | Refresh token, API key | Broker database (encrypted) | Persistent |
| Usage secret | Access token, signed header | Bridge process memory | Less than one hour |

---

## The three critical environment variables

### ENCRYPTION_KEY

The `ENCRYPTION_KEY` is a 32-byte, Base64-encoded value that the Broker uses for AES-GCM 256-bit encryption of all tokens stored in PostgreSQL. Generate it with:

```bash
openssl rand -base64 32
```

If this key is lost or changed, every stored connection in the database becomes permanently unreadable. There is no recovery path. You will need to delete all stored connections and require users to reconnect their provider accounts.

Store this key in a secrets manager (Azure Key Vault, AWS Secrets Manager, HashiCorp Vault). Inject it as an environment variable at deploy time. Never commit it to version control. Treat it as you would a private key for a certificate authority.

### STATE_KEY

The `STATE_KEY` is a 32-byte, Base64-encoded value used to sign the `state` and `nonce` parameters during the OAuth handshake. Both the Broker and the Gateway must have the same `STATE_KEY`. Generate it with:

```bash
openssl rand -base64 32
```

If the keys differ between Broker and Gateway, every OAuth callback will fail with an invalid state error. Both services perform a startup check and will exit with a fatal error if `STATE_KEY` is absent:

```
FATAL: STATE_KEY environment variable is required and must be identical across Broker and Gateway
```

In orchestrated deployments (Kubernetes, Docker Swarm, Azure Container Apps), inject this from a shared secret object so both services always receive the same value.

### API_KEY / BROKER_API_KEY

The `API_KEY` on the Broker and the corresponding `BROKER_API_KEY` on the Gateway authenticate the Gateway-to-Broker channel. The Gateway includes this key in every request it proxies to the Broker. If this key is compromised, an attacker can query any stored connection's token and register or delete providers.

---

## Network hardening

The Broker supports an `ALLOWED_CIDRS` configuration that restricts which IP addresses can reach it. In production, set this to the IP address (or CIDR range) of the Gateway. This means that even if the Broker's API key is leaked, it cannot be used from any host outside your trusted network.

```
ALLOWED_CIDRS=10.0.0.0/8
```

The Gateway should be the only service with a path to the Broker. Agents talk to the Gateway; nothing else reaches the Broker.

Mutual TLS between the Gateway and Broker is on the roadmap. Until it ships, the API key plus network-level IP allowlisting is the recommended defense-in-depth posture.

---

## Audit log

Every mutation that affects the control plane is written to the `audit_events` table. This includes provider creates, updates, and deletes; OAuth flow completions; token retrievals; and token refresh failures. Each record captures the event type, the structured event data, the caller IP address (respecting `X-Forwarded-For`), and the User-Agent.

The audit log is queryable via `GET /audit` on the Broker. See the [Audit Log reference](../reference/audit-log.md) for the schema and query parameters.

If you manage providers declaratively with `nexus-cli`, every `apply` run generates audit log entries in addition to the git history of your manifest file, giving you two independent records of every provider change.
