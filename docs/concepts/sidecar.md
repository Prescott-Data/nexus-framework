---
icon: material/shield-link-variant
---

# The Sidecar

The Sidecar is a standalone HTTP proxy that runs next to an agent process. It lets Python, TypeScript, shell scripts, and other non-Go agents call provider APIs without linking the Go Bridge or handling raw access tokens directly.

## How It Works

1. The agent sends an HTTP request to `nexus-sidecar` with `X-Nexus-Connection-ID`.
2. The sidecar resolves the connection through the Gateway using `GET /v1/token/{connection_id}`.
3. The Gateway response includes provider credentials and a `strategy` object.
4. The sidecar applies the strategy with the same auth engine used by the Bridge.
5. The request is forwarded to the configured upstream route and the response is returned to the agent.

## Routing

Routes are explicit. `NEXUS_ROUTES` maps a short route name to an upstream base URL:

```bash
NEXUS_ROUTES=github=https://api.github.com,slack=https://slack.com/api
```

Agents can select a route with a path prefix:

```bash
curl http://localhost:8070/github/user/repos \
  -H "X-Nexus-Connection-ID: conn_123"
```

They can also select a route with `X-Nexus-Provider` and keep the original provider path unchanged:

```bash
curl http://localhost:8070/user/repos \
  -H "X-Nexus-Provider: github" \
  -H "X-Nexus-Connection-ID: conn_123"
```

The sidecar rejects unknown routes instead of proxying arbitrary destinations.

## Credential Boundary

The agent does not receive the token payload from Nexus. It only receives the upstream response. Before forwarding, the sidecar strips caller-supplied `Authorization`, `X-Nexus-Connection-ID`, `X-Nexus-Provider`, and other `X-Nexus-*` headers so agent-controlled headers cannot override Nexus-managed credentials or leak sidecar metadata upstream.

## Supported Strategies

The sidecar delegates request mutation to `nexus-bridge/pkg/auth`, so it supports the same HTTP auth strategies as the Bridge:

| Strategy | Behavior |
|---|---|
| `oauth2` | Sets `Authorization: Bearer <access_token>`. |
| `header` | Sets a configured header from a credential field. |
| `query_param` | Adds a configured query parameter. |
| `basic_auth` | Sets HTTP Basic auth. |
| `hmac_payload` | Signs the buffered request body and sets a configured signature header. |
| `aws_sigv4` | Applies AWS Signature Version 4 request signing. |
| `path` | Renders credential placeholders in path or query templates. |

## Operations

The service exposes:

| Endpoint | Description |
|---|---|
| `/health` | Basic liveness response. |
| `/metrics` | Prometheus metrics, including `nexus_sidecar_proxy_requests_total`. |

Configure `REQUEST_BODY_LIMIT` to cap the memory used by body-signing strategies. The default is `10MiB`.
