package gql_ev

import (
	"context"
	"crypto/sha256"
	"fmt"
	"strings"
	"time"

	"github.com/keys-pub/keys"
	"github.com/sour-is/ev/internal/logz"
	"github.com/sour-is/ev/pkg/domain"
	"github.com/sour-is/ev/pkg/es"
	"github.com/sour-is/ev/pkg/msgbus"
	"go.opentelemetry.io/otel/metric/instrument/syncint64"
	"go.uber.org/multierr"
)

type Resolver struct {
	es *es.EventStore

	Mresolver_posts            syncint64.Counter
	Mresolver_post_added       syncint64.Counter
	Mresolver_post_added_event syncint64.Counter
}

func New(es *es.EventStore) (*Resolver, error) {
	m := logz.Meter(context.Background())

	var errs error

	Mresolver_posts, err := m.SyncInt64().Counter("resolver_posts")
	errs = multierr.Append(errs, err)

	Mresolver_post_added, err := m.SyncInt64().Counter("resolver_post_added")
	errs = multierr.Append(errs, err)

	Mresolver_post_added_event, err := m.SyncInt64().Counter("resolver_post_added")
	errs = multierr.Append(errs, err)

	return &Resolver{
		es,
		Mresolver_posts,
		Mresolver_post_added,
		Mresolver_post_added_event,
	}, errs
}

// Posts is the resolver for the events field.
func (r *Resolver) Posts(ctx context.Context, streamID string, paging *PageInput) (*Connection, error) {
	ctx, span := logz.Span(ctx)
	defer span.End()

	r.Mresolver_posts.Add(ctx, 1)

	lis, err := r.es.Read(ctx, streamID, paging.GetIdx(0), paging.GetCount(30))
	if err != nil {
		return nil, err
	}

	edges := make([]Edge, 0, len(lis))
	for i := range lis {
		span.AddEvent(fmt.Sprint("post ", i, " of ", len(lis)))
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

func (r *Resolver) PostAdded(ctx context.Context, streamID string, after int64) (<-chan *PostEvent, error) {
	ctx, span := logz.Span(ctx)
	defer span.End()

	r.Mresolver_post_added.Add(ctx, 1)

	es := r.es.EventStream()
	if es == nil {
		return nil, fmt.Errorf("EventStore does not implement streaming")
	}

	sub, err := es.Subscribe(ctx, streamID, after)
	if err != nil {
		return nil, err
	}

	ch := make(chan *PostEvent)

	go func() {
		defer func() {
			ctx, cancel := context.WithTimeout(context.Background(), 1*time.Second)
			defer cancel()
			sub.Close(ctx)
		}()

		for sub.Recv(ctx) {
			events, err := sub.Events(ctx)
			if err != nil {
				break
			}
			r.Mresolver_post_added_event.Add(ctx, int64(len(events)))

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

func (r *Resolver) CreateSaltyUser(ctx context.Context, nick string, pub string) (*SaltyUser, error) {
	streamID := fmt.Sprintf("saltyuser-%x", sha256.Sum256([]byte(strings.ToLower(nick))))

	key, err := keys.NewEdX25519PublicKeyFromID(keys.ID(pub))
	if err != nil {
		return nil, err
	}

	a, err := es.Create(ctx, r.es, streamID, func(ctx context.Context, agg *domain.SaltyUser) error {
		return agg.OnUserRegister(nick, key)
	})
	if err != nil {
		return nil, err
	}

	return &SaltyUser{
		Nick:   nick,
		Pubkey: pub,
		Inbox:  a.Inbox.String(),
	}, err
}

func (r *Resolver) SaltyUser(ctx context.Context, nick string) (*SaltyUser, error) {
	streamID := fmt.Sprintf("saltyuser-%x", sha256.Sum256([]byte(strings.ToLower(nick))))

	a, err := es.Update(ctx, r.es, streamID, func(ctx context.Context, agg *domain.SaltyUser) error { return nil })
	if err != nil {
		return nil, err
	}

	return &SaltyUser{
		Nick:   nick,
		Pubkey: a.Pubkey.String(),
		Inbox:  a.Inbox.String(),
	}, err
}
