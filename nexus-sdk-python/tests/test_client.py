"""Unit tests for the Nexus Python SDK."""

import json
import time
from http.server import HTTPServer, BaseHTTPRequestHandler
import threading
import unittest

from nexus_sdk import NexusClient, NexusClientOptions, TokenCache, RequestConnectionInput
from nexus_sdk.types import CachedToken, NexusError


class MockHandler(BaseHTTPRequestHandler):
    """Mock HTTP handler for testing."""

    routes: dict = {}

    def do_GET(self):
        path = self.path.split("?")[0]
        handler = self.routes.get(("GET", path))
        if handler:
            handler(self)
        else:
            self.send_error(404)

    def do_POST(self):
        path = self.path.split("?")[0]
        handler = self.routes.get(("POST", path))
        if handler:
            handler(self)
        else:
            self.send_error(404)

    def log_message(self, *args):
        pass  # Suppress logs during tests


def _start_mock_server(routes: dict) -> tuple[HTTPServer, str]:
    """Start a mock HTTP server and return (server, base_url)."""
    MockHandler.routes = routes
    server = HTTPServer(("127.0.0.1", 0), MockHandler)
    port = server.server_address[1]
    thread = threading.Thread(target=server.serve_forever, daemon=True)
    thread.start()
    return server, f"http://127.0.0.1:{port}"


class TestTokenCache(unittest.TestCase):
    def test_empty_cache_returns_none(self):
        cache = TokenCache()
        self.assertIsNone(cache.get("ws", "gh"))

    def test_set_and_get(self):
        cache = TokenCache()
        token = CachedToken("tok1", "Bearer", time.time() + 600)
        cache.set("ws", "gh", token)
        got = cache.get("ws", "gh")
        self.assertIsNotNone(got)
        self.assertEqual(got.access_token, "tok1")

    def test_expired_token_returns_none(self):
        cache = TokenCache()
        token = CachedToken("old", "Bearer", time.time() - 60)
        cache.set("ws", "expired", token)
        self.assertIsNone(cache.get("ws", "expired"))

    def test_delete(self):
        cache = TokenCache()
        token = CachedToken("tok1", "Bearer", time.time() + 600)
        cache.set("ws", "gh", token)
        cache.delete("ws", "gh")
        self.assertIsNone(cache.get("ws", "gh"))


class TestRequestConnection(unittest.TestCase):
    def test_request_connection(self):
        def handler(h):
            h.send_response(200)
            h.send_header("Content-Type", "application/json")
            h.end_headers()
            h.wfile.write(json.dumps({
                "authUrl": "https://example.com/auth",
                "connection_id": "abc-123",
            }).encode())

        server, base_url = _start_mock_server({("POST", "/v1/request-connection"): handler})
        try:
            client = NexusClient(NexusClientOptions(gateway_url=base_url))
            resp = client.request_connection(RequestConnectionInput(
                user_id="ws-001", provider_name="github",
                scopes=["repo"], return_url="http://localhost",
            ))
            self.assertEqual(resp.connection_id, "abc-123")
            self.assertEqual(resp.auth_url, "https://example.com/auth")
        finally:
            server.shutdown()


class TestCheckConnection(unittest.TestCase):
    def test_check_connection(self):
        def handler(h):
            h.send_response(200)
            h.send_header("Content-Type", "application/json")
            h.end_headers()
            h.wfile.write(json.dumps({"status": "active"}).encode())

        server, base_url = _start_mock_server({("GET", "/v1/check-connection/abc"): handler})
        try:
            client = NexusClient(NexusClientOptions(gateway_url=base_url))
            status = client.check_connection("abc")
            self.assertEqual(status, "active")
        finally:
            server.shutdown()


class TestResolveToken(unittest.TestCase):
    def test_resolve_token(self):
        def handler(h):
            h.send_response(200)
            h.send_header("Content-Type", "application/json")
            h.end_headers()
            h.wfile.write(json.dumps({
                "access_token": "gho_abc123",
                "token_type": "bearer",
                "credentials": {
                    "access_token": "gho_abc123",
                    "token_type": "bearer",
                    "expires_at": "2026-12-31T23:59:59Z",
                },
            }).encode())

        server, base_url = _start_mock_server({("GET", "/v1/resolve"): handler})
        try:
            client = NexusClient(NexusClientOptions(gateway_url=base_url))
            token = client.resolve_token("ws-001", "github")
            self.assertEqual(token.access_token, "gho_abc123")
            self.assertEqual(token.token_type, "bearer")
        finally:
            server.shutdown()

    def test_resolve_missing_params(self):
        client = NexusClient(NexusClientOptions(gateway_url="http://localhost"))
        with self.assertRaises(NexusError):
            client.resolve_token("", "github")


class TestGetCachedToken(unittest.TestCase):
    def test_cache_miss_then_hit(self):
        call_count = 0

        def handler(h):
            nonlocal call_count
            call_count += 1
            h.send_response(200)
            h.send_header("Content-Type", "application/json")
            h.end_headers()
            h.wfile.write(json.dumps({
                "access_token": "fresh-token",
                "token_type": "Bearer",
                "credentials": {
                    "access_token": "fresh-token",
                    "token_type": "Bearer",
                    "expires_at": "2026-12-31T23:59:59Z",
                },
            }).encode())

        server, base_url = _start_mock_server({("GET", "/v1/resolve"): handler})
        try:
            client = NexusClient(NexusClientOptions(gateway_url=base_url))
            cache = TokenCache()

            # First call — cache miss, hits server
            t1 = client.get_cached_token(cache, "ws", "gh")
            self.assertEqual(t1.access_token, "fresh-token")
            self.assertEqual(call_count, 1)

            # Second call — cache hit, no server call
            t2 = client.get_cached_token(cache, "ws", "gh")
            self.assertEqual(t2.access_token, "fresh-token")
            self.assertEqual(call_count, 1)  # Still 1
        finally:
            server.shutdown()


class TestAuthenticatedFetch(unittest.TestCase):
    def test_injects_authorization_header(self):
        received_auth = None

        def resolve_handler(h):
            h.send_response(200)
            h.send_header("Content-Type", "application/json")
            h.end_headers()
            h.wfile.write(json.dumps({
                "access_token": "injected-token",
                "token_type": "bearer",
                "credentials": {
                    "access_token": "injected-token",
                    "token_type": "bearer",
                    "expires_at": "2026-12-31T23:59:59Z",
                },
            }).encode())

        def upstream_handler(h):
            nonlocal received_auth
            received_auth = h.headers.get("Authorization")
            h.send_response(200)
            h.send_header("Content-Type", "application/json")
            h.end_headers()
            h.wfile.write(json.dumps({"login": "testuser"}).encode())

        # Use a single server with both routes to avoid the shared routes issue
        server, base_url = _start_mock_server({
            ("GET", "/v1/resolve"): resolve_handler,
            ("GET", "/user"): upstream_handler,
        })
        try:
            client = NexusClient(NexusClientOptions(gateway_url=base_url))
            cache = TokenCache()

            status, _, body = client.authenticated_fetch(
                cache, "ws-001", "github", f"{base_url}/user",
            )

            self.assertEqual(status, 200)
            self.assertEqual(received_auth, "Bearer injected-token")
            data = json.loads(body)
            self.assertEqual(data["login"], "testuser")
        finally:
            server.shutdown()


if __name__ == "__main__":
    unittest.main()
