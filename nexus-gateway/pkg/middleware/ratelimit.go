package middleware

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"strconv"
	"strings"

	"github.com/go-redis/redis/v8"
	"github.com/go-redis/redis_rate/v9"
)

type contextKey string

const workspaceIDKey = contextKey("workspace_id")

func getEnvInt(key string, fallback int) int {
	if val := os.Getenv(key); val != "" {
		if parsed, err := strconv.Atoi(val); err == nil {
			return parsed
		}
	}
	return fallback
}

// RateLimiter returns an HTTP middleware that throttles requests per workspace_id.
func RateLimiter(redisClient *redis.Client) func(http.Handler) http.Handler {
	limiter := redis_rate.NewLimiter(redisClient)

	// Fetch configurations at initialization (can be dynamic per-request if needed)
	enabled := os.Getenv("RATE_LIMIT_ENABLED") == "true"
	reqConnRPM := getEnvInt("RATE_LIMIT_REQUEST_CONNECTION_RPM", 10)
	callbackRPM := getEnvInt("RATE_LIMIT_CALLBACK_RPM", 20)
	tokenRPM := getEnvInt("RATE_LIMIT_TOKEN_RPM", 60)

	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if !enabled || redisClient == nil {
				next.ServeHTTP(w, r)
				return
			}

			var limit redis_rate.Limit
			var limitKeyPrefix string

			path := r.URL.Path

			switch {
			case strings.HasPrefix(path, "/v1/request-connection") && r.Method == http.MethodPost:
				limit = redis_rate.PerMinute(reqConnRPM)
				limitKeyPrefix = "req_conn"
			case strings.HasPrefix(path, "/v1/callback") || strings.HasPrefix(path, "/auth/callback"):
				limit = redis_rate.PerMinute(callbackRPM)
				limitKeyPrefix = "callback"
			case strings.HasPrefix(path, "/v1/token") && r.Method == http.MethodGet:
				limit = redis_rate.PerMinute(tokenRPM)
				limitKeyPrefix = "token"
			case strings.HasPrefix(path, "/v1/refresh") && r.Method == http.MethodPost:
				limit = redis_rate.PerMinute(tokenRPM)
				limitKeyPrefix = "refresh"
			default:
				// Route not subject to rate limits
				next.ServeHTTP(w, r)
				return
			}

			if limit.Rate <= 0 {
				next.ServeHTTP(w, r)
				return
			}

			workspaceID := extractWorkspaceID(r)
			if workspaceID == "" {
				// Fallback to IP address if workspace_id isn't available (e.g., initial callback hits)
				workspaceID = extractIP(r)
			}

			key := fmt.Sprintf("rate_limit:%s:%s", limitKeyPrefix, workspaceID)

			res, err := limiter.Allow(r.Context(), key, limit)
			if err != nil {
				// Fail open on Redis error so we don't break production if Redis is temporarily unreachable
				next.ServeHTTP(w, r)
				return
			}

			if res.Allowed == 0 {
				w.Header().Set("Retry-After", strconv.Itoa(int(res.RetryAfter.Seconds())))
				http.Error(w, "Too Many Requests", http.StatusTooManyRequests)
				return
			}

			next.ServeHTTP(w, r)
		})
	}
}

// extractWorkspaceID attempts to find the workspace_id in the Context, Header, or Query.
func extractWorkspaceID(r *http.Request) string {
	if ws, ok := r.Context().Value(workspaceIDKey).(string); ok && ws != "" {
		return ws
	}
	// The grpc-gateway might put things differently or simple string
	if ws, ok := r.Context().Value("workspace_id").(string); ok && ws != "" {
		return ws
	}

	if ws := r.Header.Get("X-Workspace-Id"); ws != "" {
		return ws
	}

	if ws := r.URL.Query().Get("workspace_id"); ws != "" {
		return ws
	}

	return ""
}

// extractIP is a simple fallback identifier extraction
func extractIP(r *http.Request) string {
	if ip := r.Header.Get("X-Real-IP"); ip != "" {
		return ip
	}
	if ip := r.Header.Get("X-Forwarded-For"); ip != "" {
		parts := strings.Split(ip, ",")
		return strings.TrimSpace(parts[0])
	}
	ip := r.RemoteAddr
	if idx := strings.LastIndex(ip, ":"); idx != -1 {
		return ip[:idx]
	}
	return ip
}

// ContextWithWorkspace injects a workspace_id into the context.
func ContextWithWorkspace(ctx context.Context, id string) context.Context {
	return context.WithValue(ctx, workspaceIDKey, id)
}
