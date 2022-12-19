package peerfinder

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net/http"
	"time"

	"github.com/sour-is/ev/internal/lg"
	"github.com/sour-is/ev/pkg/es"
	"github.com/sour-is/ev/pkg/es/event"
	"github.com/sour-is/ev/pkg/set"
)

// RefreshJob retrieves peer info from the peerdb
func (s *service) RefreshJob(ctx context.Context, _ time.Time) error {
	ctx, span := lg.Span(ctx)
	defer span.End()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, s.statusURL, nil)
	span.RecordError(err)
	if err != nil {
		return err
	}
	req.Header.Set("Accept", "application/json")

	res, err := http.DefaultClient.Do(req)
	span.RecordError(err)
	if err != nil {
		return err
	}

	defer res.Body.Close()
	var peers []*Peer
	err = json.NewDecoder(res.Body).Decode(&peers)
	span.RecordError(err)
	if err != nil {
		return err
	}

	err = s.state.Modify(ctx, func(ctx context.Context, t *state) error {
		for _, peer := range peers {
			t.peers[peer.ID] = peer
		}

		return nil
	})
	span.RecordError(err)
	if err != nil {
		return err
	}

	log.Printf("processed %d peers", len(peers))
	span.AddEvent(fmt.Sprintf("processed %d peers", len(peers)))

	s.up.Store(true)

	err = s.cleanPeerJobs(ctx)

	span.RecordError(err)
	return err
}

const maxResults = 30

// CleanJob truncates streams old request data
func (s *service) CleanJob(ctx context.Context, now time.Time) error {
	ctx, span := lg.Span(ctx)
	defer span.End()

	span.AddEvent("clear peerfinder requests")

	err := s.cleanRequests(ctx, now)
	if err != nil {
		return err
	}
	// if err = s.cleanResults(ctx, endRequestID); err != nil {
	// 	return err
	// }

	return s.cleanPeerJobs(ctx)
}
func (s *service) cleanPeerJobs(ctx context.Context) error {
	ctx, span := lg.Span(ctx)
	defer span.End()

	peers := set.New[string]()
	err := s.state.Modify(ctx, func(ctx context.Context, state *state) error {
		for id := range state.peers {
			peers.Add(id)
		}
		return nil
	})
	if err != nil {
		return err
	}

	// trunctate all the peer streams to last 30
	for streamID := range peers {
		streamID = aggPeer(streamID)
		first, err := s.es.FirstIndex(ctx, streamID)
		if err != nil {
			return err
		}
		last, err := s.es.LastIndex(ctx, streamID)
		if err != nil {
			return err
		}
		if last-first < maxResults {
			fmt.Println("SKIP", streamID, first, last)
			continue
		}

		newFirst := int64(last - 30)
		// fmt.Println("TRUNC", streamID, first, newFirst, last)
		span.AddEvent(fmt.Sprint("TRUNC", streamID, first, newFirst, last))
		err = s.es.Truncate(ctx, streamID, int64(newFirst))
		if err != nil {
			return err
		}
	}

	return nil
}
func (s *service) cleanRequests(ctx context.Context, now time.Time) error {
	ctx, span := lg.Span(ctx)
	defer span.End()

	var streamIDs []string
	var startPosition, endPosition int64

	first, err := s.es.FirstIndex(ctx, queueRequests)
	if err != nil {
		return err
	}
	last, err := s.es.LastIndex(ctx, queueRequests)
	if err != nil {
		return err
	}

	if last-first < maxResults {
		// fmt.Println("SKIP", queueRequests, first, last)
		return nil
	}

	startPosition = int64(first - 1)
	endPosition = int64(last - maxResults)

	for {
		events, err := s.es.Read(ctx, queueRequests, startPosition, 1000) // read 1000 from the top each loop.
		if err != nil && !errors.Is(err, es.ErrNotFound) {
			span.RecordError(err)
			return err
		}

		if len(events) == 0 {
			break
		}

		startPosition = int64(events.Last().EventMeta().ActualPosition)
		for _, event := range events {
			switch e := event.(type) {
			case *RequestSubmitted:
				if e.eventMeta.ActualPosition < last-maxResults {
					streamIDs = append(streamIDs, e.RequestID())
				}
			}
		}
	}

	// truncate all reqs to found end position
	// fmt.Println("TRUNC", queueRequests, int64(endPosition), last)
	span.AddEvent(fmt.Sprint("TRUNC", queueRequests, int64(endPosition), last))
	err = s.es.Truncate(ctx, queueRequests, int64(endPosition))
	if err != nil {
		return err
	}

	// truncate all the request streams
	for _, streamID := range streamIDs {
		s.state.Modify(ctx, func(ctx context.Context, state *state) error {
			return state.ApplyEvents(event.NewEvents(&RequestTruncated{
				RequestID: streamID,
			}))
		})

		err := s.cleanResult(ctx, streamID)
		if err != nil {
			return err
		}
	}

	return nil
}
func (s *service) cleanResult(ctx context.Context, requestID string) error {
	ctx, span := lg.Span(ctx)
	defer span.End()

	streamID := aggRequest(requestID)

	last, err := s.es.LastIndex(ctx, streamID)
	if err != nil {
		return err
	}
	// truncate all reqs to found end position
	// fmt.Println("TRUNC", streamID, last)
	span.AddEvent(fmt.Sprint("TRUNC", streamID, last))
	err = s.es.Truncate(ctx, streamID, int64(last))
	if err != nil {
		return err
	}
	return nil
}
