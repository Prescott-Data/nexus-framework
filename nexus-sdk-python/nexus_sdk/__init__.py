"""
Nexus SDK for Python — unified client for Nexus OAuth Gateway.

Provides a synchronous client for both standard app workflows
(connection management, token retrieval) and MCP server workflows
(workspace-scoped token resolution, caching, and authenticated HTTP requests).
"""

from nexus_sdk.client import NexusClient
from nexus_sdk.token_cache import TokenCache
from nexus_sdk.types import (
    NexusClientOptions,
    NexusError,
    RequestConnectionInput,
    RequestConnectionResponse,
    RetryPolicy,
    TokenResponse,
    CachedToken,
)

__all__ = [
    "NexusClient",
    "TokenCache",
    "NexusClientOptions",
    "NexusError",
    "RequestConnectionInput",
    "RequestConnectionResponse",
    "RetryPolicy",
    "TokenResponse",
    "CachedToken",
]

__version__ = "0.2.3"
