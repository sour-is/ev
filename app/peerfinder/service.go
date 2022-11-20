package peerfinder

import (
	"context"
	"embed"
	"encoding/json"
	"errors"
	"fmt"
	"html/template"
	"io"
	"io/fs"
	"log"
	"net"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/oklog/ulid"
	contentnegotiation "gitlab.com/jamietanna/content-negotiation-go"
	"go.opentelemetry.io/otel/attribute"

	"github.com/sour-is/ev/internal/lg"
	"github.com/sour-is/ev/pkg/es"
	"github.com/sour-is/ev/pkg/es/event"
	"github.com/sour-is/ev/pkg/locker"
	"github.com/sour-is/ev/pkg/math"
	"github.com/sour-is/ev/pkg/set"
)

const (
	aggInfo       = "pf-info"
	queueRequests = "pf-requests"
	queueResults  = "pf-results"
	initVersion   = "1.2.1"
)

func aggRequest(id string) string { return "pf-request-" + id }
func aggPeer(id string) string    { return "pf-peer-" + id }

var (
	//go:embed pages/* layouts/* assets/*
	files     embed.FS
	templates map[string]*template.Template
)

type service struct {
	es        *es.EventStore
	statusURL string

	state *locker.Locked[state]
}

type state struct {
	peers map[string]*Peer
}

