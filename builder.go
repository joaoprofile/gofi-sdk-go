package gofi

import (
	"context"
	"crypto/tls"
	"database/sql"
	"fmt"
	"log/slog"
	"net/http"

	"github.com/joaoprofile/gofi/base/common"
	"github.com/joaoprofile/gofi/base/environment"
	"github.com/joaoprofile/gofi/base/observer"
	"github.com/joaoprofile/gofi/base/session"
	"github.com/joaoprofile/gofi/config"
	"github.com/joaoprofile/gofi/msq"
	"github.com/joaoprofile/gofi/msq/port"
	"github.com/joaoprofile/gofi/msq/provider/kafka"
	"github.com/joaoprofile/gofi/msq/provider/oci"
	"github.com/joaoprofile/gofi/msq/provider/rabbitmq"
	redisbroker "github.com/joaoprofile/gofi/msq/provider/redis"
	"github.com/joaoprofile/gofi/msq/provider/sqs"
	"github.com/joaoprofile/gofi/netx"
	"github.com/joaoprofile/gofi/obs"
	"github.com/joaoprofile/gofi/obs/logging"
	"github.com/joaoprofile/gofi/sqln"
	"github.com/joaoprofile/gofi/sqln/connection"
	_ "github.com/joaoprofile/gofi/sqln/driver/postgres"
	"github.com/joaoprofile/gofi/sqln/migrate"
	"github.com/redis/go-redis/v9"
)

// gofiInstance is the private struct that implements both Builder and Service.
// Consumers never hold a reference to this type directly — only to the
// Builder or Service interfaces.
type gofiInstance struct {
	serviceName   string
	env           *environment.Environment
	tlsConfig     *tls.Config
	httpServer    netx.HttpServer
	databaseConn  *sql.DB
	cacheConn     redis.UniversalClient
	messagingConn msq.Messaging
	telemetry     *obs.Telemetry
}

// newInstance is called by New() and performs all startup side effects.
func newInstance(serviceName string) *gofiInstance {
	tlsConfig := &tls.Config{InsecureSkipVerify: true}
	http.DefaultTransport.(*http.Transport).TLSClientConfig = tlsConfig

	env := environment.Instance()
	if serviceName != "" {
		env.AppName = serviceName
	}

	common.SetBrazil()
	config.InitLogging(env, env.AppName)
	config.ConfigureCache(env)
	config.InitCloud(env)
	observer.Instance()

	return &gofiInstance{
		serviceName: serviceName,
		env:         env,
		tlsConfig:   tlsConfig,
	}
}

// Infrastructure

func (g *gofiInstance) AddDatabase() Builder {
	cfg, err := config.Database(g.env)
	if err != nil {
		logging.Error("failed to build database config", slog.Any("error", err))
		return g
	}

	opts := []connection.Option{}
	if g.env.DatabaseMigration {
		opts = append(opts, connection.WithMigrations(migrate.Config{Path: ".migrations"}))
	}

	conn, err := connection.NewConnection(cfg, opts...)
	if err != nil {
		logging.Error(
			"failed to initialize database connection",
			slog.String("driver", string(cfg.Driver)),
			slog.Any("error", err),
		)
		return g
	}

	connection.SetGlobal(conn)
	observer.Attach(connection.NewObserver("main", conn))

	g.databaseConn = conn.DB()
	return g
}

func (g *gofiInstance) AddCache() Builder {
	g.cacheConn = sqln.InstanceRedis()
	return g
}

// AddLockerCache is kept for API compatibility.
// Distributed locking is now integrated into AddSession via the Driver.
func (g *gofiInstance) AddLockerCache() Builder {
	return g
}

func (g *gofiInstance) AddSession(sessionConfig ...*session.Config) Builder {
	config := session.DefaultSessionConfig()
	if len(sessionConfig) > 0 && sessionConfig[0] != nil {
		config = sessionConfig[0]
	}

	switch g.env.CacheType {
	case string(environment.REDIS_CACHE):
		driver := session.NewRedisDriver(sqln.InstanceRedis(), config.TTL)
		session.New(driver, config)
	case string(environment.OCI_CACHE):
		fmt.Println("OCI session driver: not implemented")
	default:
		fmt.Println("session: unknown cache type")
	}

	return g
}

// Transport

func (g *gofiInstance) NewHttpServer(port string, cfg ...*netx.WSConfig) Builder {
	var config *netx.WSConfig
	if len(cfg) > 0 && cfg[0] != nil {
		config = cfg[0]
	} else {
		config = &netx.WSConfig{}
	}
	config.ServerPort = port

	g.httpServer = netx.NewServer(config)
	return g
}

func (g *gofiInstance) Handlers(handlers ...netx.RouterHandler) Builder {
	g.httpServer.AddHandlers(handlers...)
	return g
}

func (g *gofiInstance) Use(middleware ...netx.Middleware) Builder {
	g.httpServer.Use(middleware...)
	return g
}

func (g *gofiInstance) UseAuth(auth netx.Middleware) Builder {
	g.httpServer.UseAuth(auth)
	return g
}

// Observability
//
// AddObservability initialises distributed tracing, metrics and logs via
// OpenTelemetry. Configuration is read from the environment:
func (g *gofiInstance) AddObservability() Builder {
	teleCfg := config.Observability(g.env)
	if teleCfg.CollectorAddr == "" {
		logging.Warn("AddObservability: OTEL_EXPORTER_OTLP_ENDPOINT is not set, skipping")
		return g
	}

	tele, err := obs.Init(context.Background(), teleCfg)
	if err != nil {
		logging.Error("AddObservability: failed to initialise telemetry", slog.Any("error", err))
		return g
	}

	g.telemetry = tele
	return g
}

