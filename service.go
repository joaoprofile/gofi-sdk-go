package gofi

import (
	"context"
	"database/sql"
	"log/slog"
	"os"
	"os/signal"
	"syscall"

	"github.com/joaoprofile/gofi/base/environment"
	"github.com/joaoprofile/gofi/msq"
	"github.com/joaoprofile/gofi/netx"
	"github.com/joaoprofile/gofi/obs/logging"
	"github.com/redis/go-redis/v9"
)

//  Resource accessors

func (g *gofiInstance) Environment() *environment.Environment {
	return g.env
}

func (g *gofiInstance) Database() *sql.DB {
	return g.databaseConn
}

func (g *gofiInstance) Cache() redis.UniversalClient {
	return g.cacheConn
}

func (g *gofiInstance) Messaging() msq.Messaging {
	return g.messagingConn
}

func (g *gofiInstance) HttpServer() netx.HttpServer {
	return g.httpServer
}

//  Lifecycle

// ListenAndServe starts the HTTP server if configured, or blocks until an OS
// signal is received.
func (g *gofiInstance) ListenAndServe() {
	if g.httpServer != nil {
		g.httpServer.ListenAndServe()
		return
	}

	interrupt := make(chan os.Signal, 1)
	signal.Notify(interrupt, syscall.SIGINT, syscall.SIGTERM, syscall.SIGHUP, os.Interrupt)

	logging.Info("server started")
	sig := <-interrupt
	logging.Info("received shutdown signal", slog.String("signal", sig.String()))
}

// Shutdown gracefully stops all active resources.
// Connections registered during Build() are closed in reverse order.
// TODO: integrate lifecycle.Registry for ordered shutdown
func (g *gofiInstance) Shutdown(ctx context.Context) error {
	if g.httpServer != nil {
		logging.Info("shutting down HTTP server")
		// TODO: httpServer.Shutdown(ctx)
	}

	if g.cacheConn != nil {
		logging.Info("closing cache connection")
		if err := g.cacheConn.Close(); err != nil {
			logging.Error("error closing cache connection", slog.Any("error", err))
		}
	}

	if g.databaseConn != nil {
		logging.Info("closing database connection")
		if err := g.databaseConn.Close(); err != nil {
			logging.Error("error closing database connection", slog.Any("error", err))
		}
	}

	if g.telemetry != nil {
		logging.Info("shutting down telemetry")
		if err := g.telemetry.Shutdown(ctx); err != nil {
			logging.Error("error shutting down telemetry", slog.Any("error", err))
		}
	}

	return nil
}
