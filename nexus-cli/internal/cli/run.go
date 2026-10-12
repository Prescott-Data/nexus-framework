package cli

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"net/http"
	"os"
	"sort"
	"strings"
	"sync"
	"time"
)

// httpClient is shared across all requests with a conservative timeout.
var httpClient = &http.Client{Timeout: 30 * time.Second}

// userAgent identifies CLI-driven changes in the Broker's audit log
// (user_agent column), as distinct from manual API calls.
func userAgent() string { return "nexus-cli/" + Version }

// brokerRequest builds a request to the Broker with the headers every call
// carries: the API key (matching the Broker's ApiKeyMiddleware), the CLI
// User-Agent, and a JSON content type when there is a body.
func brokerRequest(method, url string, body []byte, apiKey string) (*http.Request, error) {
	var reader io.Reader
	if body != nil {
		reader = bytes.NewReader(body)
	}
	req, err := http.NewRequest(method, url, reader)
	if err != nil {
		return nil, err
	}
	if apiKey != "" {
		req.Header.Set("X-API-Key", apiKey)
	}
	req.Header.Set("User-Agent", userAgent())
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	return req, nil
}

// runApply is plan (isPlanOnly) and apply: read and resolve the manifest,
// fetch the live providers, print the plan, and for apply, execute it.
func runApply(args []string, isPlanOnly bool, stdin io.Reader, stdout, stderr io.Writer) int {
	name := "apply"
	if isPlanOnly {
		name = "plan"
	}
	fs := flag.NewFlagSet(name, flag.ContinueOnError)
	var mf manifestFlags
	addManifestFlags(fs, &mf)
	pruneFlag := fs.Bool("prune", false, "Delete providers not in the manifest")
	var autoApprove *bool
	if !isPlanOnly {
		autoApprove = fs.Bool("auto-approve", false, "Skip the interactive confirmation (for scripted applies from a trusted environment; not for apply-on-merge pipelines)")
	}
	if ok, code := parseFlags(fs, args, stderr); !ok {
		return code
	}

	brokerURL := os.Getenv("BROKER_BASE_URL")
	if brokerURL == "" {
		brokerURL = "http://localhost:8080"
	}
	apiKey := os.Getenv("API_KEY")

	// Read the manifest once with placeholders kept, to learn which
	// placeholders it needs; resolve exactly those; read it again expanded.
	// Parsing twice is cheap and keeps the remote source to one round trip.
	shape, err := readManifest(mf.file, keepPlaceholders)
	if err != nil {
		fmt.Fprintf(stderr, "✗ %v\n", err)
		return 1
	}
	src, err := newSecretSource(mf.secrets, mf.secretPrefix)
	if err != nil {
		fmt.Fprintf(stderr, "✗ %v\n", err)
		return 1
	}
	lookup, err := lookupWith(context.Background(), src, shape.Placeholders)
	if err != nil {
		fmt.Fprintf(stderr, "✗ %v\n", err)
		return 1
	}
	l, err := readManifest(mf.file, lookup)
	if err != nil {
		fmt.Fprintf(stderr, "✗ %v\n", err)
		return 1
	}
	if len(l.Missing) > 0 {
		fmt.Fprintf(stderr, "✗ %s\n", unresolvedMessage(src, l.Missing))
		return 1
	}
	manifest := l.Manifest

	if issues := validateManifest(l); len(issues) > 0 {
		for _, i := range issues {
			fmt.Fprintf(stderr, "✗ %s\n", i)
		}
		fmt.Fprintf(stderr, "✗ Manifest is invalid; the Broker would reject it. Fix the %d problem(s) above (nexus validate).\n", len(issues))
		return 1
	}

	fmt.Fprintln(stdout, readSummary(l, mf.file))

	// Fetch current live state
	req, err := brokerRequest("GET", brokerURL+"/providers", nil, apiKey)
	if err != nil {
		fmt.Fprintf(stderr, "✗ Failed to create request: %v\n", err)
		return 1
	}

	resp, err := httpClient.Do(req)
	if err != nil {
		fmt.Fprintf(stderr, "✗ Failed to fetch live providers: %v\n", err)
		return 1
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		fmt.Fprintf(stderr, "✗ Failed to fetch live providers, status: %d, body: %s\n", resp.StatusCode, string(body))
		return 1
	}

	var liveProviders []map[string]interface{}
	if err := json.NewDecoder(resp.Body).Decode(&liveProviders); err != nil {
		fmt.Fprintf(stderr, "✗ Failed to decode live providers: %v\n", err)
		return 1
	}

	// Build live provider map: name → full config.
	// Fetch full profiles concurrently with a bounded worker pool to avoid N+1 latency.

	const maxConcurrency = 5
	sem := make(chan struct{}, maxConcurrency)
	var mu sync.Mutex
	var wg sync.WaitGroup
	var fetchErr error

	liveProviderMap := make(map[string]map[string]interface{})

	for _, lp := range liveProviders {
		name, nameOk := lp["name"].(string)
		id, idOk := lp["id"].(string)
		if !nameOk || !idOk {
			continue
		}

		wg.Add(1)
		go func(name, id string) {
			defer wg.Done()
			sem <- struct{}{}        // acquire
			defer func() { <-sem }() // release

			reqProfile, err := brokerRequest("GET", brokerURL+"/providers/"+id, nil, apiKey)
			if err != nil {
				mu.Lock()
				fetchErr = fmt.Errorf("failed to create request for provider %s: %w", name, err)
				mu.Unlock()
				return
			}

			respProfile, err := httpClient.Do(reqProfile)
			if err != nil {
				mu.Lock()
				fetchErr = fmt.Errorf("failed to fetch profile for provider %s: %w", name, err)
				mu.Unlock()
				return
			}
			defer respProfile.Body.Close()

			if respProfile.StatusCode != http.StatusOK {
				body, _ := io.ReadAll(respProfile.Body)
				mu.Lock()
				fetchErr = fmt.Errorf("failed to fetch profile for %s, status: %d, body: %s", name, respProfile.StatusCode, string(body))
				mu.Unlock()
				return
			}

			var fullProfile map[string]interface{}
			profileDec := json.NewDecoder(respProfile.Body)
			profileDec.UseNumber() // compare numbers by their text, like the manifest side
			if err := profileDec.Decode(&fullProfile); err != nil {
				mu.Lock()
				fetchErr = fmt.Errorf("failed to decode profile for %s: %w", name, err)
				mu.Unlock()
				return
			}

			mu.Lock()
			liveProviderMap[name] = fullProfile
			mu.Unlock()
		}(name, id)
	}

	wg.Wait()
	if fetchErr != nil {
		fmt.Fprintf(stderr, "✗ Error fetching provider profiles: %v\n", fetchErr)
		return 1
	}

	manifestProviderMap := make(map[string]Provider)
	for _, p := range manifest.Providers {
		manifestProviderMap[p.Name] = p
	}

	fmt.Fprintln(stdout, "\n--- Execution Plan ---")

	toCreate := []Provider{}
	toUpdate := make(map[string]map[string]interface{}) // map ID to updates
	toUpdateNames := make(map[string]string)            // map ID to Name for logging
	toDelete := []string{}                              // list of IDs
	toDeleteNames := []string{}

	for i, p := range manifest.Providers {
		if live, exists := liveProviderMap[p.Name]; exists {
			id, ok := live["id"].(string)
			if !ok {
				fmt.Fprintf(stderr, "✗ Provider %s has invalid or missing 'id' in live state\n", p.Name)
				return 1
			}
			drifted, updates, err := computeDrift(p, live)
			if err != nil {
				fmt.Fprintf(stderr, "✗ Failed to compute drift for provider %s: %v\n", p.Name, err)
				return 1
			}
			if drifted {
				toUpdate[id] = updates
				toUpdateNames[id] = p.Name
				fmt.Fprintf(stdout, "~ UPDATE : %s\n", p.Name)
				// Sort keys for deterministic plan output
				fields := make([]string, 0, len(updates))
				for field := range updates {
					fields = append(fields, field)
				}
				sort.Strings(fields)
				for _, field := range fields {
					newVal := updates[field]
					oldVal := live[field]
					if isSecretField(field) || (i < len(l.Sensitive) && l.Sensitive[i][field]) {
						fmt.Fprintf(stdout, "    %s: *** → ***\n", field)
					} else {
						fmt.Fprintf(stdout, "    %s: %v → %v\n", field, formatVal(oldVal), formatVal(newVal))
					}
				}
			} else {
				fmt.Fprintf(stdout, "= OK     : %s (no changes)\n", p.Name)
			}
		} else {
			toCreate = append(toCreate, p)
			fmt.Fprintf(stdout, "+ CREATE : %s\n", p.Name)
		}
	}

	// Orphans in name order, so two runs print the same plan.
	liveNames := make([]string, 0, len(liveProviderMap))
	for name := range liveProviderMap {
		liveNames = append(liveNames, name)
	}
	sort.Strings(liveNames)
	for _, name := range liveNames {
		live := liveProviderMap[name]
		if _, exists := manifestProviderMap[name]; !exists {
			if *pruneFlag {
				id, ok := live["id"].(string)
				if !ok {
					fmt.Fprintf(stderr, "✗ Provider %s has invalid or missing 'id' in live state\n", name)
					return 1
				}
				toDelete = append(toDelete, id)
				toDeleteNames = append(toDeleteNames, name)
				fmt.Fprintf(stdout, "- DELETE : %s\n", name)
			} else {
				fmt.Fprintf(stdout, "! ORPHAN : %s (would be deleted if -prune was passed)\n", name)
			}
		}
	}

	if len(toCreate) == 0 && len(toUpdate) == 0 && len(toDelete) == 0 {
		fmt.Fprintln(stdout, "\nNo changes required. Infrastructure matches configuration.")
		return 0
	}

	if isPlanOnly {
		fmt.Fprintln(stdout, "\nPlan complete. Run 'nexus apply' to perform these actions.")
		return 0
	}

	if !*autoApprove {
		fmt.Fprint(stdout, "\nDo you want to perform these actions?\n  Nexus will perform the actions described above.\n  Only 'yes' will be accepted to approve.\n\n  Enter a value: ")

		confirmation, err := bufio.NewReader(stdin).ReadString('\n')
		if err != nil && confirmation == "" {
			fmt.Fprintf(stderr, "\n✗ Failed to read input: %v\n", err)
			return 1
		}

		if strings.TrimSpace(confirmation) != "yes" {
			fmt.Fprintln(stdout, "\nApply cancelled.")
			return 0
		}
	} else {
		fmt.Fprintln(stdout, "\n-auto-approve set; applying without confirmation.")
	}

	fmt.Fprintln(stdout, "\n--- Applying Changes ---")

	var failed []string // "name: reason", for the summary at the end
	fail := func(name, format string, a ...interface{}) {
		reason := fmt.Sprintf(format, a...)
		fmt.Fprintln(stdout, reason)
		failed = append(failed, name+": "+reason)
	}

	for _, p := range toCreate {
		fmt.Fprintf(stdout, "Creating %s... ", p.Name)

		payload := map[string]interface{}{
			"profile": p,
		}

		jsonData, err := json.Marshal(payload)
		if err != nil {
			fail(p.Name, "Failed to marshal: %v", err)
			continue
		}

		req, err := brokerRequest("POST", brokerURL+"/providers", jsonData, apiKey)
		if err != nil {
			fail(p.Name, "Failed to create request: %v", err)
			continue
		}

		resp, err := httpClient.Do(req)
		if err != nil {
			fail(p.Name, "Request failed: %v", err)
			continue
		}

		if resp.StatusCode == http.StatusCreated || resp.StatusCode == http.StatusOK {
			resp.Body.Close()
			fmt.Fprintln(stdout, "OK")
		} else {
			errBody, _ := io.ReadAll(resp.Body)
			resp.Body.Close()
			fail(p.Name, "FAILED (Status %d): %s", resp.StatusCode, strings.TrimSpace(string(errBody)))
		}
	}

	// Updates in name order, like the plan.
	updateIDs := make([]string, 0, len(toUpdate))
	for id := range toUpdate {
		updateIDs = append(updateIDs, id)
	}
	sort.Slice(updateIDs, func(i, j int) bool { return toUpdateNames[updateIDs[i]] < toUpdateNames[updateIDs[j]] })
	for _, id := range updateIDs {
		updates := toUpdate[id]
		name := toUpdateNames[id]
		fmt.Fprintf(stdout, "Updating %s... ", name)

		jsonData, err := json.Marshal(updates)
		if err != nil {
			fail(name, "Failed to marshal: %v", err)
			continue
		}

		req, err := brokerRequest("PATCH", brokerURL+"/providers/"+id, jsonData, apiKey)
		if err != nil {
			fail(name, "Failed to create request: %v", err)
			continue
		}

		resp, err := httpClient.Do(req)
		if err != nil {
			fail(name, "Request failed: %v", err)
			continue
		}

		if resp.StatusCode == http.StatusOK {
			resp.Body.Close()
			fmt.Fprintln(stdout, "OK")
		} else {
			errBody, _ := io.ReadAll(resp.Body)
			resp.Body.Close()
			fail(name, "FAILED (Status %d): %s", resp.StatusCode, strings.TrimSpace(string(errBody)))
		}
	}

	for i, id := range toDelete {
		name := toDeleteNames[i]
		fmt.Fprintf(stdout, "Deleting %s... ", name)

		req, err := brokerRequest("DELETE", brokerURL+"/providers/"+id, nil, apiKey)
		if err != nil {
			fail(name, "Failed to create request: %v", err)
			continue
		}

		resp, err := httpClient.Do(req)
		if err != nil {
			fail(name, "Request failed: %v", err)
			continue
		}

		if resp.StatusCode == http.StatusOK {
			resp.Body.Close()
			fmt.Fprintln(stdout, "OK")
		} else {
			errBody, _ := io.ReadAll(resp.Body)
			resp.Body.Close()
			fail(name, "FAILED (Status %d): %s", resp.StatusCode, strings.TrimSpace(string(errBody)))
		}
	}

	if len(failed) > 0 {
		fmt.Fprintf(stdout, "\nApply completed with %d error(s):\n", len(failed))
		for _, f := range failed {
			fmt.Fprintf(stdout, "  ✗ %s\n", f)
		}
		return 1
	}
	fmt.Fprintln(stdout, "\nApply complete.")
	return 0
}

// readSummary is the first line of plan, apply and validate output.
func readSummary(l loaded, path string) string {
	if l.Directory {
		return fmt.Sprintf("Read %d providers from %s (%d files)", len(l.Manifest.Providers), path, l.Files)
	}
	return fmt.Sprintf("Read %d providers from %s", len(l.Manifest.Providers), path)
}

// isSecretField returns true for fields that are always masked in plan
// output. A field that held a placeholder in the manifest is masked as well
// (loaded.Sensitive), whatever its name.
func isSecretField(field string) bool {
	switch field {
	case "client_secret", "client_id":
		return true
	}
	return false
}

// formatVal returns a human-readable string for a plan diff value.
func formatVal(v interface{}) string {
	if v == nil {
		return "<nil>"
	}
	switch val := v.(type) {
	case string:
		if val == "" {
			return `""`
		}
		return val
	case []interface{}:
		parts := make([]string, len(val))
		for i, item := range val {
			parts[i] = fmt.Sprintf("%v", item)
		}
		return "[" + strings.Join(parts, ", ") + "]"
	default:
		return fmt.Sprintf("%v", v)
	}
}
