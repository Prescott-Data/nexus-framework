"""
Nexus SDK Client — unified Python client for the Nexus OAuth Gateway.

Supports all gateway API methods plus MCP-specific token resolution,
caching, and authenticated HTTP session injection.
"""

from __future__ import annotations

import json
import logging
import math
import random
import sys
import time
import urllib.error
import urllib.parse
import urllib.request
from datetime import datetime, timezone
from typing import Any, Optional

from nexus_sdk.token_cache import TokenCache
from nexus_sdk.types import (
    CachedToken,
    NexusClientOptions,
    NexusError,
    RequestConnectionInput,
    RequestConnectionResponse,
    RetryPolicy,
    TokenResponse,
)

# Default logger writes to stderr — safe for MCP stdio transports
logger = logging.getLogger("nexus_sdk")
if not logger.handlers:
    _handler = logging.StreamHandler(sys.stderr)
    _handler.setFormatter(logging.Formatter("[NexusSDK] %(message)s"))
    logger.addHandler(_handler)
    logger.setLevel(logging.INFO)


class NexusClient:
    """
    Unified Python client for the Nexus OAuth Gateway.

    Provides both standard gateway API methods (request_connection,
    check_connection, get_token, refresh_connection, wait_for_active)
    and MCP-specific features (resolve_token, get_cached_token,
    authenticated_session).

    Example (standard app)::

        client = NexusClient(NexusClientOptions(gateway_url="https://gateway.example.com"))
        conn = client.request_connection(RequestConnectionInput(
            user_id="ws-001", provider_name="github",
            scopes=["repo"], return_url="http://localhost/callback",
        ))
        status = client.wait_for_active(conn.connection_id)
        token = client.get_token_by_connection_id(conn.connection_id)

    Example (MCP server)::

        client = NexusClient(NexusClientOptions(gateway_url="https://gateway.example.com"))
        cache = TokenCache()
        session = client.authenticated_session(cache, "ws-001", "github")
        resp = session.get("https://api.github.com/user/repos")
    """

    def __init__(self, options: NexusClientOptions):
        self._gateway_url = options.gateway_url.rstrip("/")
        self._api_key = options.api_key
        self._timeout = options.timeout
        self._retry = options.retry_policy

    # ─────────────────────────────────────────────
    #  Core HTTP helper with retries
    # ─────────────────────────────────────────────

    def _do_request(
        self,
        method: str,
        url: str,
        body: Optional[dict[str, Any]] = None,
    ) -> dict[str, Any]:
        """Execute an HTTP request with retry logic. Returns parsed JSON."""

        def attempt() -> dict[str, Any]:
            headers = {"Accept": "application/json"}
            if self._api_key:
                headers["X-API-Key"] = self._api_key

            data = None
            if body is not None:
                headers["Content-Type"] = "application/json"
                data = json.dumps(body).encode("utf-8")

            req = urllib.request.Request(url, data=data, headers=headers, method=method)

            try:
                with urllib.request.urlopen(req, timeout=self._timeout) as resp:
                    return json.loads(resp.read().decode("utf-8"))
            except urllib.error.HTTPError as e:
                status = e.code
                resp_body = e.read().decode("utf-8", errors="replace")

                # Classify retryable statuses
                retryable = status in (502, 503, 504)
                if self._retry.retry_on_429 and status == 429:
                    retryable = True

                if not retryable:
                    # Parse structured error if possible
                    try:
                        parsed = json.loads(resp_body)
                        code = parsed.get("error", f"http_{status}")
                        message = parsed.get("message", resp_body)
                    except (json.JSONDecodeError, KeyError):
                        code = f"http_{status}"
                        message = resp_body
                    raise NexusError(code, message, status)

                raise _RetryableError(status)

        last_error: Optional[Exception] = None

        for i in range(self._retry.retries + 1):
            if i > 0:
                logger.info("Attempt %d/%d: %s %s", i + 1, self._retry.retries + 1, method, url)

            try:
                return attempt()
            except _RetryableError as e:
                last_error = e
                if i == self._retry.retries:
                    raise NexusError(
                        "max_retries_exceeded",
                        f"Request failed after {self._retry.retries + 1} attempts: {method} {url}",
                        e.status_code,
                    ) from e

                delay = self._backoff(i)
                logger.info("Retrying in %.1fs: %s", delay, last_error)
                time.sleep(delay)
            except NexusError:
                raise
            except Exception as e:
                raise NexusError("request_failed", str(e)) from e

        # Should never reach here
        raise last_error or NexusError("unexpected", "Unexpected retry loop exit")

    def _backoff(self, attempt_index: int) -> float:
        """Exponential backoff with jitter, capped."""
        capped = min(attempt_index, 10)
        factor = 1 << capped  # 2^attempt
        base = self._retry.min_delay * factor
        bounded = min(base, self._retry.max_delay)
        jitter = 0.2 + random.random() * 0.6  # 0.2..0.8
        return bounded * jitter

    # ─────────────────────────────────────────────
    #  Gateway API Methods
    # ─────────────────────────────────────────────

    def request_connection(self, inp: RequestConnectionInput) -> RequestConnectionResponse:
        """
        Initiate an OAuth connection request.

        Wraps POST /v1/request-connection
        """
        payload = {
            "user_id": inp.user_id,
            "provider_name": inp.provider_name,
            "scopes": inp.scopes,
            "return_url": inp.return_url,
        }
        if inp.metadata:
            payload["metadata"] = inp.metadata

        data = self._do_request("POST", f"{self._gateway_url}/v1/request-connection", payload)

        return RequestConnectionResponse(
            auth_url=data["authUrl"],
            connection_id=data["connection_id"],
            state=data.get("state"),
            scopes=data.get("scopes"),
            provider_id=data.get("provider_id"),
        )

    def check_connection(self, connection_id: str) -> str:
        """
        Check the status of a connection.

        Wraps GET /v1/check-connection/{connection_id}

        Returns: Status string (e.g., "active", "pending", "failed").
        """
        if not connection_id.strip():
            raise NexusError("invalid_input", "connection_id must not be empty")

        cid = urllib.parse.quote(connection_id, safe="")
        data = self._do_request("GET", f"{self._gateway_url}/v1/check-connection/{cid}")
        return data["status"]

    def get_token_by_connection_id(self, connection_id: str) -> TokenResponse:
        """
        Retrieve a token by connection ID.

        Wraps GET /v1/token/{connection_id}
        """
        if not connection_id.strip():
            raise NexusError("invalid_input", "connection_id must not be empty")

        cid = urllib.parse.quote(connection_id, safe="")
        raw = self._do_request("GET", f"{self._gateway_url}/v1/token/{cid}")
        return self._parse_token_response(raw)

    def refresh_connection(self, connection_id: str) -> TokenResponse:
        """
        Force a token refresh for a connection.

        Wraps POST /v1/refresh/{connection_id}
        """
        if not connection_id.strip():
            raise NexusError("invalid_input", "connection_id must not be empty")

        cid = urllib.parse.quote(connection_id, safe="")
        raw = self._do_request("POST", f"{self._gateway_url}/v1/refresh/{cid}")
        return self._parse_token_response(raw)

    def wait_for_active(
        self,
        connection_id: str,
        interval: float = 1.5,
        timeout: float = 300.0,
    ) -> str:
        """
        Poll check_connection until the status is "active" or "failed".

        Args:
            connection_id: The connection ID to poll.
            interval: Polling interval in seconds.
            timeout: Maximum time to wait in seconds.

        Returns: Terminal status ("active" or "failed").
        """
        deadline = time.time() + timeout
        while True:
            status = self.check_connection(connection_id)
            if status in ("active", "failed"):
                return status
            if time.time() >= deadline:
                raise NexusError("timeout", f"wait_for_active timed out after {timeout}s")
            time.sleep(interval)

    # ─────────────────────────────────────────────
    #  MCP Token Resolution
    # ─────────────────────────────────────────────

    def resolve_token(self, workspace_id: str, provider: str) -> CachedToken:
        """
        Resolve a token from the gateway using workspace ID + provider name.

        This is the primary method used by MCP servers for dynamic auth.
        Wraps GET /v1/resolve?workspace_id=...&provider_name=...
        """
        logger.info("Resolving token for workspace=%s provider=%s", workspace_id, provider)

        params = urllib.parse.urlencode({
            "workspace_id": workspace_id,
            "provider_name": provider,
        })
        data = self._do_request("GET", f"{self._gateway_url}/v1/resolve?{params}")

        credentials = data.get("credentials", {})
        if not credentials:
            raise NexusError("invalid_response", "Missing credentials in gateway response")

        # Determine auth injection strategy from broker response
        header_name = "Authorization"
        value_prefix = "Bearer "
        token_type = "Bearer"
        strategy = data.get("strategy", {})
        strategy_config = strategy.get("config", {}) if isinstance(strategy, dict) else {}

        if strategy.get("type") == "header" and strategy_config:
            # Non-OAuth strategy: broker specifies which header and prefix to use
            if strategy_config.get("header_name"):
                header_name = str(strategy_config["header_name"])
            vp = strategy_config.get("value_prefix")
            if vp is not None:
                # Explicit prefix from broker — may be empty for raw API keys
                value_prefix = str(vp)
                token_type = value_prefix.strip() or header_name
        elif credentials.get("token_type"):
            token_type = str(credentials["token_type"])
            # Normalize Bearer casing per RFC 6750
            if token_type.lower() == "bearer":
                value_prefix = "Bearer "
            else:
                value_prefix = token_type + " "

        # Extract access token
        access_token = credentials.get("access_token") or data.get("access_token")
        if not access_token and strategy_config.get("credential_field"):
            access_token = credentials.get(strategy_config["credential_field"])

        if not access_token:
            raise NexusError("invalid_credentials", "Could not locate access token in response")

        # Parse expiration — conservative 5-minute default
        expires_at = time.time() + 300  # 5 minutes
        raw_expires_at = credentials.get("expires_at")
        if raw_expires_at is None:
            raw_expires_at = data.get("expires_at")

        if raw_expires_at is not None:
            try:
                if isinstance(raw_expires_at, (int, float)) and not isinstance(raw_expires_at, bool):
                    parsed_expires_at = float(raw_expires_at)
                else:
                    raw_expires_at_str = str(raw_expires_at).strip()
                    if not raw_expires_at_str:
                        raise ValueError("empty expires_at")

                    try:
                        parsed_expires_at = float(raw_expires_at_str)
                    except ValueError:
                        dt = datetime.fromisoformat(raw_expires_at_str.replace("Z", "+00:00"))
                        if dt.tzinfo is None:
                            dt = dt.replace(tzinfo=timezone.utc)
                        parsed_expires_at = dt.timestamp()

                if not math.isfinite(parsed_expires_at):
                    raise ValueError("non-finite expires_at")

                # Heuristic: values larger than 1e12 are almost certainly epoch milliseconds.
                if parsed_expires_at > 1e12:
                    parsed_expires_at /= 1000.0

                expires_at = parsed_expires_at
            except (ValueError, TypeError, OverflowError):
                logger.warning(
                    "Could not parse expires_at=%r for workspace=%s provider=%s, using 5-minute fallback",
                    raw_expires_at, workspace_id, provider,
                )
        else:
            logger.warning(
                "No expires_at for workspace=%s provider=%s, using 5-minute fallback",
                workspace_id, provider,
            )

        return CachedToken(
            access_token=str(access_token),
            token_type=token_type,
            expires_at=expires_at,
            header_name=header_name,
            value_prefix=value_prefix,
        )

    def get_cached_token(
        self,
        cache: TokenCache,
        workspace_id: str,
        provider: str,
    ) -> CachedToken:
        """
        Retrieve a token from cache, or fetch a fresh one via resolve_token.

        This is the high-level method that MCP servers should use.
        """
        cached = cache.get(workspace_id, provider)
        if cached is not None:
            logger.info("Using cached token for workspace=%s provider=%s", workspace_id, provider)
            return cached

        token = self.resolve_token(workspace_id, provider)
        cache.set(workspace_id, provider, token)
        return token

    def clear_token(self, cache: TokenCache, workspace_id: str, provider: str) -> None:
        """Clear a cached token, forcing the next call to fetch fresh."""
        cache.delete(workspace_id, provider)

    # ─────────────────────────────────────────────
    #  Authenticated HTTP (Python equivalent of createFetcher)
    # ─────────────────────────────────────────────

    def authenticated_fetch(
        self,
        cache: TokenCache,
        workspace_id: str,
        provider: str,
        url: str,
        *,
        method: str = "GET",
        headers: Optional[dict[str, str]] = None,
        data: Optional[bytes] = None,
    ) -> tuple[int, dict[str, str], bytes]:
        """
        Make an HTTP request with Nexus auth headers automatically injected.

        Returns a tuple of (status_code, response_headers, response_body).

        This is the zero-dependency equivalent of createFetcher (TypeScript)
        and AuthenticatedHTTPClient (Go).
        """
        token = self.get_cached_token(cache, workspace_id, provider)

        req_headers = dict(headers or {})

        # Use header name and prefix from the resolved strategy.
        # For OAuth2: Authorization: Bearer <token>
        # For API key: X-API-Key: <token>  (value_prefix is empty)
        # For custom:  X-Custom: prefix <token>
        req_headers[token.header_name] = f"{token.value_prefix}{token.access_token}"

        logger.info("Fetcher → %s %s", method, url)

        req = urllib.request.Request(url, data=data, headers=req_headers, method=method)
        try:
            with urllib.request.urlopen(req, timeout=self._timeout) as resp:
                return resp.status, dict(resp.headers), resp.read()
        except urllib.error.HTTPError as e:
            return e.code, dict(e.headers), e.read()

    # ─────────────────────────────────────────────
    #  Internal helpers
    # ─────────────────────────────────────────────

    @staticmethod
    def _parse_token_response(raw: dict[str, Any]) -> TokenResponse:
        creds = raw.get("credentials", {})
        return TokenResponse(
            access_token=raw.get("access_token") or creds.get("access_token", ""),
            token_type=raw.get("token_type") or creds.get("token_type"),
            expires_in=raw.get("expires_in"),
            expires_at=raw.get("expires_at") or creds.get("expires_at"),
            scope=raw.get("scope"),
            id_token=raw.get("id_token") or creds.get("id_token"),
            refresh_token=raw.get("refresh_token") or creds.get("refresh_token"),
            provider=raw.get("provider"),
            strategy=raw.get("strategy"),
            credentials=creds or None,
            raw=raw,
        )


class _RetryableError(Exception):
    """Internal sentinel for the retry loop."""

    def __init__(self, status_code: int):
        super().__init__(f"Retryable HTTP {status_code}")
        self.status_code = status_code
