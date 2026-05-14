"""Type definitions for the Nexus Python SDK."""

from __future__ import annotations

from dataclasses import dataclass, field
from typing import Any, Optional


@dataclass
class RetryPolicy:
    """Configures retry behavior for HTTP requests."""

    retries: int = 0
    """Total retry attempts (excluding the initial request)."""

    min_delay: float = 0.2
    """Base delay in seconds for exponential backoff."""

    max_delay: float = 2.0
    """Maximum delay cap in seconds."""

    retry_on_429: bool = False
    """Whether to also retry on HTTP 429 (Too Many Requests)."""


@dataclass
class NexusClientOptions:
    """Configuration for the NexusClient."""

    gateway_url: str
    """Base URL of the Nexus Gateway."""

    api_key: str = ""
    """Optional API key for authenticating with the Nexus Gateway."""

    retry_policy: RetryPolicy = field(default_factory=RetryPolicy)
    """Optional retry policy for HTTP requests."""

    timeout: float = 30.0
    """HTTP request timeout in seconds."""


@dataclass
class RequestConnectionInput:
    """Input for initiating an OAuth connection request."""

    user_id: str
    """The workspace/tenant ID requesting the connection."""

    provider_name: str
    """The Nexus provider name (e.g., 'github', 'slack')."""

    scopes: list[str]
    """OAuth scopes to request."""

    return_url: str
    """URL to redirect the user back to after the OAuth flow completes."""

    metadata: Optional[dict[str, Any]] = None
    """Optional arbitrary metadata to attach to the connection request."""


@dataclass
class RequestConnectionResponse:
    """Response from a connection request."""

    auth_url: str
    """The URL to redirect the user to for OAuth consent."""

    connection_id: str
    """Unique ID for the pending connection."""

    state: Optional[str] = None
    """The OAuth state parameter."""

    scopes: Optional[list[str]] = None
    """Scopes granted by the connection."""

    provider_id: Optional[str] = None
    """The internal Nexus provider ID."""


@dataclass
class TokenResponse:
    """Full token response from the broker."""

    access_token: str
    """The OAuth access token."""

    token_type: Optional[str] = None
    """Token type (e.g., 'Bearer')."""

    expires_in: Optional[int] = None
    """Seconds until the token expires."""

    expires_at: Optional[str] = None
    """ISO 8601 timestamp when the token expires."""

    scope: Optional[str] = None
    """Scopes granted (space-separated)."""

    id_token: Optional[str] = None
    """OIDC ID token, if present."""

    refresh_token: Optional[str] = None
    """Refresh token, if present."""

    provider: Optional[str] = None
    """Provider name."""

    strategy: Optional[dict[str, Any]] = None
    """Authentication strategy metadata."""

    credentials: Optional[dict[str, Any]] = None
    """Full credentials payload from the broker."""

    raw: Optional[dict[str, Any]] = None
    """Raw JSON response."""


@dataclass
class CachedToken:
    """A resolved token with its expiration metadata."""

    access_token: str
    """The raw access token."""

    token_type: str
    """Token type (e.g., 'Bearer')."""

    expires_at: float
    """Epoch timestamp (seconds) when the token expires."""


class NexusError(Exception):
    """Structured error from the Nexus Gateway."""

    def __init__(self, code: str, message: str, status_code: Optional[int] = None):
        super().__init__(f"{code}: {message}")
        self.code = code
        self.message = message
        self.status_code = status_code
