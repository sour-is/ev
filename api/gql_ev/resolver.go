package gql_ev

import (
	"context"
	"net/http"
	"reflect"

	"github.com/sour-is/ev/app/msgbus"
	"github.com/sour-is/ev/app/salty"
	"github.com/sour-is/ev/internal/graph/generated"
	"github.com/sour-is/ev/internal/logz"
)

type Resolver struct {
	msgbus.MsgbusResolver
	salty.SaltyResolver
}

func New(ctx context.Context, m msgbus.MsgbusResolver, s salty.SaltyResolver) (*Resolver, error) {
	_, span := logz.Span(ctx)
	defer span.End()

	r := &Resolver{m, s}

	return r, nil
}

// Query returns generated.QueryResolver implementation.
func (r *Resolver) Query() generated.QueryResolver { return r }

// Query returns generated.QueryResolver implementation.
func (r *Resolver) Mutation() generated.MutationResolver { return r }

// Subscription returns generated.SubscriptionResolver implementation.
func (r *Resolver) Subscription() generated.SubscriptionResolver { return r }

// ChainMiddlewares will check all embeded resolvers for a GetMiddleware func and add to handler.
func (r *Resolver) ChainMiddlewares(h http.Handler) http.Handler {
	v := reflect.ValueOf(r) // Get reflected value of *Resolver
	v = reflect.Indirect(v) // Get the pointed value (returns a zero value on nil)
	n := v.NumField()       // Get number of fields to iterate over.
	for i := 0; i < n; i++ {
		f := v.Field(i)
		if !f.CanInterface() { // Skip non-interface types.
			continue
		}
		if iface, ok := f.Interface().(interface {
			GetMiddleware() func(http.Handler) http.Handler
		}); ok {
			h = iface.GetMiddleware()(h) // Append only items that fulfill the interface.
		}
	}

	return h
}
