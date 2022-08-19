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
	"github.com/sour-is/ev/pkg/gql"
	"go.opentelemetry.io/otel/metric/instrument/syncint64"
	"go.uber.org/multierr"
)

type service struct {
	es *es.EventStore

	Mresolver_posts            syncint64.Counter
	Mresolver_post_added       syncint64.Counter
	Mresolver_post_added_event syncint64.Counter
}

type MsgbusResolver interface {
	Posts(ctx context.Context, streamID string, paging *gql.PageInput) (*gql.Connection, error)
	PostAdded(ctx context.Context, streamID string, after int64) (<-chan *PostEvent, error)
}

func New(ctx context.Context, es *es.EventStore) (*service, error) {
	ctx, span := logz.Span(ctx)
	defer span.End()

	if err := event.Register(ctx, &PostEvent{}); err != nil {
		return nil, err
	}
	if err := event.RegisterName(ctx, "domain.PostEvent", &PostEvent{}); err != nil {
		return nil, err
	}

	m := logz.Meter(ctx)

	svc := &service{es: es}

	var err, errs error
	svc.Mresolver_posts, err = m.SyncInt64().Counter("resolver_posts")
	errs = multierr.Append(errs, err)

	svc.Mresolver_post_added, err = m.SyncInt64().Counter("resolver_post_added")
	errs = multierr.Append(errs, err)

	svc.Mresolver_post_added_event, err = m.SyncInt64().Counter("resolver_post_added")
	errs = multierr.Append(errs, err)

	span.RecordError(err)

	return svc, errs
}

var upgrader = websocket.Upgrader{
	WriteBufferSize: 4096,
	CheckOrigin: func(r *http.Request) bool {
		return true
	},
}

func (s *service) RegisterHTTP(mux *http.ServeMux) {
	mux.Handle("/inbox/", logz.Htrace(http.StripPrefix("/inbox/", s), "inbox"))
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

// Posts is the resolver for the events field.
func (r *service) Posts(ctx context.Context, streamID string, paging *gql.PageInput) (*gql.Connection, error) {
	ctx, span := logz.Span(ctx)
	defer span.End()

	r.Mresolver_posts.Add(ctx, 1)

	lis, err := r.es.Read(ctx, streamID, paging.GetIdx(0), paging.GetCount(30))
	if err != nil {
		span.RecordError(err)
		return nil, err
	}

	edges := make([]gql.Edge, 0, len(lis))
	for i := range lis {
		span.AddEvent(fmt.Sprint("post ", i, " of ", len(lis)))
		e := lis[i]

		post, ok := e.(*PostEvent)
		if !ok {
			continue
		}

		edges = append(edges, post)
	}

	var first, last uint64
	if first, err = r.es.FirstIndex(ctx, streamID); err != nil {
		span.RecordError(err)
		return nil, err
	}
	if last, err = r.es.LastIndex(ctx, streamID); err != nil {
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

func (r *service) PostAdded(ctx context.Context, streamID string, after int64) (<-chan *PostEvent, error) {
	ctx, span := logz.Span(ctx)
	defer span.End()

	r.Mresolver_post_added.Add(ctx, 1)

	es := r.es.EventStream()
	if es == nil {
		return nil, fmt.Errorf("EventStore does not implement streaming")
	}

	sub, err := es.Subscribe(ctx, streamID, after)
	if err != nil {
		span.RecordError(err)
		return nil, err
	}

	ch := make(chan *PostEvent)

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
			r.Mresolver_post_added_event.Add(ctx, int64(len(events)))

			for _, e := range events {
				if p, ok := e.(*PostEvent); ok {
					select {
					case ch <- p:
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

	var pos, count int64 = 0, es.AllEvents
	qry := r.URL.Query()

	if i, err := strconv.ParseInt(qry.Get("index"), 10, 64); err == nil && i > 1 {
		pos = i - 1
	}
	if i, err := strconv.ParseInt(qry.Get("pos"), 10, 64); err == nil {
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
		payload: b,
		tags:    fields(tags),
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

	var pos int64 = 0
	qry := r.URL.Query()

	if i, err := strconv.ParseInt(qry.Get("index"), 10, 64); err == nil && i > 0 {
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
		span.AddEvent("EventStore does not implement streaming")
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
		span.AddEvent("stop ws")
		sub.Close(ctx)
	}()

	span.AddEvent("start ws")
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
	payload []byte
	tags    []string

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
func (e *PostEvent) MarshalBinary() ([]byte, error) {
	j := struct {
		Payload []byte
		Tags    []string
	}{
		Payload: e.payload,
		Tags:    e.tags,
	}
	return json.Marshal(&j)
}
func (e *PostEvent) UnmarshalBinary(b []byte) error {
	j := struct {
		Payload []byte
		Tags    []string
	}{}
	err := json.Unmarshal(b, &j)
	e.payload = j.Payload
	e.tags = j.Tags

	return err
}
func (e *PostEvent) MarshalJSON() ([]byte, error) { return e.MarshalBinary() }
func (e *PostEvent) UnmarshalJSON(b []byte) error { return e.UnmarshalBinary(b) }

func (e *PostEvent) ID() string      { return e.eventMeta.GetEventID() }
func (e *PostEvent) Tags() []string  { return e.tags }
func (e *PostEvent) Payload() string { return string(e.payload) }
func (e *PostEvent) PayloadJSON(ctx context.Context) (m map[string]interface{}, err error) {
	err = json.Unmarshal([]byte(e.payload), &m)
	return
}
func (e *PostEvent) Meta() *event.Meta { return &e.eventMeta }
func (e *PostEvent) IsEdge()           {}

func (e *PostEvent) String() string {
	var b bytes.Buffer

	// b.WriteString(e.eventMeta.StreamID)
	// b.WriteRune('@')
	b.WriteString(strconv.FormatUint(e.eventMeta.Position, 10))
	b.WriteRune('\t')

	b.WriteString(e.eventMeta.EventID.String())
	b.WriteRune('\t')
	b.WriteString(string(e.payload))
	if len(e.tags) > 0 {
		b.WriteRune('\t')
		b.WriteString(strings.Join(e.tags, ","))
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
		out[i].Payload = e.payload
		out[i].Tags = e.tags
		out[i].Topic.Name = strings.TrimPrefix(e.EventMeta().StreamID, "post-")
		out[i].Topic.Created = first.EventMeta().Created().Format(time.RFC3339Nano)
		out[i].Topic.Seq = e.EventMeta().Position
	}

	if len(out) == 1 {
		return json.NewEncoder(w).Encode(out[0])
	}

	return json.NewEncoder(w).Encode(out)
}
