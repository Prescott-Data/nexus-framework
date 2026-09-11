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

    def _dispatch(self, method: str):
        # Drain the request body before responding. Closing a socket with
        # unread data in the receive buffer makes the OS send RST instead of
        # FIN, which surfaces client-side as an intermittent
        # ConnectionResetError.
        length = int(self.headers.get("Content-Length") or 0)
        if length:
            self.rfile.read(length)

        path = self.path.split("?")[0]
        handler = self.routes.get((method, path))
        if handler:
            handler(self)
        else:
            self.send_error(404)

    def do_GET(self):
        self._dispatch("GET")

    def do_POST(self):
        self._dispatch("POST")

    def do_DELETE(self):
        self._dispatch("DELETE")

    def log_message(self, *args):
        pass  # Suppress logs during tests


class _MockServer(HTTPServer):
    """Server that fully tears down so tests cannot leak threads or sockets."""

    def close(self):
        self.shutdown()
        self.server_close()
        self._thread.join(timeout=5)


def _start_mock_server(routes: dict) -> tuple[_MockServer, str]:
    """Start a mock HTTP server and return (server, base_url).

    Each server gets its own handler subclass: a shared class-level route table
    would let concurrently-running servers from other tests observe each other's
    routes, which made the suite flaky.
    """
    handler_cls = type("ScopedMockHandler", (MockHandler,), {"routes": routes})
    server = _MockServer(("127.0.0.1", 0), handler_cls)
    port = server.server_address[1]
    thread = threading.Thread(target=server.serve_forever, daemon=True)
    thread.start()
    server._thread = thread
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
            server.close()


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
            server.close()


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
            server.close()

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
            server.close()


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
            server.close()


class TestRevokeConnection(unittest.TestCase):
    def test_revoke_sends_delete_and_parses_result(self):
        captured = {}

        def revoke_handler(h):
            captured["path"] = h.path
            h.send_response(200)
            h.send_header("Content-Type", "application/json")
            h.end_headers()
            h.wfile.write(json.dumps({
                "connection_id": "conn-1",
                "provider_name": "google",
                "status": "revoked",
                "revoked_at": "2026-01-01T00:00:00Z",
                "token_deleted": True,
                "provider_revoked": True,
                "sessions_closed": 2,
            }).encode())

        server, base_url = _start_mock_server({
            ("DELETE", "/v1/connections/conn-1"): revoke_handler,
        })
        try:
            client = NexusClient(NexusClientOptions(gateway_url=base_url))
            result = client.revoke_connection("conn-1", workspace_id="ws-1", reason="offboarding")

            self.assertEqual(result.status, "revoked")
            self.assertTrue(result.token_deleted)
            self.assertTrue(result.provider_revoked)
            self.assertEqual(result.sessions_closed, 2)
            self.assertIn("workspace_id=ws-1", captured["path"])
            self.assertIn("reason=offboarding", captured["path"])
        finally:
            server.close()

    def test_revoke_reports_provider_failure(self):
        """The local credential is destroyed even when upstream revocation fails."""

        def revoke_handler(h):
            h.send_response(200)
            h.send_header("Content-Type", "application/json")
            h.end_headers()
            h.wfile.write(json.dumps({
                "connection_id": "conn-2",
                "status": "revoked",
                "token_deleted": True,
                "provider_revoked": False,
                "provider_revocation_error": "provider does not advertise a revocation endpoint",
            }).encode())

        server, base_url = _start_mock_server({
            ("DELETE", "/v1/connections/conn-2"): revoke_handler,
        })
        try:
            client = NexusClient(NexusClientOptions(gateway_url=base_url))
            result = client.revoke_connection("conn-2")

            self.assertTrue(result.token_deleted)
            self.assertFalse(result.provider_revoked)
            self.assertIn("revocation endpoint", result.provider_revocation_error)
        finally:
            server.close()

    def test_revoke_rejects_empty_connection_id(self):
        client = NexusClient(NexusClientOptions(gateway_url="http://localhost:1"))
        with self.assertRaises(NexusError):
            client.revoke_connection("  ")


if __name__ == "__main__":
    unittest.main()
