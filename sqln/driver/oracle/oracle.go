package oracle

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

// Oracle driver. To enable it, blank-import this package:
//
//	import _ "github.com/gofi-labs/gofi-sdk-go/sqln/driver/oracle"
//
// Requires godror or go-oci8 in go.mod.
type Driver struct{}

func (Driver) Name() connection.DriverName {
	return connection.DriverOracle
}

// DSN builds a go-ora URL, oracle://user:pass@host:port/service, with every
// part escaped. TLS follows s.EffectiveSSLMode(): disable → none, require →
// SSL=true without verification, anything else → SSL=true with
// "SSL VERIFY"=true (verify-full). Custom CAs go in a go-ora wallet.
func (Driver) DSN(s connection.Settings) string {
	q := url.Values{}
	switch s.EffectiveSSLMode() {
	case connection.SSLDisable:
	case connection.SSLRequire:
		q.Set("SSL", "true")
		q.Set("SSL VERIFY", "false")
	default:
		q.Set("SSL", "true")
		q.Set("SSL VERIFY", "true")
	}
	u := url.URL{
		Scheme:   "oracle",
		User:     url.UserPassword(s.User, s.Password),
		Host:     hostPort(s.Host, s.Port),
		Path:     "/" + s.Name,
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
	if s.SSLRootCert != "" || s.SSLCert != "" {
		errs = append(errs, fmt.Errorf("%w: oracle: SSLRootCert/SSLCert are not supported; use a go-ora wallet (WALLET parameter)", connection.ErrInvalidSettings))
	}
	if s.StatementTimeout != 0 {
		errs = append(errs, fmt.Errorf("%w: oracle: StatementTimeout is not supported", connection.ErrInvalidSettings))
	}
	if strings.HasPrefix(s.Host, "/") {
		errs = append(errs, fmt.Errorf("%w: oracle: unix sockets are not supported", connection.ErrInvalidSettings))
	}
	return errors.Join(errs...)
}

func (Driver) Open(cfg connection.Config) (*sql.DB, error) {
	if cfg.Password != nil {
		return nil, connection.ErrPasswordFuncUnsupported
	}
	return sql.Open("oracle", cfg.DSN)
}

// ParseError hides the server message behind a generic one (see connection.Error).
func (Driver) ParseError(err error) error {
	return connection.WrapError("query", err)
}

func (Driver) Dialect() sqln_driver.Dialect {
	return OracleDialect{}
}

func init() {
	connection.RegisterDriver(Driver{})
}
