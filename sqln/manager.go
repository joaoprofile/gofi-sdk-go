package sqln

import (
	"context"
	"database/sql"
	"errors"
	"iter"
	"slices"

	"github.com/gofi-labs/gofi-sdk-go/sqln/cache"
	"github.com/gofi-labs/gofi-sdk-go/sqln/connection"
	"github.com/gofi-labs/gofi-sdk-go/sqln/criteria"
	"github.com/gofi-labs/gofi-sdk-go/sqln/mapping"
	"github.com/gofi-labs/gofi-sdk-go/sqln/pagination"
)

// Manager defines a read-only query interface that can be mocked in tests.
type Manager[T any] interface {
	ExecuteListQuery(db *sql.DB) ([]T, error)
	ExecuteUniqueResultQuery(db *sql.DB) (*T, error)
	ExecutePagedQuery(db *sql.DB) (*Page[T], error)
}

type manager[T any] struct {
	ctx           context.Context
	conn          *connection.Connection // optional injected connection; if nil, falls back to global
	cache         *cache.Cache[T]
	page          *pagination.PageRequest
	query         string
	args          []any
	countQuery    string // optional; when empty the total comes from dialect.BuildCount(query)
	countArgs     []any
	criteriaQuery *criteria.Query // non-nil when created via FindFromCriteria; built lazily
	cacheScope    string          // mixed into the cache key; see WithCacheScope
	err           error           // criteria build failure, returned by the execution methods
}

//  Constructors

func Find[T any](ctx context.Context, query string, params ...any) *manager[T] {
	return &manager[T]{ctx: ctx, query: query, args: params}
}

func FindWithFilter[T any](ctx context.Context, params *QueryParam) *manager[T] {
	return &manager[T]{ctx: ctx, query: params.Query, args: params.Params}
}

func NewCustomQuery[T any](ctx context.Context, query string, params ...any) *manager[T] {
	return &manager[T]{ctx: ctx, query: query, args: params}
}

func FindFromCriteria[T any](ctx context.Context, q *criteria.Query) *manager[T] {
	return &manager[T]{ctx: ctx, criteriaQuery: q}
}

// Chainable options

// WithConnection injects a specific connection to use instead of the global singleton.
// Enables multi-tenant or multi-database scenarios without relying on connection.SetGlobal.
// Cached results are keyed by the connection's ID, so databases never share entries.
func (q *manager[T]) WithConnection(conn *connection.Connection) *manager[T] {
	q.conn = conn
	return q
}

func (q *manager[T]) WithPage(page *pagination.PageRequest) *manager[T] {
	q.page = page
	return q
}

// WithCache caches the result under a key of the query, its arguments, the page
// and the database (connection ID), plus any cache scope. Queries that run
// inside a transaction carried by ctx bypass the cache and request coalescing:
// they may see uncommitted rows that must not reach other callers.
func (q *manager[T]) WithCache(c *cache.Cache[T]) *manager[T] {
	q.cache = c
	return q
}

// WithCacheScope adds scope to the cache key. Set it whenever the same query on
// the same database returns different rows per caller — row-level security,
// session variables, per-tenant schemas selected at runtime — e.g. the tenant ID.
// Concurrent misses are only coalesced within the same scope. See also
// ContextWithCacheScope for a scope set once per request.
func (q *manager[T]) WithCacheScope(scope string) *manager[T] {
	q.cacheScope = scope
	return q
}

// WithCountQuery overrides the query that computes the pagination total.
// Without it the total comes from dialect.BuildCount(query), which wraps the
// whole page query — projection, joins and all. It serves the caller that knows
// some part of that query cannot affect the count yet carries a cost the planner
// will not remove on its own: a projection-only LEFT JOIN LATERAL is the typical
// case, since it yields 0-or-1 row per left row (never changing the total) but
// runs once per row.
// The query is used as given — BuildCount is not applied on top — and must
// return a single row holding one integer.
func (q *manager[T]) WithCountQuery(query string, args ...any) *manager[T] {
	q.countQuery = query
	q.countArgs = args
	return q
}

