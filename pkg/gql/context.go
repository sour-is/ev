package gql

import "context"

func ToContext[K comparable, V any](ctx context.Context, key K, value V) context.Context {
	return context.WithValue(ctx, key, value)
}
func FromContext[K comparable, V any](ctx context.Context, key K) V {
	var empty V
	if v, ok := ctx.Value(key).(V); ok {
		return v
	}
	return empty
}
