---
icon: material/cog-outline
---

# Environment Variables

This page documents every environment variable accepted by the Broker and Gateway. Variables marked **Required** will cause the service to refuse to start if absent.

---

## Shared

Both the Broker and the Gateway must receive the same value for `STATE_KEY`. If they differ, all OAuth callbacks will fail.

| Variable | Required | Description |
|---|---|---|
| `STATE_KEY` | Yes | 32-byte Base64 string used to sign and verify OAuth `state` and `nonce` parameters. Generate with `openssl rand -base64 32`. |

---

## Broker

| Variable | Required | Description |
|---|---|---|
| `DATABASE_URL` | Yes | PostgreSQL connection string. Example: `postgres://nexus:password@localhost:5432/nexus` |
| `REDIS_URL` | Yes | Redis URL for caching and peer discovery. Example: `redis://localhost:6379` |
| `ENCRYPTION_KEY` | Yes | 32-byte Base64 string for AES-GCM 256-bit token encryption. Generate with `openssl rand -base64 32`. This key must never change while connections exist in the database. |
| `STATE_KEY` | Yes | Same as the shared `STATE_KEY`. Must match the Gateway exactly. |
| `API_KEY` | Conditional | Single key that the Gateway and admin callers use to authenticate with the Broker. Required unless `API_KEYS`, `API_KEY_FILE`, or `API_KEYS_FILE` supplies keys. |
| `API_KEYS` | No | Comma-separated Broker API keys. Useful during manual key rollover. |
| `API_KEY_FILE` | No | Path to a mounted secret file containing one Broker API key. Reloaded without process restart. |
| `API_KEYS_FILE` | No | Path to a mounted secret file containing comma- or newline-separated Broker API keys. Reloaded without process restart. |
| `API_KEY_RELOAD_INTERVAL` | No | How often the Broker re-reads `API_KEY_FILE` and `API_KEYS_FILE`. Default: `30s` |
| `BASE_URL` | Yes | The public URL of the Broker, used to construct the OAuth callback URL. Example: `https://broker.example.com` |
| `REDIRECT_PATH` | No | The path appended to `BASE_URL` for the OAuth callback. Default: `/auth/callback` |
| `ALLOWED_CIDRS` | No | Comma-separated list of IP ranges allowed to reach the Broker. In production, restrict this to the Gateway's IP. Example: `10.0.0.0/8` |
| `ALLOWED_RETURN_DOMAINS` | No | Comma-separated list of allowed domains for the `return_url` parameter in connection requests. Prevents open redirect abuse. |
| `REQUIRE_API_KEY` | No | When `true`, the Broker rejects requests without a valid `X-API-Key` header. Default: `true` |
| `REQUIRE_ALLOWLIST` | No | When `true`, the Broker enforces `ALLOWED_CIDRS` for all requests. Default: `false` |
| `PORT` | No | Port the Broker listens on. Default: `8080` |

---

## Gateway

| Variable | Required | Description |
|---|---|---|
| `BROKER_BASE_URL` | Yes | Internal URL of the Broker. Example: `http://nexus-broker:8080` |
| `BROKER_API_KEY` | Yes | API key used to authenticate the Gateway with the Broker. Must match the Broker's `API_KEY`. |
| `STATE_KEY` | Yes | Same as the shared `STATE_KEY`. Must match the Broker exactly. |
| `PORT` | No | Port the Gateway listens on. Default: `8090` |

---

## Key generation

Both `ENCRYPTION_KEY` and `STATE_KEY` are 32-byte values encoded as Base64. Generate them with:

```bash
openssl rand -base64 32
```

Run this command twice, once for each key. Do not reuse the same value for both.

---

## Next steps

For production deployment configuration including Docker, Kubernetes, and Azure Container Apps, see [Deploying Nexus](../infrastructure/deploying-nexus.md).