// Execution

func (q *manager[T]) Execute() (*T, error) {
	conn := q.connection()
	q.resolveFromCriteria(conn)
	return q.ExecuteUniqueResultQuery(q.readDB(conn))
}

func (q *manager[T]) List() ([]T, error) {
	conn := q.connection()
	q.resolveFromCriteria(conn)
	return q.ExecuteListQuery(q.readDB(conn))
}

func (q *manager[T]) UniqueResult() (*T, error) {
	conn := q.connection()
	q.resolveFromCriteria(conn)
	return q.ExecuteUniqueResultQuery(q.readDB(conn))
}

func (q *manager[T]) PagedList() (*Page[T], error) {
	conn := q.connection()
	q.resolveFromCriteria(conn) // uses BuildBase when page is set
	return q.ExecutePagedQuery(q.readDB(conn))
}

// readDB is the replica unless ctx carries a transaction (which the query
// helpers pick up from ctx anyway).
func (q *manager[T]) readDB(conn *connection.Connection) *sql.DB {
	if _, ok := connection.TxFor(q.ctx, conn.DB()); ok {
		return conn.DB()
	}
	return conn.ReadDB()
}

// All streams the result row by row without building the whole slice; the
// cache and pagination do not apply. Stop early by breaking the loop. The
// whole stream is bounded by the query timeout; pass a ctx with a longer
// deadline for long exports.
func (q *manager[T]) All() iter.Seq2[T, error] {
	return func(yield func(T, error) bool) {
		var zero T
		conn := q.connection()
		q.resolveFromCriteria(conn)
		instance := q.readDB(conn)
		if err := q.validate(instance); err != nil {
			yield(zero, err)
			return
		}
		ctx, cancel := conn.WithQueryTimeout(q.ctx)
		defer cancel()
		rows, err := NewQuery().FetchRows(ctx, instance, q.query, q.args...)
		if err != nil {
			yield(zero, err)
			return
		}
		for v, err := range mapping.Each[T](rows) {
			if !yield(v, connection.WrapError("scan", err)) {
				return
			}
		}
	}
}

// queryContext bounds q.ctx by the connection's query timeout (the default
// when no connection is known, as with the Manager interface methods).
func (q *manager[T]) queryContext() (context.Context, context.CancelFunc) {
	conn := q.conn
	if conn == nil {
		conn, _ = connection.Global()
	}
	return conn.WithQueryTimeout(q.ctx)
}

//  Manager interface (testable / mockable)

func (q *manager[T]) ExecuteListQuery(instance *sql.DB) ([]T, error) {
	q.resolveFromCriteriaLazy()
	if err := q.validate(instance); err != nil {
		return nil, err
	}
	return cached(q, instance, "list", q.fetchList, slices.Clone)
}

func (q *manager[T]) ExecuteUniqueResultQuery(instance *sql.DB) (*T, error) {
	q.resolveFromCriteriaLazy()
	if err := q.validate(instance); err != nil {
		return nil, err
	}
	return cached(q, instance, "unique", q.fetchUniqueResult, clonePtr)
}

func (q *manager[T]) ExecutePagedQuery(instance *sql.DB) (*Page[T], error) {
	q.resolveFromCriteriaLazy()
	if q.page == nil {
		return nil, errors.New(ErrPageIsEmpty)
	}
	if q.page.Limit == 0 {
		q.page.Limit = DefaultLimit
	}
	if err := q.validate(instance); err != nil {
		return nil, err
	}
	return cached(q, instance, "page", q.fetchPage, clonePage[T])
}

func (q *manager[T]) fetchPage(instance *sql.DB) (*Page[T], error) {
	var result Page[T]
	var err error
	result.TotalElements, err = q.pageTotal(instance)
	if err != nil {
		return nil, err
	}
	result.TotalPages = (result.TotalElements + uint64(q.page.Limit-1)) / uint64(q.page.Limit)
	result.Size = uint64(q.page.Limit)
	result.Number = uint64(q.page.Page)
	result.Content, err = q.fetchPagedList(instance)
	if err != nil {
		return nil, err
	}
	result.NumberOfElements = uint64(len(result.Content))
	return &result, nil
}

