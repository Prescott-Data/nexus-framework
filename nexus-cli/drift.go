package main

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
)

// computeDrift computes the difference between a desired Provider manifest
// and the live state returned by the Broker. It returns a boolean indicating
// if there is drift, a map of the fields that need to be patched, and an error
// if serialization fails.
func computeDrift(desired Provider, live map[string]interface{}) (bool, map[string]interface{}, error) {
	updates := make(map[string]interface{})
	drifted := false

	// Marshal and unmarshal desired to get a map reflecting exactly what
	// would be sent as JSON (respecting json tags and omitempty).
	b, err := json.Marshal(desired)
	if err != nil {
		return false, nil, fmt.Errorf("failed to marshal desired provider: %w", err)
	}
	var desiredMap map[string]interface{}
	if err := json.Unmarshal(b, &desiredMap); err != nil {
		return false, nil, fmt.Errorf("failed to unmarshal desired provider: %w", err)
	}

	for k, v := range desiredMap {
		if k == "name" || k == "id" || k == "created_at" || k == "updated_at" {
			continue
		}

		liveVal, exists := live[k]

		// The Broker never returns client_secret. It returns a fingerprint of
		// it (0.6.4+), so a changed secret is detected by comparing
		// fingerprints without the secret ever being read back. A row with no
		// fingerprint yet (written before 0.6.4 and not resolved since) cannot
		// be compared and is left alone; re-sending would re-version every
		// such provider's secret on every apply.
		if k == "client_secret" {
			desired, _ := v.(string)
			liveFP, hasFP := live["client_secret_fingerprint"].(string)
			if !hasFP || liveFP == "" {
				continue
			}
			if secretFingerprint(desired) != liveFP {
				updates[k] = v
				drifted = true
			}
			continue
		}

		// To handle subtle type differences from JSON unmarshaling (e.g., float64 vs int),
		// we marshal both values and compare their JSON string representations.
		vb, _ := json.Marshal(v)
		lvb, _ := json.Marshal(liveVal)

		if !exists || string(vb) != string(lvb) {
			// Special case: if desired is empty array/slice and live is null/missing, don't consider it drift
			if string(vb) == "[]" && (string(lvb) == "null" || string(lvb) == "") {
				continue
			}

			// Special case: if desired is an empty string and live is missing/null
			if string(vb) == `""` && (string(lvb) == "null" || string(lvb) == "") {
				continue
			}

			updates[k] = v
			drifted = true
		}
	}

	return drifted, updates, nil
}

// secretFingerprint matches the Broker's provider.SecretFingerprint: the
// first 16 hex characters of the SHA-256 of the secret.
func secretFingerprint(secret string) string {
	sum := sha256.Sum256([]byte(secret))
	return hex.EncodeToString(sum[:])[:16]
}
