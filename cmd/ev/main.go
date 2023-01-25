package main

import (
	"context"
	"errors"
	"log"
	"net/http"
	"os"
	"os/signal"

	"github.com/sour-is/ev/internal/lg"
	"github.com/sour-is/ev/pkg/service"
)

var apps service.Apps
var appName, version = service.AppName()

func main() {
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, os.Kill)
	go func() {
		<-ctx.Done()
		defer cancel() // restore interrupt function
	}()
	if err := Run(ctx); err != nil {
		log.Fatal(err)
		os.Exit(1)
	}
}
func Run(ctx context.Context) error {
	svc := &service.Harness{}
	ctx, stop := lg.Init(ctx, appName)
	svc.OnStop(stop)
	svc.Add(lg.NewHTTP(ctx))
	svc.Setup(ctx, apps.Apps()...)

	// Run application
	if err := svc.Run(ctx, appName, version); err != nil && !errors.Is(err, http.ErrServerClosed) {
		return err
	}
	return nil
}