// Messaging

func messagingEventHandler(_ context.Context, ev msq.BrokerEvent) {
	attrs := []any{
		slog.String("event", string(ev.Type)),
		slog.String("topic", ev.Topic),
	}
	if ev.MessageID != "" {
		attrs = append(attrs, slog.String("message_id", ev.MessageID))
	}
	if ev.Error != nil {
		attrs = append(attrs, slog.Any("error", ev.Error))
		logging.Error("messaging event", attrs...)
	} else {
		logging.Info("messaging event", attrs...)
	}
}

// brokerFromEnv builds a port.Broker for the given type, mapping the MESSAGING_*
// (and CACHE_* for Redis) variables of env into each provider's typed Config via
// the config package. The providers themselves stay decoupled from environment.
func brokerFromEnv(env *environment.Environment, bt msq.BrokerType, exchange string) (port.Broker, error) {
	switch bt {
	case msq.BrokerKafka:
		return kafka.New(config.Kafka(env))

	case msq.BrokerRabbitMQ:
		conn, err := rabbitmq.DialURL(config.RabbitMQURL(env))
		if err != nil {
			return nil, fmt.Errorf("rabbitmq: dial failed: %w", err)
		}
		return rabbitmq.New(conn, exchange), nil

	case msq.BrokerSQS:
		return sqs.New()

	case msq.BrokerOCI:
		return oci.New(config.OCIQueue(env))

	case msq.BrokerRedis:
		return redisbroker.New(config.RedisBroker(env)), nil

	default:
		return nil, fmt.Errorf("msq: unknown BrokerType %q — use BrokerKafka, BrokerRabbitMQ, BrokerSQS, BrokerOCI or BrokerRedis", bt)
	}
}

// AddMessaging registers a messaging provider (RabbitMQ, Kafka, SQS, OCI, Redis).
//
// Three calling patterns, in order of ergonomics:
//
//	// 1 — zero-config: reads MESSAGING_PROVIDER from env
//	AddMessaging()
//
//	// 2 — explicit type, credentials from env
//	AddMessaging(msq.Config{BrokerType: msq.BrokerKafka})
//
//	// 3 — explicit broker instance (full control / custom config)
//	AddMessaging(msq.Config{Broker: myBroker})
//
// If the broker implements port.BrokerSetup, Setup is called automatically
// so that exchanges, topics or queues are declared before the first producer/consumer.
func (g *gofiInstance) AddMessaging(cfg ...msq.Config) Builder {
	var c msq.Config
	if len(cfg) > 0 {
		c = cfg[0]
	}

	// Zero-config: derive BrokerType from MESSAGING_PROVIDER env var.
	if c.Broker == nil && c.BrokerType == "" {
		envProvider := g.env.GetMessagingProvider()
		if envProvider == "" {
			logging.Error("AddMessaging: no broker configured — set BrokerType, pass a Broker, or set MESSAGING_PROVIDER")
			return g
		}
		c.BrokerType = msq.BrokerType(envProvider)
	}

	// Default event handler: structured log with severity matched to event type.
	if c.OnEvent == nil {
		c.OnEvent = messagingEventHandler
	}

	// Build broker from env when only BrokerType is given.
	if c.Broker == nil {
		b, err := brokerFromEnv(g.env, c.BrokerType, c.Exchange)
		if err != nil {
			logging.Error("AddMessaging: failed to build broker from env", slog.Any("error", err))
			return g
		}
		c.Broker = b
	}

	svc, err := msq.New(c)
	if err != nil {
		logging.Error("AddMessaging: failed to build broker service", slog.Any("error", err))
		return g
	}

	if setup, ok := c.Broker.(port.BrokerSetup); ok {
		if err := setup.Setup(context.Background()); err != nil {
			logging.Error("AddMessaging: broker setup failed", slog.Any("error", err))
			return g
		}
	}

	g.messagingConn = svc
	return g
}

// Injection methods — accept pre-built instances.
// Use these when the sub-modules are consumed independently and the caller
// controls the connection lifecycle outside of gofi.

func (g *gofiInstance) WithDatabase(db *sql.DB) Builder {
	g.databaseConn = db
	return g
}

func (g *gofiInstance) WithCache(client redis.UniversalClient) Builder {
	g.cacheConn = client
	return g
}

func (g *gofiInstance) WithMessaging(broker msq.Broker) Builder {
	g.messagingConn = broker
	return g
}

func (g *gofiInstance) WithObservability(tele *obs.Telemetry) Builder {
	g.telemetry = tele
	return g
}

func (g *gofiInstance) WithHttpServer(server netx.HttpServer) Builder {
	g.httpServer = server
	return g
}

// Build finalizes construction and returns the Service interface.
// After this point, Add* methods are no longer accessible.
func (g *gofiInstance) Build() Service {
	// Register DB pool gauges once both the pool and telemetry exist (the usual
	// order is AddDatabase().AddObservability(), so do it here at the end).
	if g.telemetry != nil && g.databaseConn != nil {
		if err := obs.ObserveDBStats("main", g.databaseConn); err != nil {
			logging.Error("Build: failed to register DB pool stats", slog.Any("error", err))
		}
	}
	return g
}
