package msgbus

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/sour-is/ev/pkg/es"
	"github.com/sour-is/ev/pkg/es/event"
)

type service struct {
	es *es.EventStore
}

func New(ctx context.Context, es *es.EventStore) (*service, error) {
	if err := event.Register(ctx, &PostEvent{}); err != nil {
		return nil, err
	}
	return &service{es}, nil
}

func (s *service) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	name, tags, _ := strings.Cut(r.URL.Path, "/")
	if name == "" {
		w.WriteHeader(http.StatusNotFound)
		return
	}

	var first event.Event = event.NilEvent
	if lis, err := s.es.Read(ctx, "post-"+name, 0, 1); err == nil && len(lis) > 0 {
		first = lis[0]
	}

	switch r.Method {
	case http.MethodGet:
		var pos, count int64 = -1, -99
		qry := r.URL.Query()

		if i, err := strconv.ParseInt(qry.Get("idx"), 10, 64); err == nil {
			pos = i
		}
		if i, err := strconv.ParseInt(qry.Get("n"), 10, 64); err == nil {
			count = i
		}

		log.Print("GET topic=", name, " idx=", pos, " n=", count)
		events, err := s.es.Read(ctx, "post-"+name, pos, count)
		if err != nil {
			log.Print(err)
			w.WriteHeader(http.StatusInternalServerError)
			return
		}

		if strings.Contains(r.Header.Get("Accept"), "application/json") {
			w.Header().Add("Content-Type", "application/json")

			if err = encodeJSON(w, first, events); err != nil {
				log.Print(err)

				w.WriteHeader(http.StatusInternalServerError)
				return
			}
			return
		}

		for i := range events {
			fmt.Fprintln(w, events[i])
		}

		return
	case http.MethodPost, http.MethodPut:
		b, err := io.ReadAll(io.LimitReader(r.Body, 64*1024))
		if err != nil {
			log.Print(err)
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		r.Body.Close()

		if name == "" {
			w.WriteHeader(http.StatusNotFound)
			return
		}

		events := event.NewEvents(&PostEvent{
			Payload: b,
			Tags:    fields(tags),
		})
		_, err = s.es.Append(r.Context(), "post-"+name, events)
		if err != nil {
			log.Print(err)

			w.WriteHeader(http.StatusInternalServerError)
			return
		}

		if first == event.NilEvent {
			first = events.First()
		}

		m := events.First().EventMeta()
		log.Print("POST topic=", name, " tags=", tags, " idx=", m.Position, " id=", m.EventID)

		w.WriteHeader(http.StatusAccepted)
		if strings.Contains(r.Header.Get("Accept"), "application/json") {
			w.Header().Add("Content-Type", "application/json")
			if err = encodeJSON(w, first, events); err != nil {
				log.Print(err)

				w.WriteHeader(http.StatusInternalServerError)
				return
			}

			return
		}

		w.Header().Add("Content-Type", "text/plain")
		fmt.Fprintf(w, "OK %d %s", m.Position, m.EventID)
		return
	default:
		w.WriteHeader(http.StatusMethodNotAllowed)
	}
}

type PostEvent struct {
	Payload []byte
	Tags    []string

	eventMeta event.Meta
}

func (e *PostEvent) EventMeta() event.Meta {
	if e == nil {
		return event.Meta{}
	}
	return e.eventMeta
}
func (e *PostEvent) SetEventMeta(eventMeta event.Meta) {
	if e == nil {
		return
	}
	e.eventMeta = eventMeta
}
func (e *PostEvent) String() string {
	var b bytes.Buffer

	// b.WriteString(e.eventMeta.StreamID)
	// b.WriteRune('@')
	b.WriteString(strconv.FormatUint(e.eventMeta.Position, 10))
	b.WriteRune('\t')

	b.WriteString(e.eventMeta.EventID.String())
	b.WriteRune('\t')
	b.WriteString(string(e.Payload))
	if len(e.Tags) > 0 {
		b.WriteRune('\t')
		b.WriteString(strings.Join(e.Tags, ","))
	}

	return b.String()
}
func fields(s string) []string {
	if s == "" {
		return nil
	}
	return strings.Split(s, "/")
}

func encodeJSON(w io.Writer, first event.Event, events event.Events) error {
	out := make([]struct {
		ID      uint64   `json:"id"`
		Payload []byte   `json:"payload"`
		Created string   `json:"created"`
		Tags    []string `json:"tags"`
		Topic   struct {
			Name    string `json:"name"`
			TTL     uint64 `json:"ttl"`
			Seq     uint64 `json:"seq"`
			Created string `json:"created"`
		} `json:"topic"`
	}, len(events))

	for i := range events {
		e, ok := events[i].(*PostEvent)
		if !ok {
			continue
		}
		out[i].ID = e.EventMeta().Position
		out[i].Created = e.EventMeta().Created().Format(time.RFC3339Nano)
		out[i].Payload = e.Payload
		out[i].Tags = e.Tags
		out[i].Topic.Name = e.EventMeta().StreamID
		out[i].Topic.Created = first.EventMeta().Created().Format(time.RFC3339Nano)
		out[i].Topic.Seq = e.EventMeta().Position
	}

	if len(out) == 1 {
		return json.NewEncoder(w).Encode(out[0])
	}

	return json.NewEncoder(w).Encode(out)
}