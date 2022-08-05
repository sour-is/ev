package main

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"os/signal"
	"strconv"
	"strings"
	"time"

	"golang.org/x/sync/errgroup"

	"github.com/sour-is/ev/pkg/es"
	"github.com/sour-is/ev/pkg/es/driver"
	"github.com/sour-is/ev/pkg/es/driver/disk-store"
	"github.com/sour-is/ev/pkg/es/event"
)

func main() {
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, os.Kill)
	go func() {
		<-ctx.Done()
		defer cancel()
	}()

	if err := run(ctx); err != nil {
		log.Fatal(err)
	}
}

func run(ctx context.Context) error {
	event.Register(&PostEvent{})
	diskstore.Init(ctx)

	es, err := es.Open(ctx, "file:data")
	if err != nil {
		return err
	}

	svc := &service{
		es: es,
	}

	s := http.Server{
		Addr: ":8080",
	}

	http.HandleFunc("/event/", svc.event)

	log.Print("Listen on ", s.Addr)
	g, ctx := errgroup.WithContext(ctx)

	g.Go(s.ListenAndServe)

	g.Go(func() error {
		<-ctx.Done()
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		log.Print("shutdown http")
		return s.Shutdown(ctx)
	})

	return g.Wait()
}

type service struct {
	es driver.EventStore
}

func (s *service) event(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	var name, tags string
	if strings.HasPrefix(r.URL.Path, "/event/") {
		name = strings.TrimPrefix(r.URL.Path, "/event/")
		name, tags, _ = strings.Cut(name, "/")
	} else {
		w.WriteHeader(http.StatusNotFound)
		return
	}

	if r.Method == http.MethodGet {
		if name == "" {
			w.WriteHeader(http.StatusNotFound)
			return
		}

		var pos, count int64 = -1, -99
		qry := r.URL.Query()

		if i, err := strconv.ParseInt(qry.Get("pos"), 10, 64); err == nil {
			pos = i
		}
		if i, err := strconv.ParseInt(qry.Get("n"), 10, 64); err == nil {
			count = i
		}

		log.Print("name=", name, ", pos=", pos, ", n=", count)
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
	}

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

	log.Print(name, tags)
	events := event.NewEvents(&PostEvent{
		Payload: b,
		Tags:    strings.Split(tags, "/"),
	})
	_, err = s.es.Append(r.Context(), "post-"+name, events)
	if err != nil {
		log.Print(err)

		w.WriteHeader(http.StatusInternalServerError)
		return
	}

	m := events.First().EventMeta()
	w.WriteHeader(http.StatusAccepted)
	fmt.Fprintf(w, "OK %d %s", m.Position, m.EventID)
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
