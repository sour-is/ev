package webfinger

import (
	"context"
	"crypto/ed25519"
	"embed"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"html"
	"html/template"
	"io"
	"io/fs"
	"log"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"strings"

	"github.com/golang-jwt/jwt/v4"
	"go.sour.is/pkg/lg"
	"go.sour.is/pkg/set"

	"go.sour.is/ev"
	"go.sour.is/ev/pkg/event"
)

var (
	//go:embed ui/*/*
	files     embed.FS
	templates map[string]*template.Template
)

type service struct {
	es    *ev.EventStore
	self  set.Set[string]
	cache func(string) bool
}

type Option interface {
	ApplyWebfinger(s *service)
}

type WithHostnames []string

func (o WithHostnames) ApplyWebfinger(s *service) {
	s.self = set.New(o...)
}

type WithCache func(string) bool

func (o WithCache) ApplyWebfinger(s *service) {
	s.cache = o
}

func New(ctx context.Context, es *ev.EventStore, opts ...Option) (*service, error) {
	ctx, span := lg.Span(ctx)
	defer span.End()

	if err := event.Register(
		ctx,
		&SubjectSet{},
		&SubjectDeleted{},
		&LinkSet{},
		&LinkDeleted{},
	); err != nil {
		return nil, err
	}
	svc := &service{es: es}

	for _, o := range opts {
		o.ApplyWebfinger(svc)
	}

	return svc, nil
}

