package gql

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"reflect"
	"runtime/debug"

	"github.com/99designs/gqlgen/graphql/handler"
	"github.com/ravilushqa/otelgqlgen"
	"github.com/sour-is/ev/app/gql/playground"
	"github.com/sour-is/ev/app/msgbus"
	"github.com/sour-is/ev/app/salty"
	"github.com/sour-is/ev/internal/graph/generated"
	"github.com/sour-is/ev/internal/logz"
	"github.com/sour-is/ev/pkg/es"
	"github.com/sour-is/ev/pkg/gql"
	"github.com/vektah/gqlparser/v2/gqlerror"
)

type Resolver struct {
	msgbus.MsgbusResolver
	salty.SaltyResolver
	es.EventResolver
}

func New(ctx context.Context, resolvers ...interface{ RegisterHTTP(*http.ServeMux) }) (*Resolver, error) {
	_, span := logz.Span(ctx)
	defer span.End()

	r := &Resolver{}
	v := reflect.ValueOf(r)
	v = reflect.Indirect(v)
	noop := reflect.ValueOf(&noop{})

outer:
	for _, idx := range reflect.VisibleFields(v.Type()) {
		field := v.FieldByIndex(idx.Index)

		for i := range resolvers {
			rs := reflect.ValueOf(resolvers[i])

			if field.IsNil() && rs.Type().Implements(field.Type()) {
				span.AddEvent(fmt.Sprint("found ", field.Type().Name()))
				field.Set(rs)
				continue outer
			}
		}

		span.AddEvent(fmt.Sprint("default ", field.Type().Name()))
		field.Set(noop)
	}

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

func (r *Resolver) RegisterHTTP(mux *http.ServeMux) {
	gql := handler.NewDefaultServer(generated.NewExecutableSchema(generated.Config{Resolvers: r}))
	gql.SetRecoverFunc(NoopRecover)
	gql.Use(otelgqlgen.Middleware())
	mux.Handle("/", playground.Handler("GraphQL playground", "/gql"))
	mux.Handle("/gql", logz.Htrace(r.ChainMiddlewares(gql), "gql"))
}

type noop struct{}

func NoopRecover(ctx context.Context, err interface{}) error {
	if err, ok := err.(string); ok && err == "not implemented" {
		return gqlerror.Errorf("not implemented")
	}
	fmt.Fprintln(os.Stderr, err)
	fmt.Fprintln(os.Stderr)
	debug.PrintStack()

	return gqlerror.Errorf("internal system error")
}

var _ msgbus.MsgbusResolver = (*noop)(nil)
var _ salty.SaltyResolver = (*noop)(nil)
var _ es.EventResolver = (*noop)(nil)

func (*noop) CreateSaltyUser(ctx context.Context, nick string, pubkey string) (*salty.SaltyUser, error) {
	panic("not implemented")
}
func (*noop) Posts(ctx context.Context, streamID string, paging *gql.PageInput) (*gql.Connection, error) {
	panic("not implemented")
}
func (*noop) SaltyUser(ctx context.Context, nick string) (*salty.SaltyUser, error) {
	panic("not implemented")
}
func (*noop) PostAdded(ctx context.Context, streamID string, after int64) (<-chan *msgbus.PostEvent, error) {
	panic("not implemented")
}
func (*noop) Events(ctx context.Context, streamID string, paging *gql.PageInput) (*gql.Connection, error) {
	panic("not implemented")
}
func (*noop) EventAdded(ctx context.Context, streamID string, after int64) (<-chan *es.GQLEvent, error) {
	panic("not implemented")
}

func (*noop) RegisterHTTP(*http.ServeMux) {}
