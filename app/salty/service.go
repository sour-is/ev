package salty

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/keys-pub/keys"
	"go.mills.io/saltyim"
	"go.opentelemetry.io/otel/metric"
	"go.uber.org/multierr"

	"go.sour.is/ev"
	"go.sour.is/ev/pkg/event"
	"go.sour.is/pkg/gql"
	"go.sour.is/pkg/lg"
)

type DNSResolver interface {
	LookupSRV(ctx context.Context, service, proto, name string) (string, []*net.SRV, error)
}

type service struct {
	baseURL string
	es      *ev.EventStore
	dns     DNSResolver

	m_create_user  metric.Int64Counter
	m_get_user     metric.Int64Counter
	m_api_ping     metric.Int64Counter
	m_api_register metric.Int64Counter
	m_api_lookup   metric.Int64Counter
	m_api_send     metric.Int64Counter
	m_req_time     metric.Int64Histogram

	opts []Option
}

type Option interface {
	ApplySalty(*service)
}

type WithBaseURL string

func (o WithBaseURL) ApplySalty(s *service) {
	s.baseURL = string(o)
}

type contextKey struct {
	name string
}

var saltyKey = contextKey{"salty"}

type SaltyResolver interface {
	CreateSaltyUser(ctx context.Context, nick string, pub string) (*SaltyUser, error)
	SaltyUser(ctx context.Context, nick string) (*SaltyUser, error)
	IsResolver()
}

func New(ctx context.Context, es *ev.EventStore, opts ...Option) (*service, error) {
	ctx, span := lg.Span(ctx)
	defer span.End()

	if err := event.Register(ctx, &UserRegistered{}); err != nil {
		return nil, err
	}
	if err := event.RegisterName(ctx, "domain.UserRegistered", &UserRegistered{}); err != nil {
		return nil, err
	}

	m := lg.Meter(ctx)

	svc := &service{opts: opts, es: es, dns: net.DefaultResolver}

	for _, o := range opts {
		o.ApplySalty(svc)

		if o, ok := o.(interface{ Setup(context.Context) error }); ok {
			if err := o.Setup(ctx); err != nil {
				return nil, err
			}
		}
	}

	var err, errs error
	svc.m_create_user, err = m.Int64Counter("salty_create_user",
		metric.WithDescription("salty create user graphql called"),
	)
	errs = multierr.Append(errs, err)

	svc.m_get_user, err = m.Int64Counter("salty_get_user",
		metric.WithDescription("salty get user graphql called"),
	)
	errs = multierr.Append(errs, err)

	svc.m_api_ping, err = m.Int64Counter("salty_api_ping",
		metric.WithDescription("salty api ping called"),
	)
	errs = multierr.Append(errs, err)

	svc.m_api_register, err = m.Int64Counter("salty_api_register",
		metric.WithDescription("salty api register"),
	)
	errs = multierr.Append(errs, err)

	svc.m_api_lookup, err = m.Int64Counter("salty_api_lookup",
		metric.WithDescription("salty api ping lookup"),
	)
	errs = multierr.Append(errs, err)

	svc.m_api_send, err = m.Int64Counter("salty_api_send",
		metric.WithDescription("salty api ping send"),
	)
	errs = multierr.Append(errs, err)

	svc.m_req_time, err = m.Int64Histogram("salty_request_time",
		metric.WithDescription("histogram of requests"),
		metric.WithUnit("ns"),
	)
	errs = multierr.Append(errs, err)

	span.RecordError(err)

	return svc, errs
}

func (s *service) BaseURL() string {
	if s == nil {
		return "http://missing.context/"
	}
	return s.baseURL
}

