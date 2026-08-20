# Nexus Sidecar

`nexus-sidecar` is a standalone HTTP reverse proxy for agents that should not receive raw provider credentials. Agents send ordinary HTTP requests to the sidecar, identify the Nexus connection to use, and the sidecar retrieves credentials from the Gateway and applies the provider's auth strategy before forwarding the request upstream.

## Configuration

| Variable | Required | Default | Description |
|---|---:|---|---|
| `PORT` | No | `8070` | HTTP listen port. |
| `GATEWAY_BASE_URL` | Yes | - | Nexus Gateway base URL, for example `http://gateway:8090`. |
| `NEXUS_ROUTES` | Yes | - | Comma-separated route allowlist, for example `github=https://api.github.com,slack=https://slack.com/api`. |
| `TOKEN_CACHE_TTL` | No | disabled | Fallback token cache TTL when the Gateway response does not include `expires_in` or `expires_at`. |
| `REQUEST_BODY_LIMIT` | No | `10MiB` | Maximum request body buffered for body-signing strategies such as `hmac_payload` and `aws_sigv4`. |

Route names may contain letters, numbers, `_`, and `-`. Route targets must be `http` or `https` URLs.

## Calling Through The Sidecar

The sidecar supports two routing styles:

Path-prefixed routing:

```bash
curl http://localhost:8070/github/user/repos \
  -H "X-Nexus-Connection-ID: $CONNECTION_ID"
```

Header-based routing:

```bash
curl http://localhost:8070/user/repos \
  -H "X-Nexus-Provider: github" \
  -H "X-Nexus-Connection-ID: $CONNECTION_ID"
```

In both cases, the sidecar fetches `GET /v1/token/{connection_id}` from the Gateway, applies the returned `strategy` using the bridge auth engine, strips caller-supplied Nexus and authorization headers, and forwards the request to the configured upstream.

## Local Run

```bash
GATEWAY_BASE_URL=http://localhost:8090 \
NEXUS_ROUTES=github=https://api.github.com \
go run ./cmd/nexus-sidecar
```

Health and metrics endpoints:

```bash
curl http://localhost:8070/health
curl http://localhost:8070/metrics
```

## Python Example

See [examples/python_requests.py](examples/python_requests.py) for a minimal `requests` client that calls GitHub through the sidecar without handling an access token directly.
