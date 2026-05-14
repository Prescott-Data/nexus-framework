"""Thread-safe, TTL-aware in-memory token cache."""

from __future__ import annotations

import threading
import time

from nexus_sdk.types import CachedToken


class TokenCache:
    """
    Thread-safe in-memory cache for resolved tokens.

    Keyed by workspace+provider pairs. Automatically evicts entries
    that are within the safety buffer of their expiration time.

    Args:
        buffer_seconds: Safety margin (in seconds) before the actual expiry
            time. The cache will treat the token as expired this many seconds
            early, forcing a re-fetch before the upstream token actually expires.
    """

    def __init__(self, buffer_seconds: float = 30.0):
        self._entries: dict[str, CachedToken] = {}
        self._lock = threading.RLock()
        self._buffer = buffer_seconds

    @staticmethod
    def _key(workspace_id: str, provider: str) -> str:
        return f"{workspace_id}:{provider}"

    def get(self, workspace_id: str, provider: str) -> CachedToken | None:
        """Return a cached token if it exists and is not expired."""
        key = self._key(workspace_id, provider)
        with self._lock:
            token = self._entries.get(key)
            if token is None:
                return None
            if time.time() > (token.expires_at - self._buffer):
                # Expired or within safety buffer — evict
                del self._entries[key]
                return None
            return token

    def set(self, workspace_id: str, provider: str, token: CachedToken) -> None:
        """Store a token in the cache."""
        key = self._key(workspace_id, provider)
        with self._lock:
            self._entries[key] = token

    def delete(self, workspace_id: str, provider: str) -> None:
        """Remove a cached token."""
        key = self._key(workspace_id, provider)
        with self._lock:
            self._entries.pop(key, None)
