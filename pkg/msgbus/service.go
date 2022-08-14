package msgbus

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/gorilla/websocket"
	"github.com/sour-is/ev/internal/logz"
	"github.com/sour-is/ev/pkg/es"
	"github.com/sour-is/ev/pkg/es/event"
)

type service struct {
	es *es.EventStore
}

func New(ctx context.Context, es *es.EventStore) (*service, error) {
	ctx, span := logz.Span(ctx)
	defer span.End()

	if err := event.Register(ctx, &PostEvent{}); err != nil {
		return nil, err
	}
	return &service{es}, nil
}

var upgrader = websocket.Upgrader{
	WriteBufferSize: 4096,
	CheckOrigin: func(r *http.Request) bool {
		return true
	},
}

func (s *service) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	ctx, span := logz.Span(ctx)
	defer span.End()
	r = r.WithContext(ctx)

	switch r.Method {
	case http.MethodGet:
		if r.Header.Get("Upgrade") == "websocket" {
			s.websocket(w, r)
			return
		}
		s.get(w, r)
	case http.MethodPost, http.MethodPut:
		s.post(w, r)
	default:
		w.WriteHeader(http.StatusMethodNotAllowed)
	}
}

func (s *service) get(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	ctx, span := logz.Span(ctx)
	defer span.End()

	name, _, _ := strings.Cut(r.URL.Path, "/")
	if name == "" {
		w.WriteHeader(http.StatusNotFound)
		return
	}

	var first event.Event = event.NilEvent
	if lis, err := s.es.Read(ctx, "post-"+name, 0, 1); err == nil && len(lis) > 0 {
		first = lis[0]
	}

	var pos, count int64 = -1, -99
	qry := r.URL.Query()

	if i, err := strconv.ParseInt(qry.Get("index"), 10, 64); err == nil {
		pos = i
	}
	if i, err := strconv.ParseInt(qry.Get("n"), 10, 64); err == nil {
		count = i
	}

	span.AddEvent(fmt.Sprint("GET topic=", name, " idx=", pos, " n=", count))
	events, err := s.es.Read(ctx, "post-"+name, pos, count)
	if err != nil {
		span.RecordError(err)
		w.WriteHeader(http.StatusInternalServerError)
		return
	}

	if strings.Contains(r.Header.Get("Accept"), "application/json") {
		w.Header().Add("Content-Type", "application/json")

		if err = encodeJSON(w, first, events...); err != nil {
			span.RecordError(err)

			w.WriteHeader(http.StatusInternalServerError)
			return
		}
		return
	}

	for i := range events {
		fmt.Fprintln(w, events[i])
	}
}
func (s *service) post(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	ctx, span := logz.Span(ctx)
	defer span.End()

	name, tags, _ := strings.Cut(r.URL.Path, "/")
	if name == "" {
		w.WriteHeader(http.StatusNotFound)
		return
	}

	var first event.Event = event.NilEvent
	if lis, err := s.es.Read(ctx, "post-"+name, 0, 1); err == nil && len(lis) > 0 {
		first = lis[0]
	}

	b, err := io.ReadAll(io.LimitReader(r.Body, 64*1024))
	if err != nil {
		span.RecordError(err)

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

	_, err = s.es.Append(ctx, "post-"+name, events)
	if err != nil {
		span.RecordError(err)

		w.WriteHeader(http.StatusInternalServerError)
		return
	}

	if first == event.NilEvent {
		first = events.First()
	}

	m := events.First().EventMeta()
	span.AddEvent(fmt.Sprint("POST topic=", name, " tags=", tags, " idx=", m.Position, " id=", m.EventID))

	w.WriteHeader(http.StatusAccepted)
	if strings.Contains(r.Header.Get("Accept"), "application/json") {
		w.Header().Add("Content-Type", "application/json")
		if err = encodeJSON(w, first, events...); err != nil {
			span.RecordError(err)

			w.WriteHeader(http.StatusInternalServerError)
			return
		}

		return
	}
	span.AddEvent("finish response")

	w.Header().Add("Content-Type", "text/plain")
	fmt.Fprintf(w, "OK %d %s", m.Position, m.EventID)
}
func (s *service) websocket(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	ctx, span := logz.Span(ctx)
	defer span.End()

	name, _, _ := strings.Cut(r.URL.Path, "/")
	if name == "" {
		w.WriteHeader(http.StatusNotFound)
		return
	}

	var first event.Event = event.NilEvent
	if lis, err := s.es.Read(ctx, "post-"+name, 0, 1); err == nil && len(lis) > 0 {
		first = lis[0]
	}

	var pos int64 = -1
	qry := r.URL.Query()

	if i, err := strconv.ParseInt(qry.Get("index"), 10, 64); err == nil {
		pos = i - 1
	}

	span.AddEvent(fmt.Sprint("WS topic=", name, " idx=", pos))

	c, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		span.RecordError(err)
		return
	}
	defer c.Close()

	ctx, cancel := context.WithCancel(ctx)
	c.SetCloseHandler(func(code int, text string) error {
		cancel()
		return nil
	})
	go func() {
		for {
			if err := ctx.Err(); err != nil {
				return
			}
			mt, message, err := c.ReadMessage()
			if err != nil {
				span.RecordError(err)
				return
			}
			span.AddEvent(fmt.Sprintf("recv: %d %s", mt, message))
		}
	}()

	es := s.es.EventStream()
	if es == nil {
		span.AddEvent(fmt.Sprint("EventStore does not implement streaming"))
		w.WriteHeader(http.StatusInternalServerError)
		return
	}

	sub, err := es.Subscribe(ctx, "post-"+name, pos)
	if err != nil {
		span.RecordError(err)
		return
	}
	defer func() {
		ctx, cancel := context.WithTimeout(context.Background(), 1*time.Second)
		defer cancel()
		span.AddEvent(fmt.Sprint("stop ws"))
		sub.Close(ctx)
	}()

	span.AddEvent(fmt.Sprint("start ws"))
	for sub.Recv(ctx) {
		events, err := sub.Events(ctx)
		if err != nil {
			break
		}
		span.AddEvent(fmt.Sprint("got events ", len(events)))
		for i := range events {
			e, ok := events[i].(*PostEvent)
			if !ok {
				continue
			}
			span.AddEvent(fmt.Sprint("send", i, e.String()))

			var b bytes.Buffer
			if err = encodeJSON(&b, first, e); err != nil {
				span.RecordError(err)
			}

			err = c.WriteMessage(websocket.TextMessage, b.Bytes())
			if err != nil {
				span.RecordError(err)
				break
			}
		}
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
func (e *PostEvent) MarshalText() ([]byte, error) {
	return json.Marshal(e)
}
func (e *PostEvent) UnmarshalText(b []byte) error {
	return json.Unmarshal(b, e)
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

func encodeJSON(w io.Writer, first event.Event, events ...event.Event) error {
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
		out[i].Topic.Name = strings.TrimPrefix(e.EventMeta().StreamID, "post-")
		out[i].Topic.Created = first.EventMeta().Created().Format(time.RFC3339Nano)
		out[i].Topic.Seq = e.EventMeta().Position
	}

	if len(out) == 1 {
		return json.NewEncoder(w).Encode(out[0])
	}

	return json.NewEncoder(w).Encode(out)
}
