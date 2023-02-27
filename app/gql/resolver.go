package gql

import (
	"context"

	"github.com/99designs/gqlgen/graphql"
	"go.sour.is/ev/app/msgbus"
	"go.sour.is/ev/app/salty"
	"go.sour.is/ev/internal/graph/generated"
	"go.sour.is/ev/pkg/es"
	"go.sour.is/ev/pkg/gql"
	"go.sour.is/ev/pkg/gql/resolver"
)

type Resolver struct {
	msgbus.MsgbusResolver
	salty.SaltyResolver
	es.EventResolver
}

// Query returns generated.QueryResolver implementation.
func (r *Resolver) Query() generated.QueryResolver { return r }

// Query returns generated.QueryResolver implementation.
func (r *Resolver) Mutation() generated.MutationResolver { return r }

// Subscription returns generated.SubscriptionResolver implementation.
func (r *Resolver) Subscription() generated.SubscriptionResolver { return r }

// func (r *Resolver) isResolver() {}
func (r *Resolver) ExecutableSchema() graphql.ExecutableSchema {
	return generated.NewExecutableSchema(generated.Config{Resolvers: r})
}
func (r *Resolver) BaseResolver() resolver.IsResolver {
	return &noop{}
}

type noop struct{}

var _ msgbus.MsgbusResolver = (*noop)(nil)
var _ salty.SaltyResolver = (*noop)(nil)
var _ es.EventResolver = (*noop)(nil)

func (*noop) IsResolver() {}
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
func (*noop) TruncateStream(ctx context.Context, streamID string, index int64) (bool, error) {
	panic("not implemented")
}
