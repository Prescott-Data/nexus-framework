# Security Model

Nexus is built on one rule: agents never hold durable secrets. An access token expires within hours. A refresh token, API key, or client secret does not. Nexus ensures agents receive only the former.

## Secret separation

| Service | What it holds |
|---|---|
| Broker | Refresh tokens, API keys, client secrets — encrypted at rest |
| Gateway | Nothing. Proxies requests, holds no credential state. |
| Bridge | Short-lived access token in memory, for the duration of one connection. Discarded on close. |

If a Bridge process is compromised, the attacker gets an access token valid for at most the remaining token lifetime. They cannot get the refresh token.

## Encryption at rest

All token material is encrypted with **AES-GCM 256-bit**. The `ENCRYPTION_KEY` must be exactly 32 bytes. A fresh 12-byte nonce from `crypto/rand` is generated per write.

```bash
openssl rand -base64 32   # ENCRYPTION_KEY
```

Losing this key makes all stored tokens permanently unreadable. There is no built-in rotation — rotation requires a migration script that decrypts and re-encrypts every token row.

## OAuth state signing

Every OAuth `state` parameter is signed with **HMAC-SHA256** using `STATE_KEY`. The state payload encodes `{workspace_id, provider_id, nonce, iat}` and is rejected if older than 10 minutes or if the signature does not verify.

```bash
openssl rand -base64 32   # STATE_KEY
```

The Broker and Gateway must share the same `STATE_KEY`. A mismatch causes all OAuth callbacks to fail.

## PKCE

All OAuth2 flows use PKCE (RFC 7636). The Broker generates a random `code_verifier` per consent request and sends the SHA-256 `code_challenge` to the provider. The verifier is submitted on callback. This is always enabled — you cannot disable it.

## Network hardening

API keys are not sufficient on their own. Layer network controls on top:

- The Broker should only accept connections from the Gateway's IP range.
- The Gateway's admin paths should only accept connections from your application backend's IP range.
- Agents should only reach the Gateway's token and check-connection endpoints.

CIDR allowlisting is configurable on both the Broker and the Gateway.

## Audit log

Every significant event is written to `audit_events`: consents created, tokens issued, refreshes succeeded or failed, connections cleaned up. Each row includes IP address and User-Agent.

The audit log has no TTL. Implement a scheduled archival job to move rows older than your retention window to cold storage. Without archival, the table grows without bound.

## What Nexus does not enforce

Nexus does not control which agents may request tokens for which connections. If your agent has a `connection_id`, it can call `GET /v1/token/{connection_id}`. Access control over which agents may use which connections belongs to your application layer.

Mutual TLS between internal services is not yet implemented. Rely on network isolation and TLS termination at the load balancer for current deployments.