//  Internal fetch helpers

func (q *manager[T]) fetchList(instance *sql.DB) ([]T, error) {
	ctx, cancel := q.queryContext()
	defer cancel()
	rows, err := NewQuery().FetchRows(ctx, instance, q.query, q.args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	list, err := mapping.GetContentList[T](rows)
	return list, connection.WrapError("scan", err)
}

func (q *manager[T]) fetchUniqueResult(instance *sql.DB) (*T, error) {
	ctx, cancel := q.queryContext()
	defer cancel()
	rows, err := NewQuery().FetchRows(ctx, instance, q.query, q.args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	v, err := mapping.GetUnique[T](rows)
	return v, connection.WrapError("scan", err)
}

func (q *manager[T]) fetchPagedList(instance *sql.DB) ([]T, error) {
	conn := q.connection()

	// Widen before multiplying: Page and Limit are both uint16, so the product
	// wraps past 65535 and the caller silently receives a different page —
	// page=4400 limit=15 asks for offset 66000 and gets 464.
	sqlQuery := conn.Dialect().BuildPagination(
		q.query,
		q.page.GetOrder(),
		q.page.Limit,
		uint64(q.page.Page)*uint64(q.page.Limit),
	)

	ctx, cancel := q.queryContext()
	defer cancel()
	rows, err := NewQuery().FetchRows(ctx, instance, sqlQuery, q.args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	list, err := mapping.GetContentList[T](rows)
	return list, connection.WrapError("scan", err)
}

func (q *manager[T]) pageTotal(instance *sql.DB) (uint64, error) {
	sqlQuery, args := q.countQuery, q.countArgs
	if sqlQuery == "" {
		sqlQuery = q.connection().Dialect().BuildCount(q.query)
		args = q.args
	}

	ctx, cancel := q.queryContext()
	defer cancel()
	var result uint64
	err := NewQuery().FetchRow(ctx, instance, sqlQuery, args...).Scan(&result)

	return result, connection.WrapError("query", err)
}

// Criteria resolution

// resolveFromCriteria builds the SQL from a criteria.Query using the provided
// connection's dialect. Called from the high-level execution methods (List,
// UniqueResult, PagedList) which already hold a connection reference.
//
// When page is set, BuildBase is used so that ORDER BY / LIMIT / OFFSET are
// handled by dialect.BuildPagination instead.
// A build failure (e.g. an invalid field) is kept and returned by validate.
func (q *manager[T]) resolveFromCriteria(conn *connection.Connection) {
	if q.criteriaQuery == nil {
		return
	}
	if q.page != nil {
		q.query, q.args, q.err = q.criteriaQuery.BuildBase(conn.Dialect())
	} else {
		q.query, q.args, q.err = q.criteriaQuery.Build(conn.Dialect())
	}
	q.criteriaQuery = nil
}

// resolveFromCriteriaLazy is used by the Manager interface methods
// (ExecuteListQuery, etc.) which receive a *sql.DB but not a Connection.
// Uses the injected connection when available, otherwise falls back to global.
func (q *manager[T]) resolveFromCriteriaLazy() {
	if q.criteriaQuery == nil {
		return
	}
	q.resolveFromCriteria(q.connection())
}

//  Utilities

func (q *manager[T]) validate(instance *sql.DB) error {
	if q.err != nil {
		return q.err
	}
	if instance == nil {
		return errors.New(ErrDatabaseNotInitialized)
	}
	if q.query == "" {
		return errors.New(ErrMsgQueryIsEmpty)
	}
	return nil
}

func (q *manager[T]) connection() *connection.Connection {
	if q.conn != nil {
		return q.conn
	}
	conn, err := connection.Global()
	if err != nil {
		panic(err)
	}
	return conn
}