func (s *service) RegisterHTTP(mux *http.ServeMux) {
	for _, o := range s.opts {
		if o, ok := o.(interface{ RegisterHTTP(mux *http.ServeMux) }); ok {
			o.RegisterHTTP(mux)
		}
	}
}
func (s *service) RegisterAPIv1(mux *http.ServeMux) {
	mux.HandleFunc("/ping", s.apiv1)
	mux.HandleFunc("/register", s.apiv1)
	mux.HandleFunc("/lookup/", s.apiv1)
	mux.HandleFunc("/send", s.apiv1)

	for _, o := range s.opts {
		if o, ok := o.(interface{ RegisterAPIv1(mux *http.ServeMux) }); ok {
			o.RegisterAPIv1(mux)
		}
	}
}
func (s *service) RegisterWellKnown(mux *http.ServeMux) {
	mux.Handle("/salty/", lg.Htrace(s, "lookup"))

	for _, o := range s.opts {
		if o, ok := o.(interface{ RegisterWellKnown(mux *http.ServeMux) }); ok {
			o.RegisterWellKnown(mux)
		}
	}
}
func (s *service) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	ctx, span := lg.Span(ctx)
	defer span.End()

	start := time.Now()
	defer s.m_req_time.Record(ctx, time.Since(start).Milliseconds())

	addr := "saltyuser-" + strings.TrimPrefix(r.URL.Path, "/salty/")
	addr = strings.TrimSuffix(addr, ".json")

	span.AddEvent(fmt.Sprint("find ", addr))
	a, err := ev.Update(ctx, s.es, addr, func(ctx context.Context, agg *SaltyUser) error { return nil })
	switch {
	case errors.Is(err, event.ErrShouldExist):
		span.RecordError(err)

		w.WriteHeader(http.StatusNotFound)
		return
	case err != nil:
		span.RecordError(err)

		w.WriteHeader(http.StatusInternalServerError)
		return
	}

	basePath, _ := url.JoinPath(s.baseURL, a.inbox.String())

	err = json.NewEncoder(w).Encode(
		struct {
			Endpoint string `json:"endpoint"`
			Key      string `json:"key"`
		}{
			Endpoint: basePath,
			Key:      a.pubkey.ID().String(),
		})
	if err != nil {
		span.RecordError(err)
	}
}

func (s *service) IsResolver() {}
func (s *service) GetMiddleware() func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			r = r.WithContext(gql.ToContext(r.Context(), saltyKey, s))
			next.ServeHTTP(w, r)
		})
	}
}
func (s *service) CreateSaltyUser(ctx context.Context, nick string, pub string) (*SaltyUser, error) {
	ctx, span := lg.Span(ctx)
	defer span.End()

	s.m_create_user.Add(ctx, 1)
	start := time.Now()
	defer s.m_req_time.Record(ctx, time.Since(start).Milliseconds())

	streamID := NickToStreamID(nick)
	span.AddEvent(streamID)

	return s.createSaltyUser(ctx, streamID, pub)
}
func (s *service) createSaltyUser(ctx context.Context, streamID, pub string) (*SaltyUser, error) {
	ctx, span := lg.Span(ctx)
	defer span.End()

	key, err := keys.NewEdX25519PublicKeyFromID(keys.ID(pub))
	if err != nil {
		span.RecordError(err)
		return nil, err
	}

	a, err := ev.Create(ctx, s.es, streamID, func(ctx context.Context, agg *SaltyUser) error {
		return agg.OnUserRegister(key)
	})
	switch {
	case errors.Is(err, ev.ErrShouldNotExist):
		span.RecordError(err)
		return nil, fmt.Errorf("user exists: %w", err)

	case err != nil:
		span.RecordError(err)
		return nil, fmt.Errorf("internal error: %w", err)
	}

	return a, nil
}
func (s *service) SaltyUser(ctx context.Context, nick string) (*SaltyUser, error) {
	ctx, span := lg.Span(ctx)
	defer span.End()

	s.m_get_user.Add(ctx, 1)
	start := time.Now()
	defer s.m_req_time.Record(ctx, time.Since(start).Milliseconds())

	streamID := fmt.Sprintf("saltyuser-%x", sha256.Sum256([]byte(strings.ToLower(nick))))
	span.AddEvent(streamID)

	a, err := ev.Update(ctx, s.es, streamID, func(ctx context.Context, agg *SaltyUser) error { return nil })
	switch {
	case errors.Is(err, ev.ErrShouldExist):
		span.RecordError(err)
		return nil, fmt.Errorf("user not found")

	case err != nil:
		span.RecordError(err)
		return nil, fmt.Errorf("%w internal error", err)
	}

	return a, err
}

