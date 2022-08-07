package service

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"log"
	"net/http"
	"strconv"
	"strings"

	"github.com/sour-is/ev/pkg/es/driver"
	"github.com/sour-is/ev/pkg/es/event"
)

type service struct {
	es driver.EventStore
}

func New(ctx context.Context, es driver.EventStore) (*service, error) {
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

		m := events.First().EventMeta()
		w.WriteHeader(http.StatusAccepted)
		log.Print("POST topic=", name, " tags=", tags, " idx=", m.Position, " id=", m.EventID)
		fmt.Fprintf(w, "OK %d %s", m.Position, m.EventID)
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
