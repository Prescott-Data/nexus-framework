package config

import (
	"log"
	"os"
	"strings"
)

// GetAllowedOrigins returns the list of allowed origins for CORS.
// It reads from the CORS_ALLOWED_ORIGINS environment variable (comma-separated).
// Defaults to local development origins if not set.
func GetAllowedOrigins() []string {
	origins := []string{"http://localhost:3000", "http://localhost:5173"} // Default dev origins
	
	val := os.Getenv("CORS_ALLOWED_ORIGINS")
	if val != "" {
		origins = strings.Split(val, ",")
		for i := range origins {
			origins[i] = strings.TrimSpace(origins[i])
		}
		log.Printf("CORS: Using configured origins: %v", origins)
	} else {
		log.Printf("CORS: CORS_ALLOWED_ORIGINS not set. Using permissive dev defaults: %v", origins)
		log.Printf("CORS: WARNING: Do not use these defaults in production.")
	}
	
	return origins
}
