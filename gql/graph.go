package gql_ev

import (
	"context"
	"encoding"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"time"

	"go.sour.is/pkg/gql"
	"go.sour.is/pkg/lg"

	"go.sour.is/ev"
	"go.sour.is/ev/event"
)

type EventResolver interface {
	Events(ctx context.Context, streamID string, paging *gql.PageInput) (*gql.Connection, error)
	EventAdded(ctx context.Context, streamID string, after int64) (<-chan *Event, error)
	TruncateStream(ctx context.Context, streamID string, index int64) (bool, error)
}
type contextKey struct {
	name string
}

var esKey = contextKey{"event-store"}

type EventStore struct {
	*ev.EventStore
}

func (es *EventStore) IsResolver() {}
func (es *EventStore) Events(ctx context.Context, streamID string, paging *gql.PageInput) (*gql.Connection, error) {
	ctx, span := lg.Span(ctx)
	defer span.End()

	lis, err := es.Read(ctx, streamID, paging.GetIdx(0), paging.GetCount(30))
	if err != nil && !errors.Is(err, ev.ErrNotFound) {
		span.RecordError(err)
		return nil, err
	}

	edges := make([]gql.Edge, 0, len(lis))
	for i := range lis {
		span.AddEvent(fmt.Sprint("event ", i, " of ", len(lis)))
		edges = append(edges, &Event{lis[i]})
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
			Next:  lis.Last().EventMeta().ActualPosition < last,
			Prev:  lis.First().EventMeta().ActualPosition > first,
			Begin: lis.First().EventMeta().ActualPosition,
			End:   lis.Last().EventMeta().ActualPosition,
		},
		Edges: edges,
	}, nil
}
func (e *EventStore) EventAdded(ctx context.Context, streamID string, after int64) (<-chan *Event, error) {
	ctx, span := lg.Span(ctx)
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

	ch := make(chan *Event)

	go func() {
		ctx, span := lg.Span(ctx)
		defer span.End()

		{
			ctx, span := lg.Fork(ctx)
			defer func() {
				defer span.End()
				ctx, cancel := context.WithTimeout(ctx, 1*time.Second)
				defer cancel()
				err := sub.Close(ctx)
				span.RecordError(err)
			}()
		}

		for <-sub.Recv(ctx) {
			events, err := sub.Events(ctx)
			if err != nil {
				span.RecordError(err)
				break
			}
			span.AddEvent(fmt.Sprintf("received %d events", len(events)))

			for i := range events {
				select {
				case ch <- &Event{events[i]}:
					continue
				case <-ctx.Done():
					return
				}

			}
		}
	}()

	return ch, nil
}
func (es *EventStore) TruncateStream(ctx context.Context, streamID string, index int64) (bool, error) {
	ctx, span := lg.Span(ctx)
	defer span.End()

	err := es.Truncate(ctx, streamID, index)
	return err == nil, err
}
func (e *EventStore) GetMiddleware() func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			ctx, span := lg.Span(r.Context())
			defer span.End()

			r = r.WithContext(gql.ToContext(ctx, esKey, e))
			next.ServeHTTP(w, r)
		})
	}
}

type Event struct {
	e event.Event
}

func (e *Event) ID() string {
	return fmt.Sprint(e.e.EventMeta().StreamID, "@", e.e.EventMeta().Position)
}
func (e *Event) EventID() string {
	return e.e.EventMeta().GetEventID()
}
func (e *Event) StreamID() string {
	return e.e.EventMeta().StreamID
}
func (e *Event) Position() uint64 {
	return e.e.EventMeta().Position
}
func (e *Event) Type() string {
	return event.TypeOf(e.e)
}
func (e *Event) Created() time.Time {
	return e.e.EventMeta().Created()
}
func (e *Event) Values() map[string]interface{} {
	return event.Values(e.e)
}
func (e *Event) Bytes() (string, error) {
	switch e := e.e.(type) {
	case encoding.BinaryMarshaler:
		b, err := e.MarshalBinary()
		return string(b), err
	case encoding.TextMarshaler:
		b, err := e.MarshalText()
		return string(b), err
	default:
		b, err := json.Marshal(e)
		return string(b), err
	}
}
func (e *Event) Meta() *event.Meta {
	meta := e.e.EventMeta()
	return &meta
}
func (e *Event) Linked(ctx context.Context) (*Event, error) {
	ctx, span := lg.Span(ctx)
	defer span.End()

	values := event.Values(e.e)
	streamID, ok := values["stream_id"].(string)
	if !ok {
		return nil, nil
	}
	pos, ok := values["pos"].(uint64)
	if !ok {
		return nil, nil
	}

	events, err := gql.FromContext[contextKey, *EventStore](ctx, esKey).ReadN(ctx, streamID, pos)
	return &Event{e: events.First()}, err
}
func (e *Event) IsEdge() {}
