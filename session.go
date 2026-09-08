package opencodeauth

import "context"

type sessionContextKey struct{}

// WithSessionID associates an opaque conversation identifier with ctx. The
// identifier is validated when an inference request is sent, allowing callers
// to construct contexts without an error-returning helper.
func WithSessionID(ctx context.Context, sessionID string) context.Context {
	if ctx == nil {
		ctx = context.Background()
	}
	return context.WithValue(ctx, sessionContextKey{}, sessionID)
}

func sessionIDFromContext(ctx context.Context) (string, bool) {
	if ctx == nil {
		return "", false
	}
	value, ok := ctx.Value(sessionContextKey{}).(string)
	return value, ok
}
