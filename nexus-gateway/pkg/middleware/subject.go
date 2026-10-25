package middleware

import (
	"context"
	"net/http"
	"strings"

	"google.golang.org/grpc"
	"google.golang.org/grpc/metadata"
)

// Headers naming on whose behalf, by which agent and for which operation a
// credential is requested. The gateway forwards them to the broker, which
// releases a credential only for the subject that owns the connection and
// records all three with every release.
var SubjectHeaders = []string{"X-Nexus-Subject", "X-Nexus-Agent", "X-Nexus-Operation"}

type subjectKey struct{}

// WithSubject returns ctx carrying the subject headers of a request.
func WithSubject(ctx context.Context, values map[string]string) context.Context {
	if len(values) == 0 {
		return ctx
	}
	return context.WithValue(ctx, subjectKey{}, values)
}

// SubjectFrom returns the subject headers carried by ctx.
func SubjectFrom(ctx context.Context) map[string]string {
	values, _ := ctx.Value(subjectKey{}).(map[string]string)
	return values
}

// Subject carries an HTTP request's subject headers in its context.
func Subject(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		values := map[string]string{}
		for _, name := range SubjectHeaders {
			if v := strings.TrimSpace(r.Header.Get(name)); v != "" {
				values[name] = v
			}
		}
		next.ServeHTTP(w, r.WithContext(WithSubject(r.Context(), values)))
	})
}

// SubjectUnaryInterceptor carries a gRPC call's subject metadata in its context.
func SubjectUnaryInterceptor() grpc.UnaryServerInterceptor {
	return func(ctx context.Context, req interface{}, info *grpc.UnaryServerInfo, handler grpc.UnaryHandler) (interface{}, error) {
		if md, ok := metadata.FromIncomingContext(ctx); ok {
			values := map[string]string{}
			for _, name := range SubjectHeaders {
				if got := md.Get(strings.ToLower(name)); len(got) > 0 && strings.TrimSpace(got[0]) != "" {
					values[name] = strings.TrimSpace(got[0])
				}
			}
			ctx = WithSubject(ctx, values)
		}
		return handler(ctx, req)
	}
}

// ForwardSubject sets the subject headers carried by ctx on an outgoing request.
func ForwardSubject(ctx context.Context, req *http.Request) {
	for name, value := range SubjectFrom(ctx) {
		req.Header.Set(name, value)
	}
}
