package es

import (
	"context"
	"fmt"
	"net/http"
	"time"

	"github.com/sour-is/ev/internal/logz"
	"github.com/sour-is/ev/pkg/es/event"
	"github.com/sour-is/ev/pkg/gql"
)

type EventResolver interface {
	Events(ctx context.Context, streamID string, paging *gql.PageInput) (*gql.Connection, error)
	EventAdded(ctx context.Context, streamID string, after int64) (<-chan *GQLEvent, error)
}

func (es *EventStore) Events(ctx context.Context, streamID string, paging *gql.PageInput) (*gql.Connection, error) {
	ctx, span := logz.Span(ctx)
	defer span.End()

	lis, err := es.Read(ctx, streamID, paging.GetIdx(0), paging.GetCount(30))
	if err != nil {
		span.RecordError(err)
		return nil, err
	}

	edges := make([]gql.Edge, 0, len(lis))
	for i := range lis {
		span.AddEvent(fmt.Sprint("event ", i, " of ", len(lis)))
		edges = append(edges, &GQLEvent{lis[i]})
	}

	var first, last uint64
	if first, err = es.FirstIndex(ctx, streamID); err != nil {
		span.RecordError(err)
		return nil, err
	}
	if last, err = es.LastIndex(ctx, streamID); err != nil {
		span.RecordError(err)
		return nil, err
	}

	return &gql.Connection{
		Paging: &gql.PageInfo{
			Next:  lis.Last().EventMeta().Position < last,
			Prev:  lis.First().EventMeta().Position > first,
			Begin: lis.First().EventMeta().Position,
			End:   lis.Last().EventMeta().Position,
		},
		Edges: edges,
	}, nil
}
func (e *EventStore) EventAdded(ctx context.Context, streamID string, after int64) (<-chan *GQLEvent, error) {
	ctx, span := logz.Span(ctx)
	defer span.End()

	es := e.EventStream()
	if es == nil {
		return nil, fmt.Errorf("EventStore does not implement streaming")
	}

	sub, err := es.Subscribe(ctx, streamID, after)
	if err != nil {
		span.RecordError(err)
		return nil, err
	}

	ch := make(chan *GQLEvent)

	go func() {
		ctx, span := logz.Span(ctx)
		defer span.End()

		defer func() {
			ctx, cancel := context.WithTimeout(context.Background(), 1*time.Second)
			defer cancel()
			err := sub.Close(ctx)
			span.RecordError(err)
		}()

		for sub.Recv(ctx) {
			events, err := sub.Events(ctx)
			if err != nil {
				span.RecordError(err)
				break
			}
			span.AddEvent(fmt.Sprintf("received %d events", len(events)))

			for i := range events {
				select {
				case ch <- &GQLEvent{events[i]}:
					continue
				case <-ctx.Done():
					return
				}

			}
		}
	}()

	return ch, nil
}
func (*EventStore) RegisterHTTP(*http.ServeMux) {}

type GQLEvent struct {
	e event.Event
}

func (e *GQLEvent) ID() string {
	return "Event/" + e.e.EventMeta().GetEventID()
}
func (e *GQLEvent) EventID() string {
	return e.e.EventMeta().GetEventID()
}
func (e *GQLEvent) Values() map[string]interface{} {
	return event.Values(e.e)
}
func (e *GQLEvent) Bytes() (string, error) {
	b, err := e.e.MarshalBinary()
	return string(b), err
}
func (e *GQLEvent) Meta() *event.Meta {
	meta := e.e.EventMeta()
	return &meta
}
func (e *GQLEvent) IsEdge() {}
