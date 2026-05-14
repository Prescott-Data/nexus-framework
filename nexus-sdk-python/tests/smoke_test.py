"""
Integration smoke test for the Python SDK against the live Azure Nexus Gateway.

Usage:
    NEXUS_GATEWAY_URL=https://dromos-oauth-gateway... python3 smoke_test.py
"""

import json
import os
import sys

# Add parent dir to path for local import
sys.path.insert(0, os.path.dirname(os.path.dirname(os.path.abspath(__file__))))

from nexus_sdk import NexusClient, NexusClientOptions, TokenCache, RequestConnectionInput

GATEWAY_URL = os.environ.get("NEXUS_GATEWAY_URL", "")
if not GATEWAY_URL:
    print("error: NEXUS_GATEWAY_URL environment variable is required", file=sys.stderr)
    print("usage: NEXUS_GATEWAY_URL=https://your-gateway.example.com python3 smoke_test.py", file=sys.stderr)
    sys.exit(1)
WORKSPACE = "test-workspace-001"


def main():
    print("╔══════════════════════════════════════════════════════╗")
    print("║     Nexus Python SDK — MCP Integration Test         ║")
    print("╚══════════════════════════════════════════════════════╝")
    print(f"\n  Gateway:   {GATEWAY_URL}")
    print(f"  Workspace: {WORKSPACE}\n")

    client = NexusClient(NexusClientOptions(gateway_url=GATEWAY_URL))
    cache = TokenCache()

    passed = 0
    failed = 0

    # 1. resolve_token (GitHub)
    print("  1. resolve_token (github)... ", end="", flush=True)
    try:
        tok = client.resolve_token(WORKSPACE, "github")
        print(f"✅ token={tok.access_token[:10]}... type={tok.token_type}")
        passed += 1
    except Exception as e:
        print(f"❌ {e}")
        failed += 1

    # 2. resolve_token (Google)
    print("  2. resolve_token (google)... ", end="", flush=True)
    try:
        tok2 = client.resolve_token(WORKSPACE, "google")
        print(f"✅ token={tok2.access_token[:10]}... type={tok2.token_type}")
        passed += 1
    except Exception as e:
        print(f"❌ {e}")
        failed += 1

    # 3. get_cached_token (cache miss then hit)
    print("  3. get_cached_token (notion, cache miss→hit)... ", end="", flush=True)
    try:
        t1 = client.get_cached_token(cache, WORKSPACE, "notion")
        t2 = client.get_cached_token(cache, WORKSPACE, "notion")
        if t1.access_token == t2.access_token:
            print("✅ cached correctly")
            passed += 1
        else:
            print("❌ cache returned different tokens")
            failed += 1
    except Exception as e:
        print(f"❌ {e}")
        failed += 1

    # 4. authenticated_fetch → GitHub /user
    print("  4. authenticated_fetch → GitHub /user... ", end="", flush=True)
    try:
        status, _, body = client.authenticated_fetch(
            cache, WORKSPACE, "github",
            "https://api.github.com/user",
            headers={"User-Agent": "NexusPythonSDK/0.2.3"},
        )
        user = json.loads(body)
        if "login" in user:
            print(f"✅ user: {user['login']}")
            passed += 1
        else:
            print(f"❌ status={status}, body={body[:80]}")
            failed += 1
    except Exception as e:
        print(f"❌ {e}")
        failed += 1

    # 5. authenticated_fetch → Google userinfo
    print("  5. authenticated_fetch → Google userinfo... ", end="", flush=True)
    try:
        status, _, body = client.authenticated_fetch(
            cache, WORKSPACE, "google",
            "https://www.googleapis.com/oauth2/v3/userinfo",
        )
        info = json.loads(body)
        if "email" in info:
            print(f"✅ user: {info['email']}")
            passed += 1
        else:
            print(f"❌ status={status}, body={body[:80]}")
            failed += 1
    except Exception as e:
        print(f"❌ {e}")
        failed += 1

    # Summary
    total = passed + failed
    print(f"\n  Results: {passed} passed, {failed} failed, {total} total")
    if failed > 0:
        print("\n  ⚠️  Some tests failed.")
        sys.exit(1)
    print("\n  All tests passed! 🎉")


if __name__ == "__main__":
    main()
