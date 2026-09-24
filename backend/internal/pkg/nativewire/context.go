package nativewire

import "context"

type requestKey struct{}

// MarkRequest keeps transport-only Native behavior out of HTTP headers.
func MarkRequest(ctx context.Context) context.Context {
	return context.WithValue(ctx, requestKey{}, true)
}

func IsRequest(ctx context.Context) bool {
	marked, _ := ctx.Value(requestKey{}).(bool)
	return marked
}
