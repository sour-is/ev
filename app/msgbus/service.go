package msgbus

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"hash/fnv"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/gorilla/websocket"
	"go.opentelemetry.io/otel/metric"
	"go.uber.org/multierr"

	"go.sour.is/ev"
	"go.sour.is/ev/pkg/event"

	"go.sour.is/pkg/gql"
	"go.sour.is/pkg/lg"
)

type service struct {
	es *ev.EventStore

	m_gql_posts            metric.Int64Counter
	m_gql_post_added       metric.Int64Counter
	m_gql_post_added_event metric.Int64Counter
	m_req_time             metric.Int64Histogram
}

type MsgbusResolver interface {
	Posts(ctx context.Context, name, tag string, paging *gql.PageInput) (*gql.Connection, error)
	PostAdded(ctx context.Context, name, tag string, after int64) (<-chan *PostEvent, error)
	IsResolver()
}

func New(ctx context.Context, es *ev.EventStore) (*service, error) {
	ctx, span := lg.Span(ctx)
	defer span.End()

	if err := event.Register(ctx, &PostEvent{}); err != nil {
		return nil, err
	}
	if err := event.RegisterName(ctx, "domain.PostEvent", &PostEvent{}); err != nil {
		return nil, err
	}

	m := lg.Meter(ctx)

	svc := &service{es: es}

	var err, errs error
	svc.m_gql_posts, err = m.Int64Counter("msgbus_posts",
		metric.WithDescription("msgbus graphql posts requests"),
	)
	errs = multierr.Append(errs, err)

	svc.m_gql_post_added, err = m.Int64Counter("msgbus_post_added",
		metric.WithDescription("msgbus graphql post added subcription requests"),
	)
	errs = multierr.Append(errs, err)

	svc.m_gql_post_added_event, err = m.Int64Counter("msgbus_post_event",
		metric.WithDescription("msgbus graphql post added subscription events"),
	)
	errs = multierr.Append(errs, err)

	svc.m_req_time, err = m.Int64Histogram("msgbus_request_time",
		metric.WithDescription("msgbus graphql post added subscription events"),
		metric.WithUnit("ns"),
	)
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

func (s *service) IsResolver() {}
func (s *service) RegisterHTTP(mux *http.ServeMux) {
	mux.Handle("/inbox/", lg.Htrace(http.StripPrefix("/inbox/", s), "inbox"))
}
func (s *service) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	ctx, span := lg.Span(ctx)
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
func (s *service) Posts(ctx context.Context, name, tag string, paging *gql.PageInput) (*gql.Connection, error) {
	ctx, span := lg.Span(ctx)
	defer span.End()

	s.m_gql_posts.Add(ctx, 1)

	start := time.Now()
	defer s.m_req_time.Record(ctx, time.Since(start).Milliseconds())

	streamID := withTag("post-"+name, tag)
	lis, err := s.es.Read(ctx, streamID, paging.GetIdx(0), paging.GetCount(30))
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
	if first, err = s.es.FirstIndex(ctx, streamID); err != nil {
		span.RecordError(err)
		return nil, err
	}
	if last, err = s.es.LastIndex(ctx, streamID); err != nil {
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

func (r *service) PostAdded(ctx context.Context, name, tag string, after int64) (<-chan *PostEvent, error) {
	ctx, span := lg.Span(ctx)
	defer span.End()

	r.m_gql_post_added.Add(ctx, 1)

	es := r.es.EventStream()
	if es == nil {
		return nil, fmt.Errorf("EventStore does not implement streaming")
	}

	streamID := withTag("post-"+name, tag)

	sub, err := es.Subscribe(ctx, streamID, after)
	if err != nil {
		span.RecordError(err)
		return nil, err
	}

	ch := make(chan *PostEvent)

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
			r.m_gql_post_added_event.Add(ctx, int64(len(events)))

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

	ctx, span := lg.Span(ctx)
	defer span.End()

	start := time.Now()
	defer s.m_req_time.Record(ctx, time.Since(start).Milliseconds())

	name, tag, _ := strings.Cut(r.URL.Path, "/")
	if name == "" {
		w.WriteHeader(http.StatusNotFound)
		return
	}
	streamID := withTag("post-"+name, tag)

	var first event.Event = event.NilEvent
	if lis, err := s.es.Read(ctx, streamID, 0, 1); err == nil && len(lis) > 0 {
		first = lis[0]
	}

	var pos, count int64 = 0, ev.AllEvents
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

	span.AddEvent(fmt.Sprint("GET topic=", streamID, " idx=", pos, " n=", count))
	events, err := s.es.Read(ctx, streamID, pos, count)
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

	ctx, span := lg.Span(ctx)
	defer span.End()

	start := time.Now()
	defer s.m_req_time.Record(ctx, time.Since(start).Milliseconds())

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
	ctx, span := lg.Span(ctx)
	defer span.End()

	name, tag, _ := strings.Cut(r.URL.Path, "/")
	if name == "" {
		w.WriteHeader(http.StatusNotFound)
		return
	}

	streamID := withTag("post-"+name, tag)

	var first event.Event = event.NilEvent
	if lis, err := s.es.Read(ctx, streamID, 0, 1); err == nil && len(lis) > 0 {
		first = lis[0]
	}

	var pos int64 = 0
	qry := r.URL.Query()

	if i, err := strconv.ParseInt(qry.Get("index"), 10, 64); err == nil && i > 0 {
		pos = i - 1
	}

	span.AddEvent(fmt.Sprint("WS topic=", streamID, " idx=", pos))

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

	sub, err := es.Subscribe(ctx, streamID, pos)
	if err != nil {
		span.RecordError(err)
		return
	}
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

	span.AddEvent("start ws")
	for <-sub.Recv(ctx) {
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

	event.IsEvent
}

func (e *PostEvent) Values() any {
	if e == nil {
		return nil
	}

	return struct {
		Payload []byte   `json:"payload"`
		Tags    []string `json:"tags,omitempty"`
	}{
		Payload: e.payload,
		Tags:    e.tags,
	}
}
func (e *PostEvent) MarshalBinary() ([]byte, error) {
	j := e.Values()
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

func (e *PostEvent) ID() string      { return e.EventMeta().GetEventID() }
func (e *PostEvent) Tags() []string  { return e.tags }
func (e *PostEvent) Payload() string { return string(e.payload) }
func (e *PostEvent) PayloadJSON(ctx context.Context) (m map[string]interface{}, err error) {
	err = json.Unmarshal([]byte(e.payload), &m)
	return
}
func (e *PostEvent) Meta() event.Meta { return e.EventMeta() }
func (e *PostEvent) IsEdge()          {}

func (e *PostEvent) String() string {
	var b bytes.Buffer

	b.WriteString(strconv.FormatUint(e.EventMeta().Position, 10))
	b.WriteRune('\t')

	b.WriteString(e.EventMeta().EventID.String())
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
	return strings.Split(s, ";")
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

func Projector(e event.Event) []event.Event {
	m := e.EventMeta()
	streamID := m.StreamID
	streamPos := m.Position

	switch e := e.(type) {
	case *PostEvent:
		lis := make([]event.Event, len(e.tags))
		for i := range lis {
			tag := e.tags[i]
			ne := event.NewPtr(streamID, streamPos)
			event.SetStreamID(withTag(streamID, tag), ne)
			lis[i] = ne
		}

		return lis
	}
	return nil
}
func withTag(id, tag string) string {
	if tag == "" {
		return id
	}

	h := fnv.New128a()
	fmt.Fprint(h, tag)
	return id + "-" + base64.RawURLEncoding.EncodeToString(h.Sum(nil))
}
