package postgres_test

import (
	"context"
	"database/sql"
	"os"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/gofi-labs/gofi-sdk-go/sqln/connection"
	_ "github.com/gofi-labs/gofi-sdk-go/sqln/driver/postgres"
	"github.com/gofi-labs/gofi-sdk-go/sqln/mapping"
	"github.com/gofi-labs/gofi-sdk-go/sqln/transaction"
	"github.com/jackc/pgx/v5"
)

// Set GOFI_IT_POSTGRES_DSN (e.g. host=localhost port=5432 user=postgres
// password=pw dbname=it sslmode=disable) to run it.
func itConn(t *testing.T) *connection.Connection {
	t.Helper()
	dsn := os.Getenv("GOFI_IT_POSTGRES_DSN")
	if dsn == "" {
		t.Skip("GOFI_IT_POSTGRES_DSN not set")
	}
	var calls atomic.Int32
	conn, err := connection.NewConnection(connection.Config{
		Driver:   connection.DriverPostgres,
		DSN:      dsn,
		Password: func(context.Context) (string, error) { calls.Add(1); return dsnPassword(t, dsn), nil },
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { conn.Close() })
	if calls.Load() == 0 {
		t.Fatal("Password callback was not used")
	}
	return conn
}

// dsnPassword feeds the DSN password through the callback path.
func dsnPassword(t *testing.T, dsn string) string {
	cfg, err := pgx.ParseConfig(dsn)
	if err != nil {
		t.Fatal(err)
	}
	return cfg.Password
}

func TestIntegration_ArraysJSONAndErrors(t *testing.T) {
	db := itConn(t).DB()
	ctx := context.Background()
	_, err := db.ExecContext(ctx, `CREATE TEMP TABLE it_item (id int PRIMARY KEY, tags text[] NOT NULL, meta jsonb NOT NULL)`)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.ExecContext(ctx, `INSERT INTO it_item VALUES ($1, $2, $3)`, 1, []string{"a", "b"}, `{"k":1}`); err != nil {
		t.Fatal(err)
	}
	rows, err := db.QueryContext(ctx, `SELECT id, tags, meta FROM it_item`)
	if err != nil {
		t.Fatal(err)
	}
	type item struct {
		ID   int      `db:"id"`
		Tags []string `db:"tags"`
		Meta []byte   `db:"meta"`
	}
	list, err := mapping.GetContentList[item](rows)
	if err != nil || len(list) != 1 || len(list[0].Tags) != 2 || len(list[0].Meta) == 0 {
		t.Fatalf("got %+v, %v", list, err)
	}
	_, err = db.ExecContext(ctx, `INSERT INTO it_item VALUES (1, '{}', '{}')`)
	if pgErr, ok := connection.AsPgError(err); !ok || pgErr.Code != "23505" {
		t.Errorf("unique violation not surfaced: %v", err)
	}
}

func TestIntegration_SerializableRetry(t *testing.T) {
	conn := itConn(t)
	connection.ResetGlobalForTest()
	connection.SetGlobal(conn)
	t.Cleanup(connection.ResetGlobalForTest)
	db, ctx := conn.DB(), context.Background()
	if _, err := db.ExecContext(ctx, `CREATE TABLE IF NOT EXISTS it_counter (n int)`); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.ExecContext(ctx, `DROP TABLE it_counter`) })

	tr := transaction.New(transaction.Options{Isolation: sql.LevelSerializable, MaxRetries: 5})
	var attempts atomic.Int32
	var wg sync.WaitGroup
	errs := make(chan error, 2)
	for range 2 {
		wg.Go(func() {
			errs <- tr.Execute(ctx, func(ctx context.Context) error {
				attempts.Add(1)
				q := connection.QuerierFrom(ctx, db)
				var n int
				if err := q.QueryRowContext(ctx, `SELECT count(*) FROM it_counter`).Scan(&n); err != nil {
					return err
				}
				time.Sleep(100 * time.Millisecond)
				_, err := q.ExecContext(ctx, `INSERT INTO it_counter VALUES ($1)`, n)
				return err
			})
		})
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		if err != nil {
			t.Fatal(err)
		}
	}
	if attempts.Load() < 3 {
		t.Errorf("attempts=%d, expected a retried serialization failure", attempts.Load())
	}
}
