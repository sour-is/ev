package peerfinder

import (
	"context"
	"fmt"
	"sync/atomic"
	"time"

	"github.com/sour-is/ev"
	"github.com/sour-is/ev/internal/lg"
	"github.com/sour-is/ev/pkg/es/event"
	"github.com/sour-is/ev/pkg/locker"
	"go.uber.org/multierr"
)

const (
	aggInfo       = "pf-info"
	queueRequests = "pf-requests"
	queueResults  = "pf-results"
	initVersion   = "1.2.1"
)

func aggRequest(id string) string { return "pf-request-" + id }
func aggPeer(id string) string    { return "pf-peer-" + id }

type service struct {
	es        *ev.EventStore
	statusURL string

	state *locker.Locked[state]
	up    atomic.Bool
	stop  func()
}

type state struct {
	peers    map[string]*Peer
	requests map[string]*Request
}

func New(ctx context.Context, es *ev.EventStore, statusURL string) (*service, error) {
	ctx, span := lg.Span(ctx)
	defer span.End()

	loadTemplates()

	if err := event.Register(ctx, &RequestSubmitted{}, &ResultSubmitted{}, &VersionChanged{}); err != nil {
		span.RecordError(err)
		return nil, err
	}

	svc := &service{
		es:        es,
		statusURL: statusURL,
		state: locker.New(&state{
			peers:    make(map[string]*Peer),
			requests: make(map[string]*Request),
		})}

	return svc, nil
}
func (s *service) loadResult(ctx context.Context, request *Request) (*Request, error) {
	if request == nil {
		return request, nil
	}

	return request, s.state.Modify(ctx, func(ctx context.Context, t *state) error {

		for i := range request.Responses {
			res := request.Responses[i]
			if peer, ok := t.peers[res.PeerID]; ok {
				res.Peer = peer
				res.Peer.ID = ""
			}
		}

		return nil
	})
}
func (s *service) Run(ctx context.Context) (err error) {
	var errs error

	ctx, span := lg.Span(ctx)
	defer span.End()

	ctx, s.stop = context.WithCancel(ctx)

	subReq, e := s.es.EventStream().Subscribe(ctx, queueRequests, 0)
	errs = multierr.Append(errs, e)

	subRes, e := s.es.EventStream().Subscribe(ctx, queueResults, 0)
	errs = multierr.Append(errs, e)

	defer func() {
		ctx, cancel := context.WithTimeout(context.Background(), 1*time.Second)
		defer cancel()

		err = multierr.Combine(subReq.Close(ctx), subRes.Close(ctx), err)
	}()

	if errs != nil {
		return errs
	}

	for {
		var events event.Events
		select {
		case <-ctx.Done():
			return nil
		case ok := <-subReq.Recv(ctx):
			if ok {
				events, err = subReq.Events(ctx)
			}
		case ok := <-subRes.Recv(ctx):
			if ok {
				events, err = subRes.Events(ctx)
			}
		}

		s.state.Modify(ctx, func(ctx context.Context, state *state) error {
			return state.ApplyEvents(events)
		})
		events = events[:0]
	}
}
func (s *service) Stop(ctx context.Context) (err error) {
	defer func() {
		if p := recover(); p != nil {
			err = fmt.Errorf("PANIC: %v", p)
		}
	}()

	s.stop()
	return err
}

func (s *state) ApplyEvents(events event.Events) error {
	for _, e := range events {
		switch e := e.(type) {
		case *RequestSubmitted:
			if _, ok := s.requests[e.RequestID()]; !ok {
				s.requests[e.RequestID()] = &Request{}
			}
			s.requests[e.RequestID()].ApplyEvent(e)
		case *ResultSubmitted:
			if _, ok := s.requests[e.RequestID]; !ok {
				s.requests[e.RequestID] = &Request{}
			}
			s.requests[e.RequestID].ApplyEvent(e)
		case *RequestTruncated:
			delete(s.requests, e.RequestID)
		}
	}

	return nil
}

func Projector(e event.Event) []event.Event {
	m := e.EventMeta()
	streamID := m.StreamID
	streamPos := m.Position

	switch e := e.(type) {
	case *RequestSubmitted:
		e1 := event.NewPtr(streamID, streamPos)
		event.SetStreamID(aggRequest(e.RequestID()), e1)

		return []event.Event{e1}
	case *ResultSubmitted:
		e1 := event.NewPtr(streamID, streamPos)
		event.SetStreamID(aggRequest(e.RequestID), e1)

		e2 := event.NewPtr(streamID, streamPos)
		event.SetStreamID(aggPeer(e.PeerID), e2)

		return []event.Event{e1, e2}
	}
	return nil
}
