package gql_ev

import (
	"context"
	"fmt"
	"log"
	"time"

	"github.com/sour-is/ev/pkg/es"
	"github.com/sour-is/ev/pkg/msgbus"
)

// This file will not be regenerated automatically.
//
// It serves as dependency injection for your app, add any dependencies you require here.

type Resolver struct {
	es *es.EventStore
}

func New(es *es.EventStore) *Resolver {
	return &Resolver{es}
}

// Posts is the resolver for the events field.
func (r *Resolver) Posts(ctx context.Context, streamID string, paging *PageInput) (*Connection, error) {
	lis, err := r.es.Read(ctx, streamID, paging.GetIdx(0), paging.GetCount(30))
	if err != nil {
		return nil, err
	}

	edges := make([]Edge, 0, len(lis))
	for i := range lis {
		e := lis[i]
		m := e.EventMeta()

		post, ok := e.(*msgbus.PostEvent)
		if !ok {
			continue
		}

		edges = append(edges, PostEvent{
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

func (r *Resolver) PostAdded(ctx context.Context, streamID string) (<-chan *PostEvent, error) {
	es := r.es.EventStream()
	if es == nil {
		return nil, fmt.Errorf("EventStore does not implement streaming")
	}

	sub, err := es.Subscribe(ctx, streamID)
	if err != nil {
		return nil, err
	}

	ch := make(chan *PostEvent)

	go func() {
		defer func() {
			ctx, cancel := context.WithTimeout(context.Background(), 1*time.Second)
			defer cancel()
			log.Print(sub.Close(ctx))
		}()

		for sub.Recv(ctx) {
			events, err := sub.Events(ctx)
			if err != nil {
				break
			}
			for _, e := range events {
				m := e.EventMeta()
				if p, ok := e.(*msgbus.PostEvent); ok {
					select {
					case ch <- &PostEvent{
						ID:      m.EventID.String(),
						Payload: string(p.Payload),
						Tags:    p.Tags,
						Meta:    &m,
					}:
						continue
					case <-ctx.Done():
						return
					}
				}
			}
		}
	}()

	return ch, nil
}
