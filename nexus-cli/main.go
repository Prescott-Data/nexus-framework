package main

import (
	"bufio"
	"bytes"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"strings"
	"time"

	"gopkg.in/yaml.v3"
)

// httpClient is shared across all requests with a conservative timeout.
var httpClient = &http.Client{Timeout: 30 * time.Second}

type Provider struct {
	Name            string                 `yaml:"name" json:"name"`
	AuthType        string                 `yaml:"auth_type,omitempty" json:"auth_type,omitempty"`
	AuthHeader      string                 `yaml:"auth_header,omitempty" json:"auth_header,omitempty"`
	ClientID        string                 `yaml:"client_id,omitempty" json:"client_id,omitempty"`
	ClientSecret    string                 `yaml:"client_secret,omitempty" json:"client_secret,omitempty"`
	AuthURL         string                 `yaml:"auth_url,omitempty" json:"auth_url,omitempty"`
	TokenURL        string                 `yaml:"token_url,omitempty" json:"token_url,omitempty"`
	Issuer          string                 `yaml:"issuer,omitempty" json:"issuer,omitempty"`
	EnableDiscovery bool                   `yaml:"enable_discovery" json:"enable_discovery"`
	Scopes          []string               `yaml:"scopes" json:"scopes"`
	APIBaseURL      string                 `yaml:"api_base_url,omitempty" json:"api_base_url,omitempty"`
	Params          map[string]interface{} `yaml:"params,omitempty" json:"params,omitempty"`
}

type Manifest struct {
	Providers []Provider `yaml:"providers"`
}

func main() {
	if len(os.Args) < 2 {
		fmt.Println("Usage: nexus-cli <command> [options]")
		fmt.Println("Commands:")
		fmt.Println("  plan     Show execution plan without making changes")
		fmt.Println("  apply    Apply provider configurations from a manifest")
		os.Exit(1)
	}

	command := os.Args[1]

	switch command {
	case "plan":
		runCommand(true)
	case "apply":
		runCommand(false)
	default:
		fmt.Printf("Unknown command: %s\n", command)
		os.Exit(1)
	}
}

// setAPIKey sets the X-API-Key header on a request, matching the Broker's ApiKeyMiddleware.
func setAPIKey(req *http.Request, apiKey string) {
	if apiKey != "" {
		req.Header.Set("X-API-Key", apiKey)
	}
}

