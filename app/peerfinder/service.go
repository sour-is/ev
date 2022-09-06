package peerfinder

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strconv"
	"strings"

	ulid "github.com/oklog/ulid/v2"
	contentnegotiation "gitlab.com/jamietanna/content-negotiation-go"

	"github.com/sour-is/ev/internal/lg"
	"github.com/sour-is/ev/pkg/es"
	"github.com/sour-is/ev/pkg/es/event"
)

const (
	queueRequests  = "pf-requests"
	queueResponses = "pf-response-"
	aggInfo        = "pf-info"
	initVersion    = "1.1.0"
)

type service struct {
	es *es.EventStore
}

func New(ctx context.Context, es *es.EventStore) (*service, error) {
	ctx, span := lg.Span(ctx)
	defer span.End()

	if err := event.Register(ctx, &Request{}, &Result{}, &VersionChanged{}); err != nil {
		span.RecordError(err)
		return nil, err
	}

	svc := &service{es: es}

	return svc, nil
}
func (s *service) RegisterHTTP(mux *http.ServeMux) {
	mux.Handle("/peers/", lg.Htrace(s, "peers"))

}
func (s *service) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	_, span := lg.Span(ctx)
	defer span.End()

	r = r.WithContext(ctx)

	switch r.Method {
	case http.MethodGet:
		switch {
		case strings.HasPrefix(r.URL.Path, "/peers/pending/"):
			s.getPending(w, r, strings.TrimPrefix(r.URL.Path, "/peers/pending/"))
			return

		case strings.HasPrefix(r.URL.Path, "/peers/req/"):
			s.getResults(w, r, strings.TrimPrefix(r.URL.Path, "/peers/req/"))
			return

		default:
			w.WriteHeader(http.StatusNotFound)
			return
		}
	case http.MethodPost:
		switch {
		case strings.HasPrefix(r.URL.Path, "/peers/req/"):
			s.postResult(w, r, strings.TrimPrefix(r.URL.Path, "/peers/req/"))
			return

		case strings.HasPrefix(r.URL.Path, "/peers/req"):
			s.postRequest(w, r)
			return

		default:
			w.WriteHeader(http.StatusNotFound)
			return
		}
	default:
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
}

func (s *service) getPending(w http.ResponseWriter, r *http.Request, uuid string) {
	ctx := r.Context()

	_, span := lg.Span(ctx)
	defer span.End()

	info, err := es.Upsert(ctx, s.es, "pf-info", func(ctx context.Context, agg *Info) error {
		return agg.OnCreate() // initialize if not exists
	})
	if err != nil {
		span.RecordError(err)
		w.WriteHeader(http.StatusInternalServerError)
		return
	}

	requests, err := s.es.Read(ctx, queueRequests, -1, -30)
	if err != nil {
		span.RecordError(err)
		w.WriteHeader(http.StatusInternalServerError)
		return
	}

	responses, err := s.es.Read(ctx, queueResponses+uuid, -1, -30)
	if err != nil {
		span.RecordError(err)
		w.WriteHeader(http.StatusInternalServerError)
		return
	}

	req := filter(requests, responses)

	negotiator := contentnegotiation.NewNegotiator("application/json", "text/environment", "text/plain", "text/html")
	negotiated, _, err := negotiator.Negotiate(r.Header.Get("Accept"))
	if err != nil {
		span.RecordError(err)
		w.WriteHeader(http.StatusNotAcceptable)
		return
	}

	span.AddEvent(negotiated.String())
	switch negotiated.String() {
	case "text/environment":
		_, err = encodeTo(w, info.MarshalEnviron, req.MarshalEnviron)
	case "application/json":
		var out interface{} = info
		if req != nil {
			out = struct {
				ScriptVersion string `json:"script_version"`
				RequestID     string `json:"req_id"`
				RequestIP     string `json:"req_ip"`
				Family        string `json:"req_family"`
				Created       string `json:"req_created"`
			}{
				info.ScriptVersion,
				req.RequestID(),
				req.RequestIP,
				strconv.Itoa(req.Family()),
				req.CreatedString(),
			}
		}
		err = json.NewEncoder(w).Encode(out)
	}
	span.RecordError(err)
}
func (s *service) getResults(w http.ResponseWriter, r *http.Request, uuid string) {
	ctx := r.Context()

	responses, err := s.es.Read(ctx, queueResponses+uuid, -1, es.AllEvents)
	if err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		return
	}

	negotiator := contentnegotiation.NewNegotiator("application/json", "text/environment", "text/csv", "text/plain", "text/html")
	negotiated, _, err := negotiator.Negotiate("application/json")
	if err != nil {
		w.WriteHeader(http.StatusNotAcceptable)
		return
	}
	switch negotiated.String() {
	// case "text/environment":
	// 	encodeTo(w, responses.MarshalBinary)
	case "application/json":
		json.NewEncoder(w).Encode(responses)
	}
}
func (s *service) postRequest(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	if err := r.ParseForm(); err != nil {
		w.WriteHeader(http.StatusUnprocessableEntity)
		return
	}

	req := &Request{
		RequestIP: r.Form.Get("req_ip"),
	}

	if hidden, err := strconv.ParseBool(r.Form.Get("req_hidden")); err != nil {
		req.Hidden = hidden
	}

	s.es.Append(ctx, queueRequests, event.NewEvents(req))
}
func (s *service) postResult(w http.ResponseWriter, r *http.Request, id string) {
	ctx := r.Context()

	if _, err := ulid.ParseStrict(id); err != nil {
		w.WriteHeader(http.StatusNotFound)
		return
	}

	if err := r.ParseForm(); err != nil {
		w.WriteHeader(http.StatusUnprocessableEntity)
		return
	}

	latency, err := strconv.ParseFloat(r.Form.Get("res_latency"), 64)
	if err != nil {
		w.WriteHeader(http.StatusUnprocessableEntity)
		return
	}

	req := &Result{
		RequestID:   id,
		PeerID:      r.Form.Get("peer_id"),
		PeerVersion: r.Form.Get("peer_version"),
		Latency:     latency,
	}

	s.es.Append(ctx, queueResponses+id, event.NewEvents(req))
}

func filter(requests, responses event.Events) *Request {
	have := make(map[string]struct{}, len(responses))
	for _, res := range toList[Result](responses...) {
		have[res.RequestID] = struct{}{}
	}
	for _, req := range reverse(toList[Request](requests...)...) {
		if _, ok := have[req.RequestID()]; !ok {
			return req
		}
	}
	return nil
}
func toList[E any, T es.PE[E]](lis ...event.Event) []T {
	newLis := make([]T, 0, len(lis))
	for i := range lis {
		if e, ok := lis[i].(T); ok {
			newLis = append(newLis, e)
		}
	}
	return newLis
}
func reverse[T any](s ...T) []T {
	first, last := 0, len(s)-1
	for first < last {
		s[first], s[last] = s[last], s[first]
		first++
		last--
	}
	return s
}
func encodeTo(w io.Writer, fns ...func() ([]byte, error)) (int, error) {
	i := 0
	for _, fn := range fns {
		b, err := fn()
		if err != nil {
			return i, err
		}

		j, err := w.Write(b)
		i += j
		if err != nil {
			return i, err
		}
	}
	return i, nil
}
