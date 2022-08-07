package gql_ev

import (
	"context"

	"github.com/sour-is/ev/pkg/es/driver"
	"github.com/sour-is/ev/pkg/es/service"
)

// This file will not be regenerated automatically.
//
// It serves as dependency injection for your app, add any dependencies you require here.

type Resolver struct {
	es driver.EventStore
}

func New(es driver.EventStore) *Resolver {
	return &Resolver{es}
}

// Events is the resolver for the events field.
func (r *Resolver) Events(ctx context.Context, streamID string, paging *PageInput) (*Connection, error) {
	lis, err := r.es.Read(ctx, streamID, paging.GetIdx(0), paging.GetCount(30))
	if err != nil {
		return nil, err
	}

	edges := make([]Edge, 0, len(lis))
	for i := range lis {
		e := lis[i]
		m := e.EventMeta()

		post, ok := e.(*service.PostEvent)
		if !ok {
			continue
		}

		edges = append(edges, Event{
			ID:      lis[i].EventMeta().EventID.String(),
			Payload: string(post.Payload),
			Tags:    post.Tags,
			Meta:    &m,
		})
	}

	var first, last uint64
	if first, err = r.es.FirstIndex(ctx, streamID); err != nil {
		return nil, err
	}
	if last, err = r.es.LastIndex(ctx, streamID); err != nil {
		return nil, err
	}

	return &Connection{
		Paging: &PageInfo{
			Next:  lis.Last().EventMeta().Position < last,
			Prev:  lis.First().EventMeta().Position > first,
			Begin: lis.First().EventMeta().Position,
			End:   lis.Last().EventMeta().Position,
		},
		Edges: edges,
	}, nil
}
