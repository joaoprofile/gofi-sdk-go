package driver_test

import (
	"strings"
	"testing"

	"github.com/gofi-labs/gofi-sdk-go/sqln/driver/mysql"
	"github.com/gofi-labs/gofi-sdk-go/sqln/driver/oracle"
	"github.com/gofi-labs/gofi-sdk-go/sqln/driver/postgres"
	"github.com/gofi-labs/gofi-sdk-go/sqln/driver/sqlserver"
)

func TestBuildPagination_EmptyOrderIsValidSQL(t *testing.T) {
	dialects := map[string]interface {
		BuildPagination(query, order string, limit uint16, offset uint64) string
	}{
		"postgres": postgres.PostgresDialect{}, "mysql": mysql.MySQLDialect{},
		"oracle": oracle.OracleDialect{}, "sqlserver": sqlserver.SQLServerDialect{},
	}
	for name, d := range dialects {
		sql := d.BuildPagination("SELECT id FROM t", "", 10, 20)
		if strings.Contains(sql, "ORDER BY  ") || strings.Contains(sql, "ORDER BY )") || strings.HasSuffix(strings.TrimSpace(sql), "ORDER BY") {
			t.Errorf("%s: dangling ORDER BY: %s", name, sql)
		}
		if name == "sqlserver" && !strings.Contains(sql, "ORDER BY (SELECT NULL)") {
			t.Errorf("sqlserver needs an ORDER BY for OFFSET/FETCH: %s", sql)
		}
		if got := d.BuildPagination("SELECT id FROM t", "id ASC", 10, 20); !strings.Contains(got, "ORDER BY id ASC") {
			t.Errorf("%s: order lost: %s", name, got)
		}
	}
}
