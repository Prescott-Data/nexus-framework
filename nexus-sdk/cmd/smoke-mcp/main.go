// Integration smoke test for the Go SDK's MCP functionality.
// Validates ResolveToken, GetCachedToken, and AuthenticatedHTTPClient
// against the live Azure Nexus Gateway with real OAuth connections.
//
// Usage:
//   NEXUS_GATEWAY_URL=https://dromos-oauth-gateway... go run .
package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"time"

	oauthsdk "github.com/Prescott-Data/nexus-framework/nexus-sdk"
)

// safePrefix returns at most n characters from s, avoiding out-of-bounds panics.
func safePrefix(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n]
}

func main() {
	gatewayURL := os.Getenv("NEXUS_GATEWAY_URL")
	if gatewayURL == "" {
		fmt.Fprintln(os.Stderr, "error: NEXUS_GATEWAY_URL environment variable is required")
		fmt.Fprintln(os.Stderr, "usage: NEXUS_GATEWAY_URL=https://your-gateway.example.com go run .")
		os.Exit(1)
	}
	workspace := "test-workspace-001"

	fmt.Println("╔══════════════════════════════════════════════════════╗")
	fmt.Println("║       Nexus Go SDK — MCP Integration Test           ║")
	fmt.Println("╚══════════════════════════════════════════════════════╝")
	fmt.Printf("\n  Gateway:   %s\n  Workspace: %s\n\n", gatewayURL, workspace)

	client := oauthsdk.New(gatewayURL)
	cache := oauthsdk.NewTokenCache(30 * time.Second)
	ctx := context.Background()

	passed := 0
	failed := 0

	// ── Test 1: ResolveToken (GitHub) ──
	fmt.Print("  1. ResolveToken (github)... ")
	tok, err := client.ResolveToken(ctx, workspace, "github")
	if err != nil {
		fmt.Printf("❌ %v\n", err)
		failed++
	} else {
		fmt.Printf("✅ token=%s... type=%s\n", safePrefix(tok.AccessToken, 10), tok.TokenType)
		passed++
	}

	// ── Test 2: ResolveToken (google) ──
	fmt.Print("  2. ResolveToken (google)... ")
	tok2, err := client.ResolveToken(ctx, workspace, "google")
	if err != nil {
		fmt.Printf("❌ %v\n", err)
		failed++
	} else {
		fmt.Printf("✅ token=%s... type=%s\n", safePrefix(tok2.AccessToken, 10), tok2.TokenType)
		passed++
	}

	// ── Test 3: GetCachedToken (cache miss → hit) ──
	fmt.Print("  3. GetCachedToken (notion, cache miss then hit)... ")
	t1, err := client.GetCachedToken(ctx, cache, workspace, "notion")
	if err != nil {
		fmt.Printf("❌ %v\n", err)
		failed++
	} else {
		t2, _ := client.GetCachedToken(ctx, cache, workspace, "notion")
		if t1.AccessToken == t2.AccessToken {
			fmt.Printf("✅ cached correctly\n")
			passed++
		} else {
			fmt.Printf("❌ cache returned different tokens\n")
			failed++
		}
	}

	// ── Test 4: AuthenticatedHTTPClient → GitHub API ──
	fmt.Print("  4. AuthenticatedHTTPClient → GitHub /user... ")
	ghClient := client.AuthenticatedHTTPClient(cache, workspace, "github")
	resp, err := ghClient.Get("https://api.github.com/user")
	if err != nil {
		fmt.Printf("❌ %v\n", err)
		failed++
	} else {
		defer resp.Body.Close()
		body, _ := io.ReadAll(resp.Body)
		var user map[string]interface{}
		_ = json.Unmarshal(body, &user)
		if login, ok := user["login"].(string); ok {
			fmt.Printf("✅ user: %s\n", login)
			passed++
		} else {
			fmt.Printf("❌ unexpected response: %s\n", safePrefix(string(body), 80))
			failed++
		}
	}

	// ── Test 5: AuthenticatedHTTPClient → Google userinfo ──
	fmt.Print("  5. AuthenticatedHTTPClient → Google userinfo... ")
	googleClient := client.AuthenticatedHTTPClient(cache, workspace, "google")
	resp2, err := googleClient.Get("https://www.googleapis.com/oauth2/v3/userinfo")
	if err != nil {
		fmt.Printf("❌ %v\n", err)
		failed++
	} else {
		defer resp2.Body.Close()
		body2, _ := io.ReadAll(resp2.Body)
		var info map[string]interface{}
		_ = json.Unmarshal(body2, &info)
		if email, ok := info["email"].(string); ok {
			fmt.Printf("✅ user: %s\n", email)
			passed++
		} else {
			fmt.Printf("❌ unexpected response: %s\n", safePrefix(string(body2), 80))
			failed++
		}
	}

	// ── Summary ──
	fmt.Printf("\n  Results: %d passed, %d failed, %d total\n", passed, failed, passed+failed)
	if failed > 0 {
		fmt.Println("\n  ⚠️  Some tests failed.")
		os.Exit(1)
	}
	fmt.Println("\n  All tests passed! 🎉")
}
