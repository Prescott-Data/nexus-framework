---
icon: material/shield-lock-outline
---

# Security Model

Nexus is built on one rule: agents never hold durable secrets. An access token expires within hours. A refresh token, API key, or client secret does not. Nexus ensures agents receive only the former.

## Secret separation

| Service | What it holds |
|---|---|
| Broker | Refresh tokens and captured credentials, AES-GCM encrypted at rest. Provider client secrets: in the `provider_profiles` row with the default `internal` backend (not encrypted by the Broker; rely on database encryption and backup handling), or, with `SECRET_BACKEND=gcp-secret-manager`, in Secret Manager for every provider registered or rotated after the switch; rows from before it keep their column value until rotated. See [Provider Secret Backends](../guides/provider-secret-backends.md). |
| Gateway | No user credentials: it proxies requests and never stores a token. It holds three configuration keys: `STATE_KEY` (shared with the Broker, verifies OAuth state), `BROKER_API_KEY` (to call the Broker), and, when `GATEWAY_REQUIRE_API_KEY=true` (off by default), the caller keys from `GATEWAY_API_KEYS` or the configured key files. |
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

Broker API keys can be rotated without restarting the Broker by mounting key files with `API_KEY_FILE` or `API_KEYS_FILE`. The files are reloaded according to `API_KEY_RELOAD_INTERVAL`.

## Audit log

Every significant event is written to `audit_events`: consents created, tokens issued, refreshes succeeded or failed, connections cleaned up. Each row includes IP address and User-Agent.

The audit log has no TTL. Implement a scheduled archival job to move rows older than your retention window to cold storage. Without archival, the table grows without bound.

## Whose credential, for whom

A connection belongs to the workspace that consented to it. A caller names the
subject it acts for with `X-Nexus-Subject` (the subject's workspace id), and may
name the agent (`X-Nexus-Agent`) and the operation (`X-Nexus-Operation`). The
broker releases a credential only for the subject that owns the connection, on
every route that hands one out: `GET /connections/{id}/token`,
`GET /connections/resolve`, `POST /connections/{id}/refresh` and agent sessions.
The gateway forwards the three headers (and their gRPC metadata) unchanged.

`SUBJECT_BINDING` sets how strictly:

| Value | A request naming the owner | Naming someone else | Naming nobody |
|---|---|---|---|
| `optional` (default) | Released | Refused, `403 subject_mismatch` | Released, as before 0.7 |
| `required` | Released | Refused, `403 subject_mismatch` | Refused, `403 subject_required` |
| `off` | Released | Released | Released |

Subjects are compared exactly, as workspace ids are stored: `ACME` is not
`acme`. `GET /connections/{id}/status` (and the gateway's
`/v1/check-connection/{id}`) report a connection's status without releasing a
credential, so they need no subject.

Use `required` where agents act for people: then a `connection_id` alone
releases nothing. A connection whose owner cannot be established is refused, not
trusted (`subject_unverifiable`). Every release and refusal is in `audit_events`
with the subject, agent and operation; an agent session records the subject it
acts for (`acting_for`, `obo: true`).

## What Nexus does not enforce

The subject is asserted by the caller that holds the gateway or broker API key,
which is your runtime. Nexus binds a credential to the subject that caller names;
it does not authenticate the person behind it. A caller that holds the key can
name any subject, so keep the key to the runtime, and see the roadmap for
workload identity (DPoP or mTLS) and token exchange.

Mutual TLS between internal services is not yet implemented. Rely on network isolation and TLS termination at the load balancer for current deployments.
