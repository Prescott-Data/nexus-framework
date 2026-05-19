---
icon: material/language-python
---

# Python SDK

The `nexus-sdk` Python package is a zero-dependency client for the Nexus Gateway supporting both standard OAuth connection management and MCP server token injection.

**Zero runtime dependencies** — uses only the Python standard library. Requires Python ≥ 3.11.

## Install

```bash
pip install nexus-sdk
```

## Initialization

```python
from nexus_sdk import NexusClient, NexusClientOptions
from nexus_sdk.types import RetryPolicy

# Minimal
client = NexusClient(NexusClientOptions(
    gateway_url='https://nexus-gateway.example.com',
))

# With options
client = NexusClient(NexusClientOptions(
    gateway_url='https://nexus-gateway.example.com',
    api_key='your-api-key',
    timeout=15.0,
    retry_policy=RetryPolicy(
        retries=3,
        min_delay=0.2,
        max_delay=2.0,
        retry_on_429=True,
    ),
))
```

## Standard App Methods

### request_connection

```python
from nexus_sdk import RequestConnectionInput

conn = client.request_connection(RequestConnectionInput(
    user_id='user-123',
    provider_name='github',
    scopes=['repo', 'read:user'],
    return_url='https://myapp.com/callback',
))
# conn.auth_url      → redirect user here
# conn.connection_id → persist this
```

### check_connection

```python
status = client.check_connection(connection_id)
# 'active' | 'pending' | 'failed'
```

### wait_for_active

Polls `check_connection` until the status is terminal or the timeout elapses.

```python
status = client.wait_for_active(
    connection_id,
    interval=1.5,   # seconds between polls (default: 1.5)
    timeout=300.0,  # total timeout in seconds (default: 300)
)
```

### get_token_by_connection_id

```python
token = client.get_token_by_connection_id(connection_id)
print(token.access_token)
```

### refresh_connection

```python
new_token = client.refresh_connection(connection_id)
```

## MCP / Workspace Methods

### resolve_token

Resolves a fresh token via `GET /v1/resolve`.

```python
from nexus_sdk import TokenCache

cache = TokenCache()
token = client.resolve_token('workspace-123', 'github')
```

### get_cached_token

Cache-aware. Fetches fresh only on cache miss or expiry (5-minute conservative default if no `expires_at`).

```python
token = client.get_cached_token(cache, 'workspace-123', 'github')
print(token.access_token)
```

### authenticated_fetch

Makes an HTTP request with the Nexus auth headers automatically injected. This is the **primary integration point for MCP server tool handlers**.

```python
import json

status, headers, body = client.authenticated_fetch(
    cache,
    'workspace-123',
    'github',
    'https://api.github.com/user/repos',
    method='GET',
    headers={'User-Agent': 'MyApp/1.0'},
)

repos = json.loads(body)
```

Returns a tuple: `(status_code: int, headers: dict, body: bytes)`.

!!! warning "MCP stdio Safety"
    All SDK logs are written to `stderr` via Python's `logging` module. Never use `print()` to stdout in an MCP server — it will corrupt the JSON-RPC stream. The SDK already handles this correctly.

### clear_token

```python
client.clear_token(cache, 'workspace-123', 'github')
# Forces the next get_cached_token() call to fetch fresh
```

## TokenCache

Thread-safe, TTL-aware in-memory cache. Safe to share across threads.

```python
from nexus_sdk import TokenCache

# buffer_seconds: evict tokens this early before actual expiry (default: 30)
cache = TokenCache(buffer_seconds=30.0)

cache.get('workspace-id', 'github')          # → CachedToken | None
cache.set('workspace-id', 'github', token)   # store
cache.delete('workspace-id', 'github')       # evict
```

## Error Handling

```python
from nexus_sdk.types import NexusError

try:
    token = client.get_token_by_connection_id('conn-id')
except NexusError as e:
    print(e.code)        # 'connection_not_found'
    print(e.message)     # human-readable
    print(e.status_code) # HTTP status, if applicable
```

## Type Reference

```python
@dataclass
class NexusClientOptions:
    gateway_url:  str
    api_key:      str = ''
    retry_policy: RetryPolicy = field(default_factory=RetryPolicy)
    timeout:      float = 30.0

@dataclass
class RetryPolicy:
    retries:      int   = 0
    min_delay:    float = 0.2
    max_delay:    float = 2.0
    retry_on_429: bool  = False

@dataclass
class TokenResponse:
    access_token:  str
    token_type:    str | None = None
    expires_at:    str | None = None
    refresh_token: str | None = None
    credentials:   dict | None = None
    raw:           dict | None = None
```

## Security Notes

- All SDK logs write to `stderr` — safe for MCP stdio transports.
- Token types are normalized to `Bearer` (capitalized) per RFC 6750.
- A conservative 5-minute TTL is applied when providers omit `expires_at`, preventing silent caching of stale tokens.
- Zero runtime dependencies reduce supply chain attack surface.
