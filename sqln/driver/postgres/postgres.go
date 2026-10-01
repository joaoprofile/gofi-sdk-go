// Package postgres registers the PostgreSQL driver, built on pgx/v5.
package postgres

import (
	"context"
	"crypto/tls"
	"database/sql"
	"errors"
	"fmt"
	"strconv"
	"strings"

	"github.com/gofi-labs/gofi-sdk-go/sqln/connection"
	"github.com/gofi-labs/gofi-sdk-go/sqln/driver"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/stdlib"
)

// ErrPasswordFuncNeedsVerifiedTLS is returned by Open when Config.Password
// (an IAM token) would travel without a verified TLS connection.
var ErrPasswordFuncNeedsVerifiedTLS = errors.New("postgres: Config.Password requires sslmode verify-ca or verify-full")

type Driver struct{}

func (Driver) Name() connection.DriverName {
	return connection.DriverPostgres
}

// DSN builds a key-value connection string with every value quoted, so no
// value can add keys. sslmode is s.EffectiveSSLMode(): verify-full unless the
// host is local. StatementTimeout becomes the statement_timeout runtime
// parameter (not accepted by PgBouncer in transaction mode).
func (Driver) DSN(s connection.Settings) string {
	var b strings.Builder
	kv := func(k, v string) {
		if b.Len() > 0 {
			b.WriteByte(' ')
		}
		b.WriteString(k)
		b.WriteByte('=')
		b.WriteString(quote(v))
	}
	if s.Host != "" {
		kv("host", s.Host)
	}
	if s.Port != 0 {
		kv("port", strconv.Itoa(s.Port))
	}
	if s.User != "" {
		kv("user", s.User)
	}
	if s.Password != "" {
		kv("password", s.Password)
	}
	if s.Name != "" {
		kv("dbname", s.Name)
	}
	kv("sslmode", s.EffectiveSSLMode())
	if s.SSLRootCert != "" {
		kv("sslrootcert", s.SSLRootCert)
	}
	if s.SSLCert != "" {
		kv("sslcert", s.SSLCert)
	}
	if s.SSLKey != "" {
		kv("sslkey", s.SSLKey)
	}
	if s.StatementTimeout > 0 {
		kv("statement_timeout", strconv.FormatInt(max(s.StatementTimeout.Milliseconds(), 1), 10))
	}
	return b.String()
}

// quote renders v as a single-quoted key-value DSN value.
func quote(v string) string {
	return "'" + strings.NewReplacer(`\`, `\\`, `'`, `\'`).Replace(v) + "'"
}

// Open returns a pgx-backed *sql.DB. Unless the DSN sets
// default_query_exec_mode, queries use cache_describe: one round trip without
// named prepared statements, so PgBouncer in transaction mode keeps working.
// With Config.Password the DSN must verify the server certificate: an IAM
// token sent to an unverified server can be replayed by whoever receives it.
func (Driver) Open(cfg connection.Config) (*sql.DB, error) {
	cc, err := pgx.ParseConfig(cfg.DSN)
	if err != nil {
		return nil, fmt.Errorf("postgres: parse dsn: %w", err)
	}
	if !strings.Contains(cfg.DSN, "default_query_exec_mode") {
		cc.DefaultQueryExecMode = pgx.QueryExecModeCacheDescribe
	}
	var opts []stdlib.OptionOpenDB
	if cfg.Password != nil {
		if !verifiedTLS(cc) {
			return nil, ErrPasswordFuncNeedsVerifiedTLS
		}
		opts = append(opts, stdlib.OptionBeforeConnect(func(ctx context.Context, c *pgx.ConnConfig) error {
			pw, err := cfg.Password(ctx)
			if err != nil {
				return fmt.Errorf("postgres: password: %w", err)
			}
			c.Password = pw
			return nil
		}))
	}
	return stdlib.OpenDB(*cc, opts...), nil
}

// verifiedTLS reports whether every connection attempt of cc, fallbacks
// included, uses TLS that verifies the server certificate (verify-ca,
// verify-full, or require with sslrootcert, which libpq treats as verify-ca).
func verifiedTLS(cc *pgx.ConnConfig) bool {
	if !verified(cc.TLSConfig) {
		return false
	}
	for _, fb := range cc.Fallbacks {
		if !verified(fb.TLSConfig) {
			return false
		}
	}
	return true
}

func verified(t *tls.Config) bool {
	return t != nil && (!t.InsecureSkipVerify || t.VerifyPeerCertificate != nil)
}

// ParseError hides the server message behind a generic one (see connection.Error).
func (Driver) ParseError(err error) error {
	return connection.WrapError("query", err)
}

func (Driver) Dialect() driver.Dialect {
	return PostgresDialect{}
}

func init() {
	connection.RegisterDriver(Driver{})
}
