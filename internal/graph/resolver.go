package graph

import (
	"net/http"
	"reflect"

	"github.com/sour-is/ev/api/gql_ev"
	"github.com/sour-is/ev/internal/graph/generated"
)

// This file will not be regenerated automatically.
//
// It serves as dependency injection for your app, add any dependencies you require here.

type Resolver struct {
	*gql_ev.Resolver
}

func New(r *gql_ev.Resolver) *Resolver {
	return &Resolver{r}
}

// Query returns generated.QueryResolver implementation.
func (r *Resolver) Query() generated.QueryResolver { return &queryResolver{r} }

// Query returns generated.QueryResolver implementation.
func (r *Resolver) Mutation() generated.MutationResolver { return &mutationResolver{r} }

// Subscription returns generated.SubscriptionResolver implementation.
func (r *Resolver) Subscription() generated.SubscriptionResolver { return &subscriptionResolver{r} }

type queryResolver struct{ *Resolver }

type mutationResolver struct{ *Resolver }

type subscriptionResolver struct{ *Resolver }

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
