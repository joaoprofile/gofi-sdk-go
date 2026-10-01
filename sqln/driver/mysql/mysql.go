package mysql

import (
	"database/sql"
	"errors"
	"fmt"
	"net"
	"net/url"
	"strconv"
	"strings"

	"github.com/joaoprofile/gofi-sdk-go/sqln/connection"
	sqln_driver "github.com/joaoprofile/gofi-sdk-go/sqln/driver"
)

// MySQL driver. To enable it, blank-import this package:
//
//	import _ "github.com/joaoprofile/gofi-sdk-go/sqln/driver/mysql"
//
// Requires the go-sql-driver/mysql driver in go.mod:
//
//	go get github.com/go-sql-driver/mysql
type Driver struct{}

const defaultPort = 3306

func (Driver) Name() connection.DriverName {
	return connection.DriverMySQL
}

// DSN builds a go-sql-driver/mysql DSN: user:pass@tcp(host:port)/dbname?tls=….
// The database name is path-escaped and the driver splits the DSN at the
// last '@' and '/', so the password may hold any character. The tls
// parameter follows s.EffectiveSSLMode(): disable → false, prefer →
// preferred, require → skip-verify, anything else → true (verify-full).
func (Driver) DSN(s connection.Settings) string {
	port := s.Port
	if port == 0 {
		port = defaultPort
	}
	host := strings.TrimSuffix(strings.TrimPrefix(s.Host, "["), "]")
	params := url.Values{"tls": {tlsParam(s.EffectiveSSLMode())}}
	return s.User + ":" + s.Password +
		"@tcp(" + net.JoinHostPort(host, strconv.Itoa(port)) + ")/" +
		url.PathEscape(s.Name) + "?" + params.Encode()
}

func tlsParam(mode string) string {
	switch mode {
	case connection.SSLDisable:
		return "false"
	case connection.SSLPrefer:
		return "preferred"
	case connection.SSLRequire:
		return "skip-verify"
	default:
		return "true"
	}
}

// ValidateSettings rejects what the DSN cannot express. A custom CA or client
// certificate needs a tls.Config registered with mysql.RegisterTLSConfig and
// a hand-built DSN with tls=<name>.
func (Driver) ValidateSettings(s connection.Settings) error {
	var errs []error
	if err := s.CheckSSLMode(connection.SSLDisable, connection.SSLPrefer, connection.SSLRequire, connection.SSLVerifyFull); err != nil {
		errs = append(errs, err)
	}
	if s.SSLRootCert != "" || s.SSLCert != "" {
		errs = append(errs, fmt.Errorf("%w: mysql: SSLRootCert/SSLCert need mysql.RegisterTLSConfig and a DSN with tls=<name>", connection.ErrInvalidSettings))
	}
	if s.StatementTimeout != 0 {
		errs = append(errs, fmt.Errorf("%w: mysql: StatementTimeout is not supported", connection.ErrInvalidSettings))
	}
	if strings.Contains(s.User, ":") {
		errs = append(errs, fmt.Errorf("%w: mysql: User cannot contain ':'", connection.ErrInvalidSettings))
	}
	if strings.HasPrefix(s.Host, "/") {
		errs = append(errs, fmt.Errorf("%w: mysql: unix sockets are not supported", connection.ErrInvalidSettings))
	}
	return errors.Join(errs...)
}

func (Driver) Open(cfg connection.Config) (*sql.DB, error) {
	if cfg.Password != nil {
		return nil, connection.ErrPasswordFuncUnsupported
	}
	return sql.Open("mysql", cfg.DSN)
}

// ParseError hides the server message behind a generic one (see connection.Error).
func (Driver) ParseError(err error) error {
	return connection.WrapError("query", err)
}

func (Driver) Dialect() sqln_driver.Dialect {
	return MySQLDialect{}
}

func init() {
	connection.RegisterDriver(Driver{})
}
