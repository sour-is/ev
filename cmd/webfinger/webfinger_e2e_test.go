package main

import (
	"context"
	"crypto/ed25519"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"strings"
	"testing"

	"github.com/matryer/is"
	"github.com/sour-is/ev/app/webfinger"
	"github.com/sour-is/ev/pkg/service"
	"golang.org/x/sync/errgroup"
)

func TestMain(m *testing.M) {
	data, err := os.MkdirTemp("", "data*")
	if err != nil {
		fmt.Printf("error creating data dir: %s\n", err)
		os.Exit(1)
	}
	defer os.RemoveAll(data)

	os.Setenv("EV_HTTP", "[::1]:61234")
	os.Setenv("WEBFINGER_DOMAINS", "::1")

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	running := make(chan struct{})
	apps.Register(99, func(ctx context.Context, s *service.Harness) error {
		go func() {
			<-s.OnRunning()
			close(running)
		}()

		return nil
	})

	wg, ctx := errgroup.WithContext(ctx)
	wg.Go(func() error {
		// Run application
		if err := run(ctx); err != nil {
			return err
		}
		return nil
	})
	wg.Go(func() error {
		<-running
		m.Run()
		cancel()
		return nil
	})

	if err := wg.Wait(); err != nil {
		fmt.Println(err)
		os.Exit(1)
	}
}

func TestGetHTTP(t *testing.T) {
	is := is.New(t)
	res, err := http.DefaultClient.Get("http://[::1]:61234/.well-known/webfinger")
	is.NoErr(err)
	is.Equal(res.StatusCode, http.StatusBadRequest)
}

func TestCreateResource(t *testing.T) {
	is := is.New(t)

	_, priv, err := ed25519.GenerateKey(nil)
	is.NoErr(err)

	jrd := &webfinger.JRD{
		Subject: "me@sour.is",
		Properties: map[string]*string{
			"foo": ptr("bar"),
		},
	}

	// create
	token, err := webfinger.NewSignedRequest(jrd, priv)
	is.NoErr(err)

	req, err := http.NewRequest(http.MethodPut, "http://[::1]:61234/.well-known/webfinger", strings.NewReader(token))
	is.NoErr(err)

	res, err := http.DefaultClient.Do(req)
	is.NoErr(err)

	is.Equal(res.StatusCode, http.StatusCreated)

	// repeat
	req, err = http.NewRequest(http.MethodPut, "http://[::1]:61234/.well-known/webfinger", strings.NewReader(token))
	is.NoErr(err)

	res, err = http.DefaultClient.Do(req)
	is.NoErr(err)

	is.Equal(res.StatusCode, http.StatusAlreadyReported)

	// fetch
	req, err = http.NewRequest(http.MethodGet, "http://[::1]:61234/.well-known/webfinger?resource=me@sour.is", nil)
	is.NoErr(err)

	res, err = http.DefaultClient.Do(req)
	is.NoErr(err)

	is.Equal(res.StatusCode, http.StatusOK)

	resJRD := &webfinger.JRD{}
	err = json.NewDecoder(res.Body).Decode(resJRD)
	is.NoErr(err)
	is.Equal(jrd.Subject, resJRD.Subject)
}

func ptr[T any](t T) *T { return &t }
