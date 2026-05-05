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

	"gopkg.in/yaml.v3"
)

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

func runCommand(isPlanOnly bool) {
	cmdFlags := flag.NewFlagSet(os.Args[1], flag.ExitOnError)
	fileFlag := cmdFlags.String("file", "nexus-providers.yaml", "Path to the providers manifest file")

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
	expandedData := os.ExpandEnv(string(data))

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
	if apiKey != "" {
		req.Header.Set("Authorization", "Bearer "+apiKey)
	}

	client := &http.Client{}
	resp, err := client.Do(req)
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

	liveProviderMap := make(map[string]string) // map name to ID
	for _, lp := range liveProviders {
		name, nameOk := lp["name"].(string)
		id, idOk := lp["id"].(string)
		if nameOk && idOk {
			liveProviderMap[name] = id
		}
	}

	manifestProviderMap := make(map[string]Provider)
	for _, p := range manifest.Providers {
		manifestProviderMap[p.Name] = p
	}

	fmt.Println("\n--- Execution Plan ---")

	toCreate := []Provider{}
	toUpdate := make(map[string]Provider) // map ID to Provider
	toDelete := []string{}                // list of IDs
	toDeleteNames := []string{}

	for _, p := range manifest.Providers {
		if id, exists := liveProviderMap[p.Name]; exists {
			toUpdate[id] = p
			fmt.Printf("~ UPDATE : %s\n", p.Name)
		} else {
			toCreate = append(toCreate, p)
			fmt.Printf("+ CREATE : %s\n", p.Name)
		}
	}

	for name, id := range liveProviderMap {
		if _, exists := manifestProviderMap[name]; !exists {
			toDelete = append(toDelete, id)
			toDeleteNames = append(toDeleteNames, name)
			fmt.Printf("- DELETE : %s\n", name)
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
		if apiKey != "" {
			req.Header.Set("Authorization", "Bearer "+apiKey)
		}
		req.Header.Set("Content-Type", "application/json")

		resp, err := client.Do(req)
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

	for id, p := range toUpdate {
		fmt.Printf("Updating %s... ", p.Name)

		jsonData, err := json.Marshal(p)
		if err != nil {
			fmt.Printf("Failed to marshal: %v\n", err)
			continue
		}

		req, err := http.NewRequest("PUT", brokerURL+"/providers/"+id, bytes.NewBuffer(jsonData))
		if err != nil {
			fmt.Printf("Failed to create request: %v\n", err)
			continue
		}
		if apiKey != "" {
			req.Header.Set("Authorization", "Bearer "+apiKey)
		}
		req.Header.Set("Content-Type", "application/json")

		resp, err := client.Do(req)
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
		if apiKey != "" {
			req.Header.Set("Authorization", "Bearer "+apiKey)
		}

		resp, err := client.Do(req)
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
