---
icon: material/database-lock-outline
---

# The Broker

The Broker is the only service in a Nexus deployment that holds durable credential material. No other service receives a refresh token, client secret, or plaintext key.

## Database schema

| Table | Purpose |
|---|---|
| `provider_profiles` | OAuth2 and static provider configuration — client ID, client secret, auth/token URLs, scopes, auth strategy |
| `connections` | One row per authorization grant — workspace ID, provider ID, status, PKCE verifier, scopes, return URL |
| `tokens` | Encrypted credential material — one row per connection, enforced by `ON CONFLICT (connection_id) DO UPDATE` |
| `audit_events` | Append-only event log — consent created, token issued, refresh succeeded or failed, connection cleaned up |

## Encryption

All token material is encrypted with AES-GCM 256-bit using your `ENCRYPTION_KEY`. The key must be exactly 32 bytes. A fresh 12-byte nonce from `crypto/rand` is generated per write and prepended to the ciphertext before base64 encoding.

```bash
# Generate ENCRYPTION_KEY
openssl rand -base64 32
```

Losing `ENCRYPTION_KEY` makes every stored token permanently unreadable. There is no recovery. Key rotation requires a migration that decrypts and re-encrypts every token row.

## Token refresh

`POST /connections/{connection_id}/refresh` — the Bridge calls this before each access token expires.

The Broker decrypts the stored token, extracts `refresh_token`, calls the provider's token endpoint, encrypts the new response, and upserts it into `tokens`.

| Provider response | Broker action |
|---|---|
| `2xx` | Encrypt and store new token, return it |
| `4xx` | Set connection status to `attention`, return `attention_required` |
| `5xx` or network error | Return error without changing connection status — Bridge retries |

Static connections (`api_key`, `basic_auth`) cannot be refreshed. Calling refresh on a static connection returns `400 static_token`.

## Connection states

| Status | Meaning |
|---|---|
| `pending` | Consent URL issued, user has not yet authorized |
| `active` | Token stored and usable |
| `attention` | Refresh failed with a `4xx` — user must re-authorize |
| `failed` | Unrecoverable. Delete and recreate. |

## OAuth state signing

The Broker signs every OAuth `state` parameter with HMAC-SHA256 using `STATE_KEY`. The state encodes `{workspace_id, provider_id, nonce, iat}` and expires after 10 minutes. Both the Broker and the Gateway must share the same `STATE_KEY`.

```bash
# Generate STATE_KEY
openssl rand -base64 32
```

## Network isolation

The Broker should only accept connections from the Gateway's IP range. It should not be reachable from the public internet or from agent processes. The Broker API key should be known only to the Gateway and can be supplied through `API_KEY`, `API_KEYS`, `API_KEY_FILE`, or `API_KEYS_FILE`.

For production rotation, mount API keys as secret files and configure `API_KEY_FILE` or `API_KEYS_FILE`. The Broker reloads those files on `API_KEY_RELOAD_INTERVAL` without a process restart.
