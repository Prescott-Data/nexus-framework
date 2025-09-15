package logging

import (
	"context"
	"encoding/json"
	"log"
	"net/url"
	"strings"

	"github.com/go-chi/chi/v5/middleware"
)

var sensitiveKeys = map[string]struct{}{
	"access_token":  {},
	"refresh_token": {},
	"client_secret": {},
	"authorization": {},
	"token":         {},
	"code":          {},
	"state":         {},
}

// RedactValue hides sensitive values by returning a placeholder.
func RedactValue(key string, value any) any {
	if _, ok := sensitiveKeys[strings.ToLower(key)]; ok {
		return "[REDACTED]"
	}
	return value
}

// RedactMap returns a shallow redacted copy of the map.
func RedactMap(m map[string]any) map[string]any {
	out := make(map[string]any, len(m))
	for k, v := range m {
		out[k] = RedactValue(k, v)
	}
	return out
}

// RedactQuery sanitizes URL query parameters.
func RedactQuery(u string) string {
	parsed, err := url.Parse(u)
	if err != nil {
		return u
	}
	q := parsed.Query()
	for key := range q {
		if _, ok := sensitiveKeys[strings.ToLower(key)]; ok {
			q.Set(key, "[REDACTED]")
		}
	}
	parsed.RawQuery = q.Encode()
	return parsed.String()
}

// Info logs a JSON line with message and fields, including request_id from context if present.
func Info(ctx context.Context, msg string, fields map[string]any) {
	if fields == nil {
		fields = map[string]any{}
	}
	if reqID := middleware.GetReqID(ctx); reqID != "" {
		fields["request_id"] = reqID
	}
	payload := map[string]any{
		"level":  "info",
		"msg":    msg,
		"fields": RedactMap(fields),
	}
	b, _ := json.Marshal(payload)
	log.Println(string(b))
}

// Error logs an error with fields.
func Error(ctx context.Context, msg string, fields map[string]any) {
	if fields == nil {
		fields = map[string]any{}
	}
	if reqID := middleware.GetReqID(ctx); reqID != "" {
		fields["request_id"] = reqID
	}
	payload := map[string]any{
		"level":  "error",
		"msg":    msg,
		"fields": RedactMap(fields),
	}
	b, _ := json.Marshal(payload)
	log.Println(string(b))
}
