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
| `SECRET_BACKEND` | No | Where OAuth2 provider client secrets are stored. `internal` (default) keeps them in Postgres; `gcp-secret-manager` stores them in Google Cloud Secret Manager. See [Provider Secret Backends](../guides/provider-secret-backends.md). |
| `GCP_PROJECT_ID` | Conditional | Project for `gcp-secret-manager`. Optional on Cloud Run, GCE and GKE. |
| `GCP_SECRET_PREFIX` | No | Secret ID prefix for `gcp-secret-manager`. Default: `nexus-providers` |

---

## Gateway

| Variable | Required | Description |
|---|---|---|
| `BROKER_BASE_URL` | Yes | Internal URL of the Broker. Example: `http://nexus-broker:8080` |
| `BROKER_API_KEY` | Yes | API key used to authenticate the Gateway with the Broker. Must match the Broker's `API_KEY`. |
| `STATE_KEY` | Yes | Same as the shared `STATE_KEY`. Must match the Broker exactly. |
| `PORT` | No | Port the Gateway listens on. Default: `8090` |
| `GATEWAY_REQUIRE_API_KEY` | No | When `true`, every route except `/health`, `/auth/callback`, `/v1/capture-schema` and `/v1/capture-credential` requires a valid `X-API-Key`. Default: `false`, which logs a warning at startup. Turn it on before exposing the Gateway. |
| `GATEWAY_API_KEYS` | Conditional | Comma-separated keys callers (agents, SDKs, the frontend) present to the Gateway. Required with `GATEWAY_REQUIRE_API_KEY=true` unless a key file is set. Distinct from `BROKER_API_KEY`, which the Gateway presents to the Broker. |
| `GATEWAY_API_KEY_FILE` / `GATEWAY_API_KEYS_FILE` | No | Mounted secret file(s) with one key, or comma- or newline-separated keys. Reloaded without restart. |
| `GATEWAY_API_KEY_RELOAD_INTERVAL` | No | How often key files are re-read. Default: `30s` |

---

## Sidecar

| Variable | Required | Description |
|---|---|---|
| `GATEWAY_BASE_URL` | Yes | URL of the Gateway the Sidecar fetches credentials from. Example: `http://nexus-gateway:8090` |
| `NEXUS_ROUTES` | Yes | Comma-separated allowlist of named upstream routes in `name=https://target` format. Example: `github=https://api.github.com,slack=https://slack.com/api` |
| `TOKEN_CACHE_TTL` | No | Fallback cache lifetime for credential payloads that carry no expiry of their own (Go duration, e.g. `5m`). Default: no fallback caching. |
| `REQUEST_BODY_LIMIT` | No | Maximum request body size buffered for body-signing strategies. Accepts sizes like `10MiB`. Default: `10MiB` |
| `PORT` | No | Port the Sidecar listens on. Default: `8070` |

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
