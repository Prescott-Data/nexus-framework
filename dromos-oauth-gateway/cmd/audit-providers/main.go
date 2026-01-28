package main

import (
	"bytes"
	"encoding/csv"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"sort"
	"strings"
	"time"
)

const gatewayURL = "https://dromos-oauth-gateway.bravesea-3f5f7e75.eastus.azurecontainerapps.io"

type ProviderMeta struct {
	ID               string   `json:"id"`
	Scopes           []string `json:"scopes"`
	APIBaseURL       string   `json:"api_base_url"`
	UserInfoEndpoint string   `json:"user_info_endpoint"`
}

type ConnectionRequest struct {
	UserID       string   `json:"user_id"`
	ProviderName string   `json:"provider_name"`
	Scopes       []string `json:"scopes"`
	ReturnURL    string   `json:"return_url"`
}

type ConnectionResponse struct {
	AuthURL string `json:"authUrl"`
}

func main() {
	fmt.Println("Starting OAuth2 Provider Audit...")
	client := &http.Client{Timeout: 10 * time.Second}

	// 1. Fetch List
	resp, err := client.Get(gatewayURL + "/v1/providers")
	if err != nil {
		fatal("Failed to fetch providers: %v", err)
	}
	defer resp.Body.Close()

	var providers map[string]map[string]ProviderMeta
	if err := json.NewDecoder(resp.Body).Decode(&providers); err != nil {
		fatal("Failed to decode providers: %v", err)
	}

	oauthProviders := providers["oauth2"]
	fmt.Printf("Found %d OAuth2 providers. Generating CSV report...\n", len(oauthProviders))

	// 2. Prepare CSV
	file, err := os.Create("providers_oauth2_audit.csv")
	if err != nil {
		fatal("Cannot create file: %v", err)
	}
	defer file.Close()

	writer := csv.NewWriter(file)
	defer writer.Flush()

	// Header
	writer.Write([]string{
		"Provider Name",
		"Status",
		"Scope Count",
		"Registered Scopes",
		"Smoke Test Result",
	})

	// Sort providers by name for cleaner report
	names := make([]string, 0, len(oauthProviders))
	for name := range oauthProviders {
		names = append(names, name)
	}
	sort.Strings(names)

	// 3. Audit Loop
	for _, name := range names {
		if name == "" { continue }
		p := oauthProviders[name]

		status := "PASS"
		testResult := ""
		scopeCount := len(p.Scopes)
		scopesStr := strings.Join(p.Scopes, " ")

		// Check Scopes
		if scopeCount == 0 {
			status = "WARN"
			testResult = "WARNING: No scopes defined in registry."
		}

		// Smoke Test: Request Connection
		// We try to request a connection using the registered scopes (or default "openid" if empty)
		// This verifies the Broker can generate the Auth URL successfully.
		scopeToUse := []string{"openid"}
		if scopeCount > 0 {
			scopeToUse = p.Scopes
		}

		reqBody, _ := json.Marshal(ConnectionRequest{
			UserID:       "audit-bot",
			ProviderName: name,
			Scopes:       scopeToUse,
			ReturnURL:    "https://example.com/callback",
		})

		connResp, err := client.Post(gatewayURL+"/v1/request-connection", "application/json", bytes.NewBuffer(reqBody))
		if err != nil {
			status = "FAIL"
			testResult = fmt.Sprintf("Network Error: %v", err)
		} else {
			defer connResp.Body.Close()
			body, _ := io.ReadAll(connResp.Body)

			if connResp.StatusCode == 200 {
				var res ConnectionResponse
				if err := json.Unmarshal(body, &res); err == nil && res.AuthURL != "" {
					if status != "WARN" {
						testResult = "SUCCESS: Auth URL generated."
					} else {
						testResult += " (Auth URL generated successfully)"
					}
				} else {
					status = "FAIL"
					testResult = "Invalid JSON response from Gateway"
				}
			} else {
				status = "FAIL"
				testResult = fmt.Sprintf("HTTP %d: %s", connResp.StatusCode, string(body))
			}
		}

		// Write row
		writer.Write([]string{
			name,
			status,
			fmt.Sprintf("%d", scopeCount),
			scopesStr,
			testResult,
		})
		
		if status == "FAIL" {
			fmt.Printf("[FAIL] %s: %s\n", name, testResult)
		} else if status == "WARN" {
			fmt.Printf("[WARN] %s: No scopes defined\n", name)
		}
	}

	fmt.Println("\nAudit Complete. Report saved to 'providers_oauth2_audit.csv'")
}

func fatal(format string, args ...interface{}) {
	fmt.Printf(format+"\n", args...)
	os.Exit(1)
}
