package resolver

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"reflect"
	"runtime/debug"
	"time"

	"github.com/99designs/gqlgen/graphql"
	"github.com/99designs/gqlgen/graphql/handler"
	"github.com/99designs/gqlgen/graphql/handler/extension"
	"github.com/99designs/gqlgen/graphql/handler/lru"
	"github.com/99designs/gqlgen/graphql/handler/transport"
	"github.com/vektah/gqlparser/v2/gqlerror"

	"github.com/gorilla/websocket"
	"github.com/ravilushqa/otelgqlgen"

	"github.com/sour-is/ev/internal/lg"
	"github.com/sour-is/ev/pkg/gql/graphiql"
	"github.com/sour-is/ev/pkg/gql/playground"
)

type BaseResolver interface {
	ExecutableSchema() graphql.ExecutableSchema
	BaseResolver() IsResolver
}

type Resolver[T BaseResolver] struct {
	res T
}
type IsResolver interface {
	IsResolver()
}

func New[T BaseResolver](ctx context.Context, base T, resolvers ...IsResolver) (*Resolver[T], error) {
	_, span := lg.Span(ctx)
	defer span.End()

	v := reflect.ValueOf(base)
	v = reflect.Indirect(v)
	noop := reflect.ValueOf(base.BaseResolver())

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

	return &Resolver[T]{base}, nil
}

func (r *Resolver[T]) Resolver() T {
	return r.res
}

// ChainMiddlewares will check all embeded resolvers for a GetMiddleware func and add to handler.
func (r *Resolver[T]) ChainMiddlewares(h http.Handler) http.Handler {
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

func (r *Resolver[T]) RegisterHTTP(mux *http.ServeMux) {
	gql := NewServer(r.res.ExecutableSchema())
	gql.SetRecoverFunc(NoopRecover)
	gql.Use(otelgqlgen.Middleware())
	mux.Handle("/graphiql", graphiql.Handler("GraphiQL playground", "/gql"))
	mux.Handle("/gql", lg.Htrace(r.ChainMiddlewares(gql), "gql"))
	mux.Handle("/playground", playground.Handler("GraphQL playground", "/gql"))
}

func NoopRecover(ctx context.Context, err interface{}) error {
	if err, ok := err.(string); ok && err == "not implemented" {
		return gqlerror.Errorf("not implemented")
	}
	fmt.Fprintln(os.Stderr, err)
	fmt.Fprintln(os.Stderr)
	debug.PrintStack()

	return gqlerror.Errorf("internal system error")
}

func NewServer(es graphql.ExecutableSchema) *handler.Server {
	srv := handler.New(es)

	srv.AddTransport(transport.Websocket{
		Upgrader: websocket.Upgrader{
			CheckOrigin: func(r *http.Request) bool {
				switch r.Header.Get("Origin") {
				case "https://ev.sour.is", "https://www.graphqlbin.com", "http://localhost:8080":
					return true
				default:
					return false
				}
			},
		},
		KeepAlivePingInterval: 10 * time.Second,
	})
	srv.AddTransport(transport.Options{})
	srv.AddTransport(transport.GET{})
	srv.AddTransport(transport.POST{})
	srv.AddTransport(transport.MultipartForm{})

	srv.SetQueryCache(lru.New(1000))

	srv.Use(extension.Introspection{})
	srv.Use(extension.AutomaticPersistedQuery{
		Cache: lru.New(100),
	})

	return srv
}
