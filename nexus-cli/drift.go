package main

import (
	"encoding/json"
)

// computeDrift computes the difference between a desired Provider manifest
// and the live state returned by the Broker. It returns a boolean indicating
// if there is drift, and a map of the fields that need to be patched.
func computeDrift(desired Provider, live map[string]interface{}) (bool, map[string]interface{}) {
	updates := make(map[string]interface{})
	drifted := false

	// Marshal and unmarshal desired to get a map reflecting exactly what
	// would be sent as JSON (respecting json tags and omitempty).
	b, _ := json.Marshal(desired)
	var desiredMap map[string]interface{}
	_ = json.Unmarshal(b, &desiredMap)

	for k, v := range desiredMap {
		if k == "name" || k == "id" || k == "created_at" || k == "updated_at" {
			continue
		}

		liveVal, exists := live[k]

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

	return drifted, updates
}