func (s *service) apiv1(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	ctx, span := lg.Span(ctx)
	defer span.End()

	start := time.Now()
	defer s.m_req_time.Record(ctx, time.Since(start).Nanoseconds())

	switch r.Method {
	case http.MethodGet:
		switch {
		case r.URL.Path == "/ping":
			s.m_api_ping.Add(ctx, 1)

			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{}`))

		case strings.HasPrefix(r.URL.Path, "/lookup/"):
			s.m_api_lookup.Add(ctx, 1)

			addr, err := s.ParseAddr(strings.TrimPrefix(r.URL.Path, "/lookup/"))
			if err != nil {
				span.RecordError(err)
				w.WriteHeader(http.StatusBadRequest)
				return
			}
			err = addr.Refresh(ctx)
			if err != nil {
				span.RecordError(err)
				w.WriteHeader(http.StatusBadRequest)
				return
			}

			err = json.NewEncoder(w).Encode(addr)
			span.RecordError(err)
			return

		default:
			w.WriteHeader(http.StatusNotFound)
			return
		}

	case http.MethodPost:
		switch r.URL.Path {
		case "/register":
			s.m_api_register.Add(ctx, 1)

			req, signer, err := saltyim.NewRegisterRequest(r.Body)
			if err != nil {
				span.RecordError(fmt.Errorf("error parsing register request: %w", err))
				http.Error(w, "Bad Request", http.StatusBadRequest)
				return
			}
			if signer != req.Key {
				http.Error(w, "Bad Request", http.StatusBadRequest)
				return
			}

			_, err = s.createSaltyUser(ctx, HashToStreamID(req.Hash), req.Key)
			if errors.Is(err, event.ErrShouldNotExist) {
				http.Error(w, "Already Exists", http.StatusConflict)
				return
			} else if err != nil {
				http.Error(w, "Error", http.StatusInternalServerError)
				return
			}

			http.Error(w, "Endpoint Created", http.StatusCreated)
			return

		case "/send":
			s.m_api_send.Add(ctx, 1)

			req, signer, err := saltyim.NewSendRequest(r.Body)
			if err != nil {
				span.RecordError(fmt.Errorf("error parsing send request: %w", err))
				http.Error(w, "Bad Request", http.StatusBadRequest)
				return
			}
			// TODO: Do something with signer?
			span.AddEvent(fmt.Sprintf("request signed by %s", signer))

			u, err := url.Parse(req.Endpoint)
			if err != nil {
				span.RecordError(fmt.Errorf("error parsing endpoint %s: %w", req.Endpoint, err))
				http.Error(w, "Bad Endpoint", http.StatusBadRequest)
				return
			}
			if !u.IsAbs() {
				span.RecordError(fmt.Errorf("endpoint %s is not an absolute uri: %w", req.Endpoint, err))
				http.Error(w, "Bad Endpoint", http.StatusBadRequest)
				return
			}

			// TODO: Queue up an internal retry and return immediately on failure?
			if err := saltyim.Send(req.Endpoint, req.Message, req.Capabilities); err != nil {
				span.RecordError(fmt.Errorf("error sending message to %s: %w", req.Endpoint, err))
				http.Error(w, "Send Error", http.StatusInternalServerError)
				return
			}

			http.Error(w, "Message Accepted", http.StatusAccepted)

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