func runCommand(isPlanOnly bool) {
	cmdFlags := flag.NewFlagSet(os.Args[1], flag.ExitOnError)
	fileFlag := cmdFlags.String("file", "nexus-providers.yaml", "Path to the providers manifest file")
	pruneFlag := cmdFlags.Bool("prune", false, "Delete providers not in the manifest")

	if err := cmdFlags.Parse(os.Args[2:]); err != nil {
		log.Fatalf("Failed to parse flags: %v", err)
	}

	brokerURL := os.Getenv("BROKER_BASE_URL")
	if brokerURL == "" {
		brokerURL = "http://localhost:8080"
	}
	apiKey := os.Getenv("API_KEY")

	// Read Manifest
	data, err := os.ReadFile(*fileFlag)
	if err != nil {
		log.Fatalf("Failed to read manifest file %s: %v", *fileFlag, err)
	}

	// Expand environment variables
	var missingVars []string
	missingSet := make(map[string]bool)

	expandedData := os.Expand(string(data), func(envVar string) string {
		val, exists := os.LookupEnv(envVar)
		if !exists {
			if !missingSet[envVar] {
				missingSet[envVar] = true
				missingVars = append(missingVars, envVar)
			}
		}
		return val
	})

	if len(missingVars) > 0 {
		log.Fatalf("Failed to process manifest. The following environment variables are unset: %s", strings.Join(missingVars, ", "))
	}

	var manifest Manifest
	if err := yaml.Unmarshal([]byte(expandedData), &manifest); err != nil {
		log.Fatalf("Failed to parse YAML manifest: %v", err)
	}

	fmt.Printf("Read %d providers from %s\n", len(manifest.Providers), *fileFlag)

	// Fetch current live state
	req, err := http.NewRequest("GET", brokerURL+"/providers", nil)
	if err != nil {
		log.Fatalf("Failed to create request: %v", err)
	}
	setAPIKey(req, apiKey)

	resp, err := httpClient.Do(req)
	if err != nil {
		log.Fatalf("Failed to fetch live providers: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		log.Fatalf("Failed to fetch live providers, status: %d, body: %s", resp.StatusCode, string(body))
	}

	var liveProviders []map[string]interface{}
	if err := json.NewDecoder(resp.Body).Decode(&liveProviders); err != nil {
		log.Fatalf("Failed to decode live providers: %v", err)
	}

	// Build live provider map: name → {id, full config}
	liveProviderMap := make(map[string]map[string]interface{})
	for _, lp := range liveProviders {
		name, nameOk := lp["name"].(string)
		id, idOk := lp["id"].(string)
		if nameOk && idOk {
			// Fetch full profile for accurate drift detection
			reqProfile, err := http.NewRequest("GET", brokerURL+"/providers/"+id, nil)
			if err != nil {
				log.Fatalf("Failed to create request for provider %s: %v", name, err)
			}
			setAPIKey(reqProfile, apiKey)

			respProfile, err := httpClient.Do(reqProfile)
			if err != nil {
				log.Fatalf("Failed to fetch profile for provider %s: %v", name, err)
			}

			if respProfile.StatusCode != http.StatusOK {
				body, _ := io.ReadAll(respProfile.Body)
				respProfile.Body.Close()
				log.Fatalf("Failed to fetch profile for %s, status: %d, body: %s", name, respProfile.StatusCode, string(body))
			}

			var fullProfile map[string]interface{}
			if err := json.NewDecoder(respProfile.Body).Decode(&fullProfile); err != nil {
				respProfile.Body.Close()
				log.Fatalf("Failed to decode profile for %s: %v", name, err)
			}
			respProfile.Body.Close()

			liveProviderMap[name] = fullProfile
		}
	}

	manifestProviderMap := make(map[string]Provider)
	for _, p := range manifest.Providers {
		manifestProviderMap[p.Name] = p
	}

	fmt.Println("\n--- Execution Plan ---")

	toCreate := []Provider{}
	toUpdate := make(map[string]map[string]interface{}) // map ID to updates
	toUpdateNames := make(map[string]string)            // map ID to Name for logging
	toDelete := []string{}                              // list of IDs
	toDeleteNames := []string{}

	for _, p := range manifest.Providers {
		if live, exists := liveProviderMap[p.Name]; exists {
			id := live["id"].(string)
			drifted, updates := computeDrift(p, live)
			if drifted {
				toUpdate[id] = updates
				toUpdateNames[id] = p.Name
				fmt.Printf("~ UPDATE : %s\n", p.Name)
			} else {
				fmt.Printf("= OK     : %s (no changes)\n", p.Name)
			}
		} else {
			toCreate = append(toCreate, p)
			fmt.Printf("+ CREATE : %s\n", p.Name)
		}
	}

	for name, live := range liveProviderMap {
		if _, exists := manifestProviderMap[name]; !exists {
			if *pruneFlag {
				id := live["id"].(string)
				toDelete = append(toDelete, id)
				toDeleteNames = append(toDeleteNames, name)
				fmt.Printf("- DELETE : %s\n", name)
			} else {
				fmt.Printf("! ORPHAN : %s (would be deleted if --prune was passed)\n", name)
			}
		}
	}

	if len(toCreate) == 0 && len(toUpdate) == 0 && len(toDelete) == 0 {
		fmt.Println("\nNo changes required. Infrastructure matches configuration.")
		return
	}

	if isPlanOnly {
		fmt.Println("\nPlan complete. Run 'nexus-cli apply' to perform these actions.")
		return
	}

	fmt.Print("\nDo you want to perform these actions?\n  Nexus will perform the actions described above.\n  Only 'yes' will be accepted to approve.\n\n  Enter a value: ")

	reader := bufio.NewReader(os.Stdin)
	confirmation, err := reader.ReadString('\n')
	if err != nil {
		log.Fatalf("Failed to read input: %v", err)
	}

	if strings.TrimSpace(confirmation) != "yes" {
		fmt.Println("\nApply cancelled.")
		return
	}

	fmt.Println("\n--- Applying Changes ---")

	for _, p := range toCreate {
		fmt.Printf("Creating %s... ", p.Name)

		payload := map[string]interface{}{
			"profile": p,
		}

		jsonData, err := json.Marshal(payload)
		if err != nil {
			fmt.Printf("Failed to marshal: %v\n", err)
			continue
		}

		req, err := http.NewRequest("POST", brokerURL+"/providers", bytes.NewBuffer(jsonData))
		if err != nil {
			fmt.Printf("Failed to create request: %v\n", err)
			continue
		}
		setAPIKey(req, apiKey)
		req.Header.Set("Content-Type", "application/json")

		resp, err := httpClient.Do(req)
		if err != nil {
			fmt.Printf("Request failed: %v\n", err)
			continue
		}
		resp.Body.Close()

		if resp.StatusCode == http.StatusCreated || resp.StatusCode == http.StatusOK {
			fmt.Println("OK")
		} else {
			fmt.Printf("FAILED (Status %d)\n", resp.StatusCode)
		}
	}

	for id, updates := range toUpdate {
		name := toUpdateNames[id]
		fmt.Printf("Updating %s... ", name)

		jsonData, err := json.Marshal(updates)
		if err != nil {
			fmt.Printf("Failed to marshal: %v\n", err)
			continue
		}

		req, err := http.NewRequest("PATCH", brokerURL+"/providers/"+id, bytes.NewBuffer(jsonData))
		if err != nil {
			fmt.Printf("Failed to create request: %v\n", err)
			continue
		}
		setAPIKey(req, apiKey)
		req.Header.Set("Content-Type", "application/json")

		resp, err := httpClient.Do(req)
		if err != nil {
			fmt.Printf("Request failed: %v\n", err)
			continue
		}
		resp.Body.Close()

		if resp.StatusCode == http.StatusOK {
			fmt.Println("OK")
		} else {
			fmt.Printf("FAILED (Status %d)\n", resp.StatusCode)
		}
	}

	for i, id := range toDelete {
		name := toDeleteNames[i]
		fmt.Printf("Deleting %s... ", name)

		req, err := http.NewRequest("DELETE", brokerURL+"/providers/"+id, nil)
		if err != nil {
			fmt.Printf("Failed to create request: %v\n", err)
			continue
		}
		setAPIKey(req, apiKey)

		resp, err := httpClient.Do(req)
		if err != nil {
			fmt.Printf("Request failed: %v\n", err)
			continue
		}
		resp.Body.Close()

		if resp.StatusCode == http.StatusOK {
			fmt.Println("OK")
		} else {
			fmt.Printf("FAILED (Status %d)\n", resp.StatusCode)
		}
	}
}

