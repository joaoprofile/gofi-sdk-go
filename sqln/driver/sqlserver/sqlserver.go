package sqlserver

import (
	"database/sql"
	"errors"
	"fmt"
	"net"
	"net/url"
	"strconv"
	"strings"

	"github.com/gofi-labs/gofi-sdk-go/sqln/connection"
	sqln_driver "github.com/gofi-labs/gofi-sdk-go/sqln/driver"
)

// SQL Server driver. To enable it, blank-import this package:
//
//	import _ "github.com/gofi-labs/gofi-sdk-go/sqln/driver/sqlserver"
//
// Requires github.com/denisenkom/go-mssqldb in go.mod:
//
//	go get github.com/denisenkom/go-mssqldb
type Driver struct{}

func (Driver) Name() connection.DriverName {
	return connection.DriverSQLServer
}

// DSN builds a go-mssqldb URL, sqlserver://user:pass@host:port?database=dbname,
// with every part escaped. Encryption follows s.EffectiveSSLMode(): disable →
// encrypt=disable, require → encrypt=true with TrustServerCertificate=true,
// anything else → encrypt=true verifying the certificate (verify-full);
// SSLRootCert becomes the certificate parameter.
func (Driver) DSN(s connection.Settings) string {
	q := url.Values{}
	if s.Name != "" {
		q.Set("database", s.Name)
	}
	switch s.EffectiveSSLMode() {
	case connection.SSLDisable:
		q.Set("encrypt", "disable")
	case connection.SSLRequire:
		q.Set("encrypt", "true")
		q.Set("TrustServerCertificate", "true")
	default:
		q.Set("encrypt", "true")
		q.Set("TrustServerCertificate", "false")
		if s.SSLRootCert != "" {
			q.Set("certificate", s.SSLRootCert)
		}
	}
	u := url.URL{
		Scheme:   "sqlserver",
		User:     url.UserPassword(s.User, s.Password),
		Host:     hostPort(s.Host, s.Port),
		RawQuery: q.Encode(),
	}
	return u.String()
}

// hostPort joins host and port, bracketing IPv6 addresses; port 0 is omitted.
func hostPort(host string, port int) string {
	host = strings.TrimSuffix(strings.TrimPrefix(host, "["), "]")
	if port != 0 {
		return net.JoinHostPort(host, strconv.Itoa(port))
	}
	if strings.Contains(host, ":") {
		return "[" + host + "]"
	}
	return host
}

// ValidateSettings rejects what the DSN cannot express.
func (Driver) ValidateSettings(s connection.Settings) error {
	var errs []error
	if err := s.CheckSSLMode(connection.SSLDisable, connection.SSLRequire, connection.SSLVerifyFull); err != nil {
		errs = append(errs, err)
	}
	if s.SSLCert != "" {
		errs = append(errs, fmt.Errorf("%w: sqlserver: client certificates are not supported", connection.ErrInvalidSettings))
	}
	if s.StatementTimeout != 0 {
		errs = append(errs, fmt.Errorf("%w: sqlserver: StatementTimeout is not supported", connection.ErrInvalidSettings))
	}
	if strings.HasPrefix(s.Host, "/") {
		errs = append(errs, fmt.Errorf("%w: sqlserver: unix sockets are not supported", connection.ErrInvalidSettings))
	}
	return errors.Join(errs...)
}

func (Driver) Open(cfg connection.Config) (*sql.DB, error) {
	if cfg.Password != nil {
		return nil, connection.ErrPasswordFuncUnsupported
	}
	return sql.Open("sqlserver", cfg.DSN)
}

// ParseError hides the server message behind a generic one (see connection.Error).
func (Driver) ParseError(err error) error {
	return connection.WrapError("query", err)
}

func (Driver) Dialect() sqln_driver.Dialect {
	return SQLServerDialect{}
}

func init() {
	connection.RegisterDriver(Driver{})
}
