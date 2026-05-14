# Nexus SDK — Python

The official Python client for the [Nexus Framework](https://github.com/Prescott-Data/nexus-framework) — a provider-agnostic OAuth 2.0 / OIDC integration layer for autonomous agents and MCP servers.

**Zero runtime dependencies** — uses only the Python standard library (`urllib`, `threading`). Requires Python ≥ 3.11.

## Install

```bash
pip install nexus-sdk
```

## Quick Start

### Standard App Flow

```python
from nexus_sdk import NexusClient, NexusClientOptions, RequestConnectionInput

client = NexusClient(NexusClientOptions(
    gateway_url='https://nexus-gateway.example.com',
))

# 1. Initiate OAuth consent
conn = client.request_connection(RequestConnectionInput(
    user_id='user-123',
    provider_name='github',
    scopes=['repo', 'read:user'],
    return_url='https://myapp.com/callback',
))
# → Redirect user to conn.auth_url

# 2. Poll until user completes consent
status = client.wait_for_active(conn.connection_id)

# 3. Fetch token
token = client.get_token_by_connection_id(conn.connection_id)
print(token.access_token)
```

### MCP Server Flow

Use `authenticated_fetch` to make HTTP requests with Nexus auth headers automatically injected.

```python
from nexus_sdk import NexusClient, NexusClientOptions, TokenCache
import json

client = NexusClient(NexusClientOptions(
    gateway_url='https://nexus-gateway.example.com',
))
cache = TokenCache()

# workspaceId identifies the tenant; provider is "github", "notion", etc.
status, headers, body = client.authenticated_fetch(
    cache, 'workspace-123', 'github',
    'https://api.github.com/user/repos',
    headers={'User-Agent': 'MyApp/1.0'},
)
repos = json.loads(body)
```

> **MCP stdio safety**: All SDK logs are written to `stderr` via Python's `logging` module, never `stdout`. This prevents corruption of the MCP JSON-RPC transport.

## Configuration

```python
from nexus_sdk import NexusClient, NexusClientOptions
from nexus_sdk.types import RetryPolicy

client = NexusClient(NexusClientOptions(
    gateway_url='https://nexus-gateway.example.com',

    # Optional: API key for gateway authentication
    api_key='your-api-key',

    # Optional: request timeout in seconds (default: 30)
    timeout=15.0,

    # Optional: retry policy with exponential backoff + jitter
    retry_policy=RetryPolicy(
        retries=3,
        min_delay=0.2,
        max_delay=2.0,
        retry_on_429=True,
    ),
))
```

## API Reference

### Gateway Methods

| Method | Description |
|---|---|
| `request_connection(input)` | Initiate an OAuth consent flow. Returns `auth_url` and `connection_id`. |
| `check_connection(connection_id)` | Poll the connection status (`"active"`, `"pending"`, `"failed"`). |
| `get_token_by_connection_id(connection_id)` | Retrieve the current token for a connection. |
| `refresh_connection(connection_id)` | Force a token refresh via the gateway. |
| `wait_for_active(connection_id, interval=1.5, timeout=300.0)` | Poll until terminal status or timeout. |

### MCP / Workspace Methods

| Method | Description |
|---|---|
| `resolve_token(workspace_id, provider)` | Resolve a fresh token via `GET /v1/resolve`. |
| `get_cached_token(cache, workspace_id, provider)` | Resolve from `TokenCache`, or fetch fresh. |
| `clear_token(cache, workspace_id, provider)` | Evict a cached token manually. |
| `authenticated_fetch(cache, workspace_id, provider, url, ...)` | Make an HTTP request with token auto-injected. Returns `(status, headers, body)`. |

### TokenCache

```python
from nexus_sdk import TokenCache

# buffer_seconds: evict tokens this many seconds before actual expiry (default: 30)
cache = TokenCache(buffer_seconds=30.0)

cache.get('workspace-id', 'github')        # → CachedToken | None
cache.set('workspace-id', 'github', token) # store a token
cache.delete('workspace-id', 'github')     # evict manually
```

`TokenCache` is **thread-safe** — safe to share across threads in multi-threaded MCP servers.

## Error Handling

```python
from nexus_sdk.types import NexusError

try:
    token = client.get_token_by_connection_id('conn-id')
except NexusError as e:
    print(e.code)        # e.g., "connection_not_found"
    print(e.message)     # human-readable
    print(e.status_code) # HTTP status, if applicable
```

## Testing

```bash
# Unit tests (no network required)
python -m unittest discover -s tests -v

# Integration smoke test against the live gateway
NEXUS_GATEWAY_URL=https://your-gateway.example.com python tests/smoke_test.py
```

## Notes

- **Zero dependencies**: Uses only `urllib`, `json`, `threading`, and `logging` from the standard library.
- Token types are normalized to `Bearer` (capitalized) per RFC 6750, regardless of how the provider returns them.
- The `authenticated_fetch` method returns raw bytes for the body. Use `json.loads(body)` to parse JSON responses.
