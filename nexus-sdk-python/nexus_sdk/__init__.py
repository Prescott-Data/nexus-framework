"""
Nexus SDK for Python — unified client for Nexus OAuth Gateway.

Supports both synchronous and MCP server (async) workflows.
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
