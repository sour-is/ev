package resolver

// THIS CODE IS A STARTING POINT ONLY. IT WILL NOT BE UPDATED WITH SCHEMA CHANGES.

import (
	"context"

	"go.sour.is/ev/app/msgbus"
	"go.sour.is/ev/app/salty"
	"go.sour.is/ev/internal/graph/generated"
	"go.sour.is/ev/pkg/es"
	"go.sour.is/ev/pkg/gql"
)

type Resolver struct{}

// // foo
func (r *mutationResolver) TruncateStream(ctx context.Context, streamID string, index int64) (bool, error) {
	panic("not implemented")
}

// // foo
func (r *mutationResolver) CreateSaltyUser(ctx context.Context, nick string, pubkey string) (*salty.SaltyUser, error) {
	panic("not implemented")
}

// // foo
func (r *queryResolver) Events(ctx context.Context, streamID string, paging *gql.PageInput) (*gql.Connection, error) {
	panic("not implemented")
}

// // foo
func (r *queryResolver) Posts(ctx context.Context, streamID string, paging *gql.PageInput) (*gql.Connection, error) {
	panic("not implemented")
}

// // foo
func (r *queryResolver) SaltyUser(ctx context.Context, nick string) (*salty.SaltyUser, error) {
	panic("not implemented")
}

// // foo
func (r *subscriptionResolver) EventAdded(ctx context.Context, streamID string, after int64) (<-chan *es.GQLEvent, error) {
	panic("not implemented")
}

// // foo
func (r *subscriptionResolver) PostAdded(ctx context.Context, streamID string, after int64) (<-chan *msgbus.PostEvent, error) {
	panic("not implemented")
}

// Mutation returns generated.MutationResolver implementation.
func (r *Resolver) Mutation() generated.MutationResolver { return &mutationResolver{r} }

// Query returns generated.QueryResolver implementation.
func (r *Resolver) Query() generated.QueryResolver { return &queryResolver{r} }

// Subscription returns generated.SubscriptionResolver implementation.
func (r *Resolver) Subscription() generated.SubscriptionResolver { return &subscriptionResolver{r} }

type mutationResolver struct{ *Resolver }
type queryResolver struct{ *Resolver }
type subscriptionResolver struct{ *Resolver }
