package gofi

import (
	"context"
	"errors"
	"log/slog"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/joaoprofile/gofi-sdk-go/base/observer"

	"github.com/joaoprofile/gofi-sdk-go/base/environment"
	"github.com/joaoprofile/gofi-sdk-go/obs/logging"
)

//  Service interface implementation
// These methods are only reachable via the Service interface, returned by Build().

func (g *gofiInstance) Environment() *environment.Environment {
	return g.env
}

//  Lifecycle

// shutdownTimeout bounds stopping the runners and releasing resources after a signal.
const shutdownTimeout = 30 * time.Second

// ListenAndServe runs the runners until SIGINT/SIGTERM, Shutdown or a runner
// failure, stops them and then closes the resources gofi created, in reverse
// order of creation.
func (g *gofiInstance) ListenAndServe() error {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	g.lifeMu.Lock()
	if g.closed {
		g.lifeMu.Unlock()
		return nil
	}
	g.stop, g.done = stop, make(chan struct{})
	done := g.done
	g.lifeMu.Unlock()
	defer close(done)

	var serveErr error
	if len(g.runners) > 0 {
		serveErr = g.run(ctx)
	} else {
		logging.Info("service started")
		<-ctx.Done()
	}
	logging.Info("shutdown started")

	shCtx, cancel := context.WithTimeout(context.Background(), shutdownTimeout)
	defer cancel()
	return errors.Join(serveErr, g.closeResources(shCtx))
}

// run starts every runner and waits for ctx or the first runner to return,
// then stops the others and collects their results.
func (g *gofiInstance) run(ctx context.Context) error {
	results := make(chan error, len(g.runners))
	for _, r := range g.runners {
		go func() { results <- r.Run() }()
	}

	var errs []error
	pending := len(g.runners)
	select {
	case err := <-results:
		errs = append(errs, err)
		pending--
	case <-ctx.Done():
	}

	stopCtx, cancel := context.WithTimeout(context.Background(), shutdownTimeout)
	defer cancel()
	for _, r := range g.runners {
		errs = append(errs, r.Stop(stopCtx))
	}
	for ; pending > 0; pending-- {
		select {
		case err := <-results:
			errs = append(errs, err)
		case <-stopCtx.Done():
			return errors.Join(append(errs, stopCtx.Err())...)
		}
	}
	return errors.Join(errs...)
}

// Shutdown stops a running ListenAndServe and waits for it to finish; when it
// is not running, it closes the resources directly.
func (g *gofiInstance) Shutdown(ctx context.Context) error {
	g.lifeMu.Lock()
	g.closed = true
	stop, done := g.stop, g.done
	g.lifeMu.Unlock()

	if stop == nil {
		return g.closeResources(ctx)
	}
	stop()
	for _, r := range g.runners {
		if err := r.Stop(ctx); err != nil {
			return err
		}
	}
	select {
	case <-done:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

// closeResources stops the observers, then closes what the components opened,
// in reverse order, and flushes the logger.
func (g *gofiInstance) closeResources(ctx context.Context) error {
	observer.Shutdown()
	var owned error
	if g.rt != nil {
		owned = g.rt.Close(ctx)
	}
	err := errors.Join(owned, logging.Shutdown(ctx))
	if err != nil {
		logging.Error("shutdown finished with errors", slog.Any("error", err))
	}
	return err
}