func (s *service) RegisterHTTP(mux *http.ServeMux) {
	a, _ := fs.Sub(files, "ui/assets")
	assets := http.StripPrefix("/webfinger/assets/", http.FileServer(http.FS(a)))

	mux.Handle("/webfinger", s.ui())
	mux.Handle("/webfinger/assets/", assets)
}
func (s *service) RegisterWellKnown(mux *http.ServeMux) {
	mux.Handle("/webfinger", lg.Htrace(s, "webfinger"))
}
func (s *service) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	ctx, span := lg.Span(ctx)
	defer span.End()

	if r.URL.Path != "/webfinger" {
		w.WriteHeader(http.StatusNotFound)
		fmt.Fprint(w, http.StatusText(http.StatusNotFound))
		return
	}

	switch r.Method {
	case http.MethodPut, http.MethodDelete:
		if r.ContentLength > 4096 {
			w.WriteHeader(http.StatusRequestEntityTooLarge)
			fmt.Fprint(w, http.StatusText(http.StatusRequestEntityTooLarge))
			span.AddEvent("request too large")

			return
		}
		body, err := io.ReadAll(io.LimitReader(r.Body, 4096))
		if err != nil {
			w.WriteHeader(http.StatusInternalServerError)
			fmt.Fprint(w, http.StatusText(http.StatusInternalServerError))
			span.RecordError(err)

			return
		}
		r.Body.Close()

		type claims struct {
			PubKey string `json:"pub"`
			*JRD
			jwt.RegisteredClaims
		}

		token, err := jwt.ParseWithClaims(
			string(body),
			&claims{},
			func(tok *jwt.Token) (any, error) {
				c, ok := tok.Claims.(*claims)
				if !ok {
					return nil, fmt.Errorf("wrong type of claim")
				}

				if c.JRD == nil {
					c.JRD = &JRD{}
				}

				c.JRD.Subject = c.RegisteredClaims.Subject

				c.SetProperty(NSpubkey, &c.PubKey)

				pub, err := dec(c.PubKey)
				return ed25519.PublicKey(pub), err
			},
			jwt.WithValidMethods([]string{"EdDSA"}),
			jwt.WithJSONNumber(),
		)
		if err != nil {
			w.WriteHeader(http.StatusUnprocessableEntity)
			fmt.Fprint(w, http.StatusText(http.StatusUnprocessableEntity), ": ", err.Error())
			span.RecordError(err)

			return
		}

		c, ok := token.Claims.(*claims)
		if !ok {
			w.WriteHeader(http.StatusUnprocessableEntity)
			fmt.Fprint(w, http.StatusText(http.StatusUnprocessableEntity))
			span.AddEvent("not a claim")

			return
		}

		if c.ID != "" && s.cache != nil {
			if ok := s.cache(c.ID); ok {
				w.WriteHeader(http.StatusAlreadyReported)
				fmt.Fprint(w, http.StatusText(http.StatusAlreadyReported))
				span.AddEvent("already seen ID")

				return
			}
		}

		json.NewEncoder(os.Stdout).Encode(c.JRD)

		for i := range c.JRD.Links {
			c.JRD.Links[i].Index = uint64(i)
		}

		a, err := ev.Upsert(ctx, s.es, StreamID(c.JRD.Subject), func(ctx context.Context, a *JRD) error {
			var auth *JRD

			for i := range a.Links {
				a.Links[i].Index = uint64(i)
			}

			// does the target have a pubkey for self auth?
			if _, ok := a.Properties[NSpubkey]; ok {
				auth = a
			}

			// Check current version for auth.
			if authID, ok := a.Properties[NSauth]; ok && authID != nil && auth == nil {
				auth = &JRD{}
				auth.SetStreamID(StreamID(*authID))
				err := s.es.Load(ctx, auth)
				if err != nil {
					return err
				}
			}
			if a.Version() == 0 || a.IsDeleted() {
				// else does the new object claim auth from another object?
				if authID, ok := c.Properties[NSauth]; ok && authID != nil && auth == nil {
					auth = &JRD{}
					auth.SetStreamID(StreamID(*authID))
					err := s.es.Load(ctx, auth)
					if err != nil {
						return err
					}
				}

				// fall back to use auth from submitted claims
				if auth == nil {
					auth = c.JRD
				}
			}

			if auth == nil {
				return fmt.Errorf("auth not found")
			}

			err = a.OnAuth(c.JRD, auth)
			if err != nil {
				return err
			}

			if r.Method == http.MethodDelete {
				return a.OnDelete(c.JRD)
			}
			return a.OnClaims(c.JRD)
		})

		if err != nil {
			w.WriteHeader(http.StatusUnprocessableEntity)
			fmt.Fprint(w, http.StatusText(http.StatusUnprocessableEntity), ": ", err.Error())
			span.RecordError(err)

			return
		}

		if version := a.Version(); r.Method == http.MethodDelete && version > 0 {
			err = s.es.Truncate(ctx, a.StreamID(), int64(version))
			span.RecordError(err)
		}

		w.Header().Set("Content-Type", "application/jrd+json")
		if r.Method == http.MethodDelete {
			w.WriteHeader(http.StatusNoContent)
		} else {
			w.WriteHeader(http.StatusCreated)
		}
		j := json.NewEncoder(w)
		j.SetIndent("", "  ")
		err = j.Encode(a)
		span.RecordError(err)

	case http.MethodGet:
		resource := r.URL.Query().Get("resource")
		rels := r.URL.Query()["rel"]

		if resource == "" {
			w.WriteHeader(http.StatusBadRequest)
			return
		}

		if u := Parse(resource); u != nil && !s.self.Has(u.URL.Host) {
			redirect := &url.URL{}
			redirect.Scheme = "https"
			redirect.Host = u.URL.Host
			redirect.RawQuery = r.URL.RawQuery
			redirect.Path = "/.well-known/webfinger"

			w.Header().Set("location", redirect.String())
			w.WriteHeader(http.StatusSeeOther)
			return
		}

		a := &JRD{}
		a.SetStreamID(StreamID(resource))
		err := s.es.Load(ctx, a)
		if err != nil {
			span.RecordError(err)

			if errors.Is(err, ev.ErrNotFound) {
				w.WriteHeader(http.StatusNotFound)
				fmt.Fprint(w, http.StatusText(http.StatusNotFound))
				return
			}

			w.WriteHeader(http.StatusInternalServerError)
			fmt.Fprint(w, http.StatusText(http.StatusInternalServerError))
			return
		}

		if a.IsDeleted() {
			w.WriteHeader(http.StatusGone)
			fmt.Fprint(w, http.StatusText(http.StatusGone))
			span.AddEvent("is deleted")

			return
		}

		if len(rels) > 0 {
			a.Links = a.GetLinksByRel(rels...)
		}

		if a.Properties != nil {
			if redirect, ok := a.Properties[NSredirect]; ok && redirect != nil {
				w.Header().Set("location", *redirect)
				w.WriteHeader(http.StatusSeeOther)
				return
			}
		}

		w.Header().Set("Content-Type", "application/jrd+json")
		w.WriteHeader(http.StatusOK)

		j := json.NewEncoder(w)
		j.SetIndent("", "  ")
		err = j.Encode(a)
		span.RecordError(err)

	default:
		w.Header().Set("Allow", "GET, PUT, DELETE, OPTIONS")
		w.WriteHeader(http.StatusMethodNotAllowed)
		fmt.Fprint(w, http.StatusText(http.StatusMethodNotAllowed))
		span.AddEvent("method not allow: " + r.Method)
	}
}
func (s *service) ui() http.HandlerFunc {
	loadTemplates()
	return func(w http.ResponseWriter, r *http.Request) {
		args := struct {
			Req    *http.Request
			Status int
			Body   []byte
			JRD    *JRD
			Err    error
		}{Status: http.StatusOK}

		if r.URL.Query().Has("resource") {
			args.Req, args.Err = http.NewRequestWithContext(r.Context(), http.MethodGet, r.URL.String(), nil)
			if args.Err != nil {
				http.Error(w, http.StatusText(http.StatusBadRequest), http.StatusBadRequest)
				return
			}
			wr := httptest.NewRecorder()
			s.ServeHTTP(wr, args.Req)

			args.Status = wr.Code

			switch wr.Code {
			case http.StatusSeeOther:
				res, err := http.DefaultClient.Get(wr.Header().Get("location"))
				args.Err = err
				if err == nil {
					args.Status = res.StatusCode
					args.Body, args.Err = io.ReadAll(res.Body)
				}
			case http.StatusOK:
				args.Body, args.Err = io.ReadAll(wr.Body)
				if args.Err == nil {
					args.JRD, args.Err = ParseJRD(args.Body)
				}
			}
			if args.Err == nil && args.Body != nil {
				args.JRD, args.Err = ParseJRD(args.Body)
			}
		}

		t := templates["home.go.tpl"]
		err := t.Execute(w, args)
		if err != nil {
			log.Println(err)
		}
	}
}

func dec(s string) ([]byte, error) {
	s = strings.TrimSpace(s)
	return base64.RawURLEncoding.DecodeString(s)
}

var funcMap = map[string]any{
	"propName": func(in string) string { return in[strings.LastIndex(in, "/")+1:] },
	"escape":   html.EscapeString,
}

func loadTemplates() error {
	if templates != nil {
		return nil
	}
	templates = make(map[string]*template.Template)
	tmplFiles, err := fs.ReadDir(files, "ui/pages")
	if err != nil {
		return err
	}

	for _, tmpl := range tmplFiles {
		if tmpl.IsDir() {
			continue
		}
		pt := template.New(tmpl.Name())
		pt.Funcs(funcMap)
		pt, err = pt.ParseFS(files, "ui/pages/"+tmpl.Name(), "ui/layouts/*.go.tpl")
		if err != nil {
			log.Println(err)

			return err
		}
		templates[tmpl.Name()] = pt
	}
	return nil
}