func New(ctx context.Context, es *es.EventStore, statusURL string) (*service, error) {
	ctx, span := lg.Span(ctx)
	defer span.End()

	loadTemplates()

	if err := event.Register(ctx, &RequestSubmitted{}, &ResultSubmitted{}, &VersionChanged{}); err != nil {
		span.RecordError(err)
		return nil, err
	}

	svc := &service{es: es, statusURL: statusURL, state: locker.New(&state{peers: make(map[string]*Peer)})}

	return svc, nil
}
func (s *service) RegisterHTTP(mux *http.ServeMux) {
	a, _ := fs.Sub(files, "assets")
	assets := http.StripPrefix("/peers/assets/", http.FileServer(http.FS(a)))

	mux.Handle("/peers/assets/", lg.Htrace(assets, "peer-assets"))
	mux.Handle("/peers/", lg.Htrace(s, "peers"))
}
func (s *service) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	ctx, span := lg.Span(ctx)
	defer span.End()

	r = r.WithContext(ctx)

	switch r.Method {
	case http.MethodGet:
		switch {
		case strings.HasPrefix(r.URL.Path, "/peers/pending/"):
			s.getPending(w, r, strings.TrimPrefix(r.URL.Path, "/peers/pending/"))
			return

		case strings.HasPrefix(r.URL.Path, "/peers/req/"):
			s.getResultsForRequest(w, r, strings.TrimPrefix(r.URL.Path, "/peers/req/"))
			return

		case strings.HasPrefix(r.URL.Path, "/peers/status"):
			s.state.Modify(r.Context(), func(ctx context.Context, state *state) error {
				for id, p := range state.peers {
					fmt.Fprintln(w, "PEER: ", id, p.Owner, p.Name)
				}

				return nil
			})

		default:
			s.getResults(w, r)
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
	ctx, span := lg.Span(r.Context())
	defer span.End()

	span.SetAttributes(
		attribute.String("uuid", uuid),
	)

	var peer *Peer
	err := s.state.Modify(ctx, func(ctx context.Context, state *state) error {
		var ok bool
		if peer, ok = state.peers[uuid]; !ok {
			return fmt.Errorf("peer not found: %s", uuid)
		}

		return nil
	})
	if err != nil {
		span.RecordError(err)
		w.WriteHeader(http.StatusNotFound)
		return
	}

	info, err := es.Upsert(ctx, s.es, aggInfo, func(ctx context.Context, agg *Info) error {
		return agg.OnUpsert() // initialize if not exists
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

	responses, err := s.es.Read(ctx, aggPeer(uuid), -1, -30)
	if err != nil {
		span.RecordError(err)
		w.WriteHeader(http.StatusInternalServerError)
		return
	}

	span.AddEvent(fmt.Sprintf("req = %d, res = %d", len(requests), len(responses)))

	req := filter(peer, requests, responses)

	negotiator := contentnegotiation.NewNegotiator("application/json", "text/environment", "text/plain", "text/html")
	negotiated, _, err := negotiator.Negotiate(r.Header.Get("Accept"))
	if err != nil {
		span.RecordError(err)
		w.WriteHeader(http.StatusNotAcceptable)
		return
	}

	span.AddEvent(negotiated.String())
	mime := negotiated.String()
	switch mime {
	case "text/environment":
		w.Header().Set("content-type", negotiated.String())
		_, err = encodeTo(w, info.MarshalEnviron, req.MarshalEnviron)
	case "application/json":
		w.Header().Set("content-type", negotiated.String())
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
func (s *service) getResults(w http.ResponseWriter, r *http.Request) {
	ctx, span := lg.Span(r.Context())
	defer span.End()

	events, err := s.es.Read(ctx, queueRequests, -1, -30)
	if err != nil {
		span.RecordError(err)
		w.WriteHeader(http.StatusInternalServerError)
		return
	}

	requests := make([]*Request, len(events))
	for i, req := range events {
		if req, ok := req.(*RequestSubmitted); ok {
			requests[i], err = s.loadResult(ctx, req.RequestID())
			if err != nil {
				span.RecordError(err)
				w.WriteHeader(http.StatusInternalServerError)
				return
			}
		}
	}

	args := requestArgs(r)
	args.Requests = requests

	s.state.Modify(ctx, func(ctx context.Context, state *state) error {
		args.CountPeers = len(state.peers)
		return nil
	})

	t := templates["home.tpl"]
	t.Execute(w, args)
}
func (s *service) getResultsForRequest(w http.ResponseWriter, r *http.Request, uuid string) {
	ctx, span := lg.Span(r.Context())
	defer span.End()

	span.SetAttributes(
		attribute.String("uuid", uuid),
	)

	request, err := s.loadResult(ctx, uuid)
	if err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		return
	}

	negotiator := contentnegotiation.NewNegotiator("application/json", "text/environment", "text/csv", "text/plain", "text/html")
	negotiated, _, err := negotiator.Negotiate(r.Header.Get("Accept"))
	if err != nil {
		w.WriteHeader(http.StatusNotAcceptable)
		return
	}
	span.AddEvent(negotiated.String())
	switch negotiated.String() {
	// case "text/environment":
	// 	encodeTo(w, responses.MarshalBinary)
	case "application/json":
		json.NewEncoder(w).Encode(request)
		return
	default:
		args := requestArgs(r)
		args.Requests = append(args.Requests, request)
		span.AddEvent(fmt.Sprint(args))
		err := renderTo(w, "req.tpl", args)
		span.RecordError(err)

		return
	}
}
func (s *service) postRequest(w http.ResponseWriter, r *http.Request) {
	ctx, span := lg.Span(r.Context())
	defer span.End()

	if err := r.ParseForm(); err != nil {
		w.WriteHeader(http.StatusUnprocessableEntity)
		return
	}

	args := requestArgs(r)
	requestIP := args.RemoteIP

	if ip := r.Form.Get("req_ip"); ip != "" {
		requestIP = ip
	}

	ip := net.ParseIP(requestIP)
	if ip == nil {
		w.WriteHeader(http.StatusBadRequest)
		return
	}
	req := &RequestSubmitted{
		RequestIP: ip.String(),
	}
	if hidden, err := strconv.ParseBool(r.Form.Get("req_hidden")); err != nil {
		req.Hidden = hidden
	}

	span.SetAttributes(
		attribute.Stringer("req_ip", ip),
	)

	s.es.Append(ctx, queueRequests, event.NewEvents(req))

	http.Redirect(w, r, "/peers/req/"+req.RequestID(), http.StatusSeeOther)
}
func (s *service) postResult(w http.ResponseWriter, r *http.Request, id string) {
	ctx, span := lg.Span(r.Context())
	defer span.End()

	span.SetAttributes(
		attribute.String("id", id),
	)

	if _, err := ulid.ParseStrict(id); err != nil {
		w.WriteHeader(http.StatusNotFound)
		return
	}

	if err := r.ParseForm(); err != nil {
		w.WriteHeader(http.StatusUnprocessableEntity)
		return
	}

	peerID := r.Form.Get("peer_id")
	err := s.state.Modify(ctx, func(ctx context.Context, state *state) error {
		var ok bool
		if _, ok = state.peers[peerID]; !ok {
			// fmt.Printf("peer not found: %s\n", peerID)
			return fmt.Errorf("peer not found: %s", peerID)
		}

		return nil
	})
	if err != nil {
		span.RecordError(err)
		w.WriteHeader(http.StatusNotFound)
		return
	}

	var unreach bool
	latency, err := strconv.ParseFloat(r.Form.Get("res_latency"), 64)
	if err != nil {
		unreach = true
	}

	req := &ResultSubmitted{
		RequestID:   id,
		PeerID:      r.Form.Get("peer_id"),
		PeerVersion: r.Form.Get("peer_version"),
		Latency:     latency,
		Unreachable: unreach,
	}

	if jitter, err := strconv.ParseFloat(r.Form.Get("res_jitter"), 64); err == nil {
		req.Jitter = jitter
	} else {
		span.RecordError(err)
	}
	if minrtt, err := strconv.ParseFloat(r.Form.Get("res_minrtt"), 64); err == nil {
		req.MinRTT = minrtt
	} else {
		span.RecordError(err)
	}
	if maxrtt, err := strconv.ParseFloat(r.Form.Get("res_maxrtt"), 64); err == nil {
		req.MaxRTT = maxrtt
	} else {
		span.RecordError(err)
	}

	span.SetAttributes(
		attribute.Stringer("result", req),
	)

	s.state.Modify(ctx, func(ctx context.Context, state *state) error {

		return nil
	})

	idx, err := s.es.LastIndex(ctx, aggRequest(id))
	if err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		return
	}
	if idx == 0 {
		w.WriteHeader(http.StatusNotFound)
		return
	}

	s.es.Append(ctx, queueResults, event.NewEvents(req))
}
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

	span.AddEvent(fmt.Sprintf("processed %d peers", len(peers)))

	err = s.state.Modify(ctx, func(ctx context.Context, t *state) error {
		for _, peer := range peers {
			t.peers[peer.ID] = peer
		}

		return nil
	})
	span.RecordError(err)
	return err
}
func (s *service) CleanJob(ctx context.Context, now time.Time) error {
	ctx, span := lg.Span(ctx)
	defer span.End()

	fmt.Println("clear peerfinder requests")
	span.AddEvent("clear peerfinder requests")

	endRequestID, err := s.cleanRequests(ctx, now)
	if err != nil {
		return err
	}
	if err = s.cleanResults(ctx, endRequestID); err != nil {
		return err
	}

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
		newFirst := math.Max(math.Max(int64(last-30), int64(first)), 1)
		if last == 0 || newFirst == int64(first) {
			// fmt.Println("SKIP", streamID, first, newFirst, last)
			span.AddEvent(fmt.Sprint("SKIP", streamID, first, newFirst, last))
			continue
		}
		// fmt.Println("TRUNC", streamID, first, newFirst, last)
		span.AddEvent(fmt.Sprint("TRUNC", streamID, first, newFirst, last))
		err = s.es.Truncate(ctx, streamID, int64(newFirst))
		if err != nil {
			return err
		}
	}

	return nil
}
func (s *service) cleanRequests(ctx context.Context, now time.Time) (string, error) {
	ctx, span := lg.Span(ctx)
	defer span.End()

	var streamIDs []string
	var endPosition uint64
	var endRequestID string

	last, err := s.es.LastIndex(ctx, queueRequests)
	if err != nil {
		return "", err
	}

end:
	for {
		events, err := s.es.Read(ctx, queueRequests, int64(endPosition), 1000) // read 1000 from the top each loop.
		if err != nil && !errors.Is(err, es.ErrNotFound) {
			span.RecordError(err)
			return "", err
		}

		if len(events) == 0 {
			break
		}

		endPosition = events.Last().EventMeta().ActualPosition
		for _, event := range events {
			switch e := event.(type) {
			case *RequestSubmitted:
				if e.eventMeta.ActualPosition < last-30 || e.Created().Before(now.Add(-24*time.Hour)) {
					streamIDs = append(streamIDs, aggRequest(e.RequestID()))
				} else {
					endRequestID = e.RequestID()
					endPosition = e.eventMeta.ActualPosition
					break end
				}
			}
		}
	}

	// truncate all reqs to found end position
	// fmt.Println("TRUNC", queueRequests, int64(endPosition), last)
	span.AddEvent(fmt.Sprint("TRUNC", queueRequests, int64(endPosition), last))
	err = s.es.Truncate(ctx, queueRequests, int64(endPosition))
	if err != nil {
		return "", err
	}

	// truncate all the request streams
	for _, streamID := range streamIDs {
		last, err := s.es.LastIndex(ctx, streamID)
		if err != nil {
			return "", err
		}
		fmt.Println("TRUNC", streamID, last)
		span.AddEvent(fmt.Sprint("TRUNC", streamID, last))
		err = s.es.Truncate(ctx, streamID, int64(last))
		if err != nil {
			return "", err
		}
	}

	return endRequestID, nil
}
func (s *service) cleanResults(ctx context.Context, endRequestID string) error {
	ctx, span := lg.Span(ctx)
	defer span.End()

	var endPosition uint64

	done := false
	for !done {
		events, err := s.es.Read(ctx, queueResults, int64(endPosition), 1000) // read 30 from the top each loop.
		if err != nil {
			return err
		}

		if len(events) == 0 {
			done = true
			continue
		}

		endPosition = events.Last().EventMeta().ActualPosition

		for _, event := range events {
			switch e := event.(type) {
			case *ResultSubmitted:
				if e.RequestID == endRequestID {
					done = true
					endPosition = e.eventMeta.ActualPosition
				}
			}
		}
	}
	// truncate all reqs to found end position
	// fmt.Println("TRUNC", queueResults, int64(endPosition), last)
	span.AddEvent(fmt.Sprint("TRUNC", queueResults, int64(endPosition)))
	err := s.es.Truncate(ctx, queueResults, int64(endPosition))
	if err != nil {
		return err
	}
	return nil
}

func filter(peer *Peer, requests, responses event.Events) *RequestSubmitted {
	have := make(map[string]struct{}, len(responses))
	for _, res := range toList[ResultSubmitted](responses...) {
		have[res.RequestID] = struct{}{}
	}
	for _, req := range reverse(toList[RequestSubmitted](requests...)...) {
		if _, ok := have[req.RequestID()]; !ok {
			if !peer.CanSupport(req.RequestIP) {
				continue
			}

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
func loadTemplates() error {
	if templates != nil {
		return nil
	}
	templates = make(map[string]*template.Template)
	tmplFiles, err := fs.ReadDir(files, "pages")
	if err != nil {
		return err
	}

	for _, tmpl := range tmplFiles {
		if tmpl.IsDir() {
			continue
		}
		pt := template.New(tmpl.Name())
		pt.Funcs(funcMap)
		pt, err = pt.ParseFS(files, "pages/"+tmpl.Name(), "layouts/*.tpl")
		if err != nil {
			log.Println(err)

			return err
		}
		templates[tmpl.Name()] = pt
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

type Args struct {
	RemoteIP   string
	Requests   []*Request
	CountPeers int
}

func requestArgs(r *http.Request) Args {
	remoteIP, _, _ := strings.Cut(r.RemoteAddr, ":")
	if s := r.Header.Get("X-Forwarded-For"); s != "" {
		remoteIP = s
	}
	return Args{
		RemoteIP: remoteIP,
	}
}

func renderTo(w io.Writer, name string, args any) (err error) {
	defer func() {
		if p := recover(); p != nil {
			err = fmt.Errorf("panic: %s", p)
		}
		if err != nil {
			fmt.Fprint(w, err)
		}
	}()

	t, ok := templates[name]
	if !ok || t == nil {
		return fmt.Errorf("missing template")
	}
	return t.Execute(w, args)
}

type ListResponse []Response

func (lis ListResponse) Len() int {
	return len(lis)
}
func (lis ListResponse) Less(i, j int) bool {
	return lis[i].Latency < lis[j].Latency
}
func (lis ListResponse) Swap(i, j int) {
	lis[i], lis[j] = lis[j], lis[i]
}

func fnOrderByPeer(rq *Request) any {
	type peerResult struct {
		Name    string
		Country string
		Latency float64
		Jitter  float64
	}
	type peer struct {
		Name     string
		Note     string
		Nick     string
		Country  string
		Latency  float64
		Jitter   float64
		VPNTypes []string

		Results []peerResult
	}

	peers := make(map[string]peer)
	sort.Sort(ListResponse(rq.Responses))
	for _, rs := range rq.Responses {
		p, ok := peers[rs.Peer.Owner]

		if !ok {
			p.Country = rs.Peer.Country
			p.Name = rs.Peer.Name
			p.Nick = rs.Peer.Nick
			p.Note = rs.Peer.Note
			p.Latency = rs.Latency
			p.Jitter = rs.Jitter
			p.VPNTypes = rs.Peer.Type
		}

		p.Results = append(p.Results, peerResult{
			Name:    rs.Peer.Name,
			Country: rs.Peer.Country,
			Latency: rs.Latency,
			Jitter:  rs.Jitter,
		})

		peers[rs.Peer.Owner] = p
	}

	return peers
}
func fnCountResponses(rq *Request) int {
	count := 0
	for _, res := range rq.Responses {
		if !res.Unreachable {
			count++
		}
	}
	return count
}

var funcMap = map[string]any{
	"orderByPeer":    fnOrderByPeer,
	"countResponses": fnCountResponses,
}

func (s *service) loadResult(ctx context.Context, uuid string) (*Request, error) {
	request := &Request{}
	request.SetStreamID(aggRequest(uuid))
	err := s.es.Load(ctx, request)
	if err != nil {
		return nil, err
	}

	return request, s.state.Modify(ctx, func(ctx context.Context, t *state) error {
		for i := range request.Responses {
			res := &request.Responses[i]
			if peer, ok := t.peers[res.PeerID]; ok {
				res.Peer = peer
				res.Peer.ID = ""
			}
		}

		return nil
	})
}
